package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GrowthCardResponse is what one delivery taught the person who delivered it.
//
// Eight named fields rather than one prose body: the questions ARE the method,
// and free text lets the writer skip exactly the ones that would expose a
// delivery they never understood. Every field may be empty — a half-filled
// card is the honest record of a loop that was not closed, which is the
// information a four-week review is looking for.
type GrowthCardResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	// The requirement this card is about, when it was tracked as an issue.
	IssueID    *string `json:"issue_id"`
	AuthorType string  `json:"author_type"`
	AuthorID   string  `json:"author_id"`
	// 需求 — also the card's display name.
	Title string `json:"title"`
	// 系统涉及
	Systems string `json:"systems"`
	// 我原本不会的东西
	Unknowns string `json:"unknowns"`
	// Agent 给出的方案
	AgentPlan string `json:"agent_plan"`
	// 我确认理解的关键点
	Understood string `json:"understood"`
	// 我亲自验证了什么
	Verified string `json:"verified"`
	// 这次真正学会了什么
	Learned string `json:"learned"`
	// 下次要补的知识
	NextGaps  string `json:"next_gaps"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

const (
	maxGrowthCardTitleLength = 200
	maxGrowthCardFieldLength = 20_000
	growthCardDefaultPage    = 50
	growthCardMaxPage        = 200
)

func growthCardToResponse(c db.GrowthCard) GrowthCardResponse {
	return GrowthCardResponse{
		ID:          uuidToString(c.ID),
		WorkspaceID: uuidToString(c.WorkspaceID),
		IssueID:     uuidToPtr(c.IssueID),
		AuthorType:  c.AuthorType,
		AuthorID:    uuidToString(c.AuthorID),
		Title:       c.Title,
		Systems:     c.Systems,
		Unknowns:    c.Unknowns,
		AgentPlan:   c.AgentPlan,
		Understood:  c.Understood,
		Verified:    c.Verified,
		Learned:     c.Learned,
		NextGaps:    c.NextGaps,
		CreatedAt:   timestampToString(c.CreatedAt),
		UpdatedAt:   timestampToString(c.UpdatedAt),
	}
}

// growthCardFields is the writable body of a card. Pointers so an update can
// tell "leave this field alone" from "clear it" — a card is filled in over
// several sittings, often one field at a time.
type growthCardFields struct {
	Title      *string `json:"title"`
	Systems    *string `json:"systems"`
	Unknowns   *string `json:"unknowns"`
	AgentPlan  *string `json:"agent_plan"`
	Understood *string `json:"understood"`
	Verified   *string `json:"verified"`
	Learned    *string `json:"learned"`
	NextGaps   *string `json:"next_gaps"`
}

type CreateGrowthCardRequest struct {
	growthCardFields
	IssueID *string `json:"issue_id"`
}

type UpdateGrowthCardRequest struct {
	growthCardFields
	// A present-but-null issue_id detaches the card from its requirement; an
	// absent one leaves the link alone. json.RawMessage is what makes those
	// two distinguishable.
	IssueID json.RawMessage `json:"issue_id"`
}

// validate rejects oversized fields. Emptiness is not an error: refusing to
// save a card with blanks would only teach the writer to fill the boxes with
// noise, which destroys the one signal the card exists to carry.
func (f growthCardFields) validate(w http.ResponseWriter) bool {
	fields := []struct {
		name  string
		value *string
		limit int
	}{
		{"title", f.Title, maxGrowthCardTitleLength},
		{"systems", f.Systems, maxGrowthCardFieldLength},
		{"unknowns", f.Unknowns, maxGrowthCardFieldLength},
		{"agent_plan", f.AgentPlan, maxGrowthCardFieldLength},
		{"understood", f.Understood, maxGrowthCardFieldLength},
		{"verified", f.Verified, maxGrowthCardFieldLength},
		{"learned", f.Learned, maxGrowthCardFieldLength},
		{"next_gaps", f.NextGaps, maxGrowthCardFieldLength},
	}
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		// Characters, not bytes: a Chinese card would otherwise hit the cap at
		// a third of the intended length.
		if len([]rune(*field.value)) > field.limit {
			writeError(w, http.StatusBadRequest,
				field.name+" must be at most "+strconv.Itoa(field.limit)+" characters")
			return false
		}
	}
	return true
}

// growthCardText reads an optional field for a create, where absent and empty
// mean the same thing.
func growthCardText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// growthCardPatch reads an optional field for an update, where absent means
// "leave the stored value alone" and the query COALESCEs on validity.
func growthCardPatch(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: strings.TrimSpace(*value), Valid: true}
}

// resolveGrowthCardIssue validates an optional issue reference and confirms it
// belongs to this workspace, so a card can never point at another tenant's
// requirement.
func (h *Handler) resolveGrowthCardIssue(
	w http.ResponseWriter,
	r *http.Request,
	raw *string,
) (pgtype.UUID, bool) {
	var issueID pgtype.UUID
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return issueID, true
	}
	// Accepts a UUID or a human identifier (COC-6); the loader resolves both
	// and enforces workspace membership.
	issue, ok := h.loadIssueForUser(w, r, strings.TrimSpace(*raw))
	if !ok {
		return issueID, false
	}
	return issue.ID, true
}

// CreateGrowthCard handles POST /api/growth-cards.
func (h *Handler) CreateGrowthCard(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateGrowthCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.validate(w) {
		return
	}
	issueID, ok := h.resolveGrowthCardIssue(w, r, req.IssueID)
	if !ok {
		return
	}

	userID := requestUserID(r)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	card, err := h.Queries.CreateGrowthCard(r.Context(), db.CreateGrowthCardParams{
		WorkspaceID: wsUUID,
		IssueID:     issueID,
		AuthorType:  authorType,
		AuthorID:    parseUUID(authorID),
		Title:       growthCardText(req.Title),
		Systems:     growthCardText(req.Systems),
		Unknowns:    growthCardText(req.Unknowns),
		AgentPlan:   growthCardText(req.AgentPlan),
		Understood:  growthCardText(req.Understood),
		Verified:    growthCardText(req.Verified),
		Learned:     growthCardText(req.Learned),
		NextGaps:    growthCardText(req.NextGaps),
	})
	if err != nil {
		slog.Warn("create growth card failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create growth card")
		return
	}

	writeJSON(w, http.StatusCreated, growthCardToResponse(card))
}

// ListGrowthCards handles GET /api/growth-cards.
func (h *Handler) ListGrowthCards(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limit := growthCardDefaultPage
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > growthCardMaxPage {
			writeError(w, http.StatusBadRequest,
				"limit must be between 1 and "+strconv.Itoa(growthCardMaxPage))
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "offset must not be negative")
			return
		}
		offset = parsed
	}

	rows, err := h.Queries.ListGrowthCards(r.Context(), db.ListGrowthCardsParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		slog.Warn("list growth cards failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list growth cards")
		return
	}
	total, err := h.Queries.CountGrowthCards(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("count growth cards failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list growth cards")
		return
	}

	resp := make([]GrowthCardResponse, len(rows))
	for i, row := range rows {
		resp[i] = growthCardToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": resp, "total": total})
}

// GetGrowthCard handles GET /api/growth-cards/{id}.
func (h *Handler) GetGrowthCard(w http.ResponseWriter, r *http.Request) {
	card, ok := h.loadGrowthCardForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, growthCardToResponse(card))
}

// UpdateGrowthCard handles PUT /api/growth-cards/{id}.
func (h *Handler) UpdateGrowthCard(w http.ResponseWriter, r *http.Request) {
	card, ok := h.loadGrowthCardForUser(w, r)
	if !ok {
		return
	}

	var req UpdateGrowthCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.validate(w) {
		return
	}

	// Absent, explicit null, and a value are three different intents.
	clearIssue := len(req.IssueID) > 0 && string(req.IssueID) == "null"
	var issueRef *string
	if len(req.IssueID) > 0 && !clearIssue {
		var raw string
		if err := json.Unmarshal(req.IssueID, &raw); err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		issueRef = &raw
	}
	issueID, ok := h.resolveGrowthCardIssue(w, r, issueRef)
	if !ok {
		return
	}

	updated, err := h.Queries.UpdateGrowthCard(r.Context(), db.UpdateGrowthCardParams{
		ID:          card.ID,
		WorkspaceID: card.WorkspaceID,
		Title:       growthCardPatch(req.Title),
		Systems:     growthCardPatch(req.Systems),
		Unknowns:    growthCardPatch(req.Unknowns),
		AgentPlan:   growthCardPatch(req.AgentPlan),
		Understood:  growthCardPatch(req.Understood),
		Verified:    growthCardPatch(req.Verified),
		Learned:     growthCardPatch(req.Learned),
		NextGaps:    growthCardPatch(req.NextGaps),
		IssueID:     issueID,
		ClearIssue:  clearIssue,
	})
	if err != nil {
		slog.Warn("update growth card failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update growth card")
		return
	}
	writeJSON(w, http.StatusOK, growthCardToResponse(updated))
}

// DeleteGrowthCard handles DELETE /api/growth-cards/{id}.
func (h *Handler) DeleteGrowthCard(w http.ResponseWriter, r *http.Request) {
	card, ok := h.loadGrowthCardForUser(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteGrowthCard(r.Context(), db.DeleteGrowthCardParams{
		ID:          card.ID,
		WorkspaceID: card.WorkspaceID,
	}); err != nil {
		slog.Warn("delete growth card failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete growth card")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListGrowthCardsForIssue handles GET /api/issues/{id}/growth-cards.
func (h *Handler) ListGrowthCardsForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListGrowthCardsForIssue(r.Context(), db.ListGrowthCardsForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list growth cards for issue failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list growth cards")
		return
	}
	resp := make([]GrowthCardResponse, len(rows))
	for i, row := range rows {
		resp[i] = growthCardToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": resp})
}

// loadGrowthCardForUser resolves the {id} path param within the caller's
// workspace. A card from another tenant is reported as not-found rather than
// forbidden, so the endpoint never confirms that an id exists elsewhere.
func (h *Handler) loadGrowthCardForUser(
	w http.ResponseWriter,
	r *http.Request,
) (db.GrowthCard, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.GrowthCard{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.GrowthCard{}, false
	}
	card, err := h.Queries.GetGrowthCard(r.Context(), db.GetGrowthCardParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "growth card not found")
		} else {
			slog.Warn("load growth card failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load growth card")
		}
		return db.GrowthCard{}, false
	}
	return card, true
}
