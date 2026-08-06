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

// CardResponse is a note someone wrote to remember what a piece of work
// taught them.
type CardResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	// The requirement this came out of, when there is one. Null is normal —
	// a lesson can come from anywhere.
	IssueID    *string `json:"issue_id"`
	AuthorType string  `json:"author_type"`
	AuthorID   string  `json:"author_id"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

const (
	maxCardTitleLength   = 200
	maxCardContentLength = 200_000
	cardDefaultPageSize  = 50
	cardMaxPageSize      = 200
)

func cardToResponse(r db.Card) CardResponse {
	return CardResponse{
		ID:          uuidToString(r.ID),
		WorkspaceID: uuidToString(r.WorkspaceID),
		IssueID:     uuidToPtr(r.IssueID),
		AuthorType:  r.AuthorType,
		AuthorID:    uuidToString(r.AuthorID),
		Title:       r.Title,
		Content:     r.Content,
		CreatedAt:   timestampToString(r.CreatedAt),
		UpdatedAt:   timestampToString(r.UpdatedAt),
	}
}

type CreateCardRequest struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	IssueID *string `json:"issue_id"`
}

type UpdateCardRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	// A present-but-null issue_id detaches the card from its requirement;
	// an absent one leaves the link alone. json.RawMessage is what makes
	// those two distinguishable.
	IssueID json.RawMessage `json:"issue_id"`
}

// resolveCardIssue validates an optional issue reference and confirms it
// belongs to this workspace, so a card can never point at another tenant's
// requirement.
func (h *Handler) resolveCardIssue(
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

// validateCardText bounds the two fields and rejects a card with nothing in
// it at all.
//
// A blank TITLE is allowed on purpose: the point of a card is to write the
// thought down, and demanding a name for it first is exactly the friction
// that stops people writing. The list falls back to a preview of the body.
// A card with neither title nor body is not a note, it is a stray click.
func validateCardText(w http.ResponseWriter, title, content string) bool {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(content) == "" {
		writeError(w, http.StatusBadRequest, "a card needs a title or some content")
		return false
	}
	if len([]rune(title)) > maxCardTitleLength {
		writeError(w, http.StatusBadRequest,
			"title must be at most "+strconv.Itoa(maxCardTitleLength)+" characters")
		return false
	}
	if len([]rune(content)) > maxCardContentLength {
		writeError(w, http.StatusBadRequest,
			"content must be at most "+strconv.Itoa(maxCardContentLength)+" characters")
		return false
	}
	return true
}

// CreateCard handles POST /api/cards.
func (h *Handler) CreateCard(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if !validateCardText(w, title, req.Content) {
		return
	}

	issueID, ok := h.resolveCardIssue(w, r, req.IssueID)
	if !ok {
		return
	}

	userID := requestUserID(r)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	card, err := h.Queries.CreateCard(r.Context(), db.CreateCardParams{
		WorkspaceID: wsUUID,
		IssueID:     issueID,
		AuthorType:  authorType,
		AuthorID:    parseUUID(authorID),
		Title:       title,
		Content:     req.Content,
	})
	if err != nil {
		slog.Warn("create card failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create card")
		return
	}

	writeJSON(w, http.StatusCreated, cardToResponse(card))
}

// ListCards handles GET /api/cards.
func (h *Handler) ListCards(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limit := cardDefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > cardMaxPageSize {
			writeError(w, http.StatusBadRequest,
				"limit must be between 1 and "+strconv.Itoa(cardMaxPageSize))
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

	// A card is written to be found again months later, which a page-by-page
	// scroll cannot do. Filtering here rather than client-side because the
	// caller only ever holds one page — a client filter would search the page,
	// not the workspace, and would silently return nothing for a card two
	// pages down.
	//
	// Lowercased in Go so SQL lowercases the column only, matching how issue
	// search feeds the pg_bigm indexes (see buildSearchQuery in issue.go).
	var (
		rows  []db.Card
		total int64
		err   error
	)
	if query := strings.TrimSpace(r.URL.Query().Get("q")); query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		rows, err = h.Queries.SearchCards(r.Context(), db.SearchCardsParams{
			WorkspaceID: wsUUID,
			Pattern:     pattern,
			Limit:       int32(limit),
			Offset:      int32(offset),
		})
		if err == nil {
			total, err = h.Queries.CountSearchCards(r.Context(), db.CountSearchCardsParams{
				WorkspaceID: wsUUID,
				Pattern:     pattern,
			})
		}
	} else {
		rows, err = h.Queries.ListCards(r.Context(), db.ListCardsParams{
			WorkspaceID: wsUUID,
			Limit:       int32(limit),
			Offset:      int32(offset),
		})
		if err == nil {
			total, err = h.Queries.CountCards(r.Context(), wsUUID)
		}
	}
	if err != nil {
		slog.Warn("list cards failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list cards")
		return
	}

	resp := make([]CardResponse, len(rows))
	for i, row := range rows {
		resp[i] = cardToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": resp, "total": total})
}

// GetCard handles GET /api/cards/{id}.
func (h *Handler) GetCard(w http.ResponseWriter, r *http.Request) {
	card, ok := h.loadCardForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, cardToResponse(card))
}

// UpdateCard handles PUT /api/cards/{id}.
func (h *Handler) UpdateCard(w http.ResponseWriter, r *http.Request) {
	card, ok := h.loadCardForUser(w, r)
	if !ok {
		return
	}

	var req UpdateCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := card.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}
	content := card.Content
	if req.Content != nil {
		content = *req.Content
	}
	if !validateCardText(w, title, content) {
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
	issueID, ok := h.resolveCardIssue(w, r, issueRef)
	if !ok {
		return
	}

	updated, err := h.Queries.UpdateCard(r.Context(), db.UpdateCardParams{
		ID:          card.ID,
		WorkspaceID: card.WorkspaceID,
		Title:       pgtype.Text{String: title, Valid: true},
		Content:     pgtype.Text{String: content, Valid: true},
		IssueID:     issueID,
		ClearIssue:  clearIssue,
	})
	if err != nil {
		slog.Warn("update card failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update card")
		return
	}
	writeJSON(w, http.StatusOK, cardToResponse(updated))
}

// DeleteCard handles DELETE /api/cards/{id}.
func (h *Handler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	card, ok := h.loadCardForUser(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteCard(r.Context(), db.DeleteCardParams{
		ID:          card.ID,
		WorkspaceID: card.WorkspaceID,
	}); err != nil {
		slog.Warn("delete card failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete card")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListCardsForIssue handles GET /api/issues/{id}/cards.
func (h *Handler) ListCardsForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListCardsForIssue(r.Context(), db.ListCardsForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list cards for issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list cards")
		return
	}
	resp := make([]CardResponse, len(rows))
	for i, row := range rows {
		resp[i] = cardToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": resp})
}

// loadCardForUser resolves the {id} path param within the caller's workspace.
// A card from another tenant is reported as not-found rather than forbidden,
// so the endpoint never confirms that an id exists elsewhere.
func (h *Handler) loadCardForUser(w http.ResponseWriter, r *http.Request) (db.Card, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Card{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.Card{}, false
	}
	card, err := h.Queries.GetCard(r.Context(), db.GetCardParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "card not found")
		} else {
			slog.Warn("load card failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load card")
		}
		return db.Card{}, false
	}
	return card, true
}
