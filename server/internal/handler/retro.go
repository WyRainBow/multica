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

// RetroResponse is a note someone wrote to remember what a piece of work
// taught them.
type RetroResponse struct {
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
	maxRetroTitleLength   = 200
	maxRetroContentLength = 200_000
	retroDefaultPageSize  = 50
	retroMaxPageSize      = 200
)

func retroToResponse(r db.Retro) RetroResponse {
	return RetroResponse{
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

type CreateRetroRequest struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	IssueID *string `json:"issue_id"`
}

type UpdateRetroRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	// A present-but-null issue_id detaches the retro from its requirement;
	// an absent one leaves the link alone. json.RawMessage is what makes
	// those two distinguishable.
	IssueID json.RawMessage `json:"issue_id"`
}

// resolveRetroIssue validates an optional issue reference and confirms it
// belongs to this workspace, so a retro can never point at another tenant's
// requirement.
func (h *Handler) resolveRetroIssue(
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

func validateRetroText(w http.ResponseWriter, title, content string) bool {
	if strings.TrimSpace(title) == "" {
		writeError(w, http.StatusBadRequest, "title must not be blank")
		return false
	}
	if len([]rune(title)) > maxRetroTitleLength {
		writeError(w, http.StatusBadRequest,
			"title must be at most "+strconv.Itoa(maxRetroTitleLength)+" characters")
		return false
	}
	if len([]rune(content)) > maxRetroContentLength {
		writeError(w, http.StatusBadRequest,
			"content must be at most "+strconv.Itoa(maxRetroContentLength)+" characters")
		return false
	}
	return true
}

// CreateRetro handles POST /api/retros.
func (h *Handler) CreateRetro(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateRetroRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if !validateRetroText(w, title, req.Content) {
		return
	}

	issueID, ok := h.resolveRetroIssue(w, r, req.IssueID)
	if !ok {
		return
	}

	userID := requestUserID(r)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	retro, err := h.Queries.CreateRetro(r.Context(), db.CreateRetroParams{
		WorkspaceID: wsUUID,
		IssueID:     issueID,
		AuthorType:  authorType,
		AuthorID:    parseUUID(authorID),
		Title:       title,
		Content:     req.Content,
	})
	if err != nil {
		slog.Warn("create retro failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create retro")
		return
	}

	writeJSON(w, http.StatusCreated, retroToResponse(retro))
}

// ListRetros handles GET /api/retros.
func (h *Handler) ListRetros(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limit := retroDefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > retroMaxPageSize {
			writeError(w, http.StatusBadRequest,
				"limit must be between 1 and "+strconv.Itoa(retroMaxPageSize))
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

	rows, err := h.Queries.ListRetros(r.Context(), db.ListRetrosParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		slog.Warn("list retros failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list retros")
		return
	}
	total, err := h.Queries.CountRetros(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("count retros failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list retros")
		return
	}

	resp := make([]RetroResponse, len(rows))
	for i, row := range rows {
		resp[i] = retroToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"retros": resp, "total": total})
}

// GetRetro handles GET /api/retros/{id}.
func (h *Handler) GetRetro(w http.ResponseWriter, r *http.Request) {
	retro, ok := h.loadRetroForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, retroToResponse(retro))
}

// UpdateRetro handles PUT /api/retros/{id}.
func (h *Handler) UpdateRetro(w http.ResponseWriter, r *http.Request) {
	retro, ok := h.loadRetroForUser(w, r)
	if !ok {
		return
	}

	var req UpdateRetroRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := retro.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}
	content := retro.Content
	if req.Content != nil {
		content = *req.Content
	}
	if !validateRetroText(w, title, content) {
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
	issueID, ok := h.resolveRetroIssue(w, r, issueRef)
	if !ok {
		return
	}

	updated, err := h.Queries.UpdateRetro(r.Context(), db.UpdateRetroParams{
		ID:          retro.ID,
		WorkspaceID: retro.WorkspaceID,
		Title:       pgtype.Text{String: title, Valid: true},
		Content:     pgtype.Text{String: content, Valid: true},
		IssueID:     issueID,
		ClearIssue:  clearIssue,
	})
	if err != nil {
		slog.Warn("update retro failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update retro")
		return
	}
	writeJSON(w, http.StatusOK, retroToResponse(updated))
}

// DeleteRetro handles DELETE /api/retros/{id}.
func (h *Handler) DeleteRetro(w http.ResponseWriter, r *http.Request) {
	retro, ok := h.loadRetroForUser(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteRetro(r.Context(), db.DeleteRetroParams{
		ID:          retro.ID,
		WorkspaceID: retro.WorkspaceID,
	}); err != nil {
		slog.Warn("delete retro failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete retro")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListRetrosForIssue handles GET /api/issues/{id}/retros.
func (h *Handler) ListRetrosForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListRetrosForIssue(r.Context(), db.ListRetrosForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list retros for issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list retros")
		return
	}
	resp := make([]RetroResponse, len(rows))
	for i, row := range rows {
		resp[i] = retroToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"retros": resp})
}

// loadRetroForUser resolves the {id} path param within the caller's workspace.
// A retro from another tenant is reported as not-found rather than forbidden,
// so the endpoint never confirms that an id exists elsewhere.
func (h *Handler) loadRetroForUser(w http.ResponseWriter, r *http.Request) (db.Retro, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Retro{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.Retro{}, false
	}
	retro, err := h.Queries.GetRetro(r.Context(), db.GetRetroParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "retro not found")
		} else {
			slog.Warn("load retro failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load retro")
		}
		return db.Retro{}, false
	}
	return retro, true
}
