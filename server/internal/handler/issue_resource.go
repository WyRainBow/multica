package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueResourceResponse is a page attached to an issue whose home is elsewhere:
// a design doc, a meeting note, a vendor page.
//
// Distinct from an attachment, which is a file we store, and from a pull
// request link, which the webhook writes and only ever points at a PR.
type IssueResourceResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
	URL         string `json:"url"`
	// The reader's label, not the page's. Empty is normal — the UI falls back
	// to the host so a row is never blank.
	Title      string `json:"title"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id"`
	Position   int32  `json:"position"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

const (
	maxIssueResourceURLLength   = 2000
	maxIssueResourceTitleLength = 200
	// Same convention as issue.position and issue_phase.position: room to
	// insert between two rows without renumbering their neighbours.
	issueResourcePositionStep = 1000
)

func issueResourceToResponse(r db.IssueResource) IssueResourceResponse {
	return IssueResourceResponse{
		ID:          uuidToString(r.ID),
		WorkspaceID: uuidToString(r.WorkspaceID),
		IssueID:     uuidToString(r.IssueID),
		URL:         r.Url,
		Title:       r.Title,
		AuthorType:  r.AuthorType,
		AuthorID:    uuidToString(r.AuthorID),
		Position:    r.Position,
		CreatedAt:   timestampToString(r.CreatedAt),
		UpdatedAt:   timestampToString(r.UpdatedAt),
	}
}

// normalizeResourceURL bounds and validates a link before it is stored.
//
// http(s) only, with a host. That rejects `javascript:` and `data:` — a stored
// URL is rendered as a clickable row, so a scheme that executes rather than
// navigates would turn adding a resource into running code in a reader's
// browser. Mirrors the URL custom-property check in property.go.
func normalizeResourceURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("url is required")
	}
	if len(trimmed) > maxIssueResourceURLLength {
		return "", errors.New("url is too long")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("url must be an http(s) URL")
	}
	return trimmed, nil
}

func validateResourceTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	// Characters, not bytes: these titles are Chinese as often as not.
	if len([]rune(trimmed)) > maxIssueResourceTitleLength {
		return "", errors.New("title is too long")
	}
	return trimmed, nil
}

type createIssueResourceRequest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// CreateIssueResource handles POST /api/issues/{id}/resources.
func (h *Handler) CreateIssueResource(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req createIssueResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized, err := normalizeResourceURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	title, err := validateResourceTitle(req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	max, err := h.Queries.MaxIssueResourcePosition(r.Context(), db.MaxIssueResourcePositionParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("read max resource position failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add resource")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	authorType, authorID := h.resolveActor(r, requestUserID(r), workspaceID)
	resource, err := h.Queries.CreateIssueResource(r.Context(), db.CreateIssueResourceParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		Url:         normalized,
		Title:       title,
		AuthorType:  authorType,
		AuthorID:    parseUUID(authorID),
		Position:    max + issueResourcePositionStep,
	})
	if err != nil {
		slog.Warn("create issue resource failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add resource")
		return
	}
	writeJSON(w, http.StatusCreated, issueResourceToResponse(resource))
}

// ListIssueResources handles GET /api/issues/{id}/resources.
func (h *Handler) ListIssueResources(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssueResources(r.Context(), db.ListIssueResourcesParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list issue resources failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list resources")
		return
	}
	resp := make([]IssueResourceResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, issueResourceToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": resp})
}

type updateIssueResourceRequest struct {
	URL   *string `json:"url"`
	Title *string `json:"title"`
}

// UpdateIssueResource handles PUT /api/issues/{id}/resources/{resourceId}.
func (h *Handler) UpdateIssueResource(w http.ResponseWriter, r *http.Request) {
	resource, ok := h.loadIssueResourceForUser(w, r)
	if !ok {
		return
	}

	var req updateIssueResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var newURL pgtype.Text
	if req.URL != nil {
		normalized, err := normalizeResourceURL(*req.URL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		newURL = pgtype.Text{String: normalized, Valid: true}
	}
	// A present-but-empty title clears the label back to the host fallback,
	// which is a real intent — so validity is tied to the pointer, not to the
	// string being non-empty.
	var newTitle pgtype.Text
	if req.Title != nil {
		title, err := validateResourceTitle(*req.Title)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		newTitle = pgtype.Text{String: title, Valid: true}
	}
	if !newURL.Valid && !newTitle.Valid {
		writeError(w, http.StatusBadRequest, "nothing to update; send url or title")
		return
	}

	updated, err := h.Queries.UpdateIssueResource(r.Context(), db.UpdateIssueResourceParams{
		ID:          resource.ID,
		WorkspaceID: resource.WorkspaceID,
		Url:         newURL,
		Title:       newTitle,
	})
	if err != nil {
		slog.Warn("update issue resource failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update resource")
		return
	}
	writeJSON(w, http.StatusOK, issueResourceToResponse(updated))
}

// DeleteIssueResource handles DELETE /api/issues/{id}/resources/{resourceId}.
func (h *Handler) DeleteIssueResource(w http.ResponseWriter, r *http.Request) {
	resource, ok := h.loadIssueResourceForUser(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteIssueResource(r.Context(), db.DeleteIssueResourceParams{
		ID:          resource.ID,
		WorkspaceID: resource.WorkspaceID,
	}); err != nil {
		slog.Warn("delete issue resource failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete resource")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadIssueResourceForUser resolves {resourceId} within the caller's workspace
// and confirms it belongs to the issue in the path, so a resource id cannot be
// used to reach across issues.
func (h *Handler) loadIssueResourceForUser(
	w http.ResponseWriter,
	r *http.Request,
) (db.IssueResource, bool) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.IssueResource{}, false
	}
	resourceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "resourceId"), "resourceId")
	if !ok {
		return db.IssueResource{}, false
	}
	resource, err := h.Queries.GetIssueResource(r.Context(), db.GetIssueResourceParams{
		ID:          resourceID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "resource not found")
		} else {
			slog.Warn("load issue resource failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load resource")
		}
		return db.IssueResource{}, false
	}
	if resource.IssueID != issue.ID {
		writeError(w, http.StatusNotFound, "resource not found")
		return db.IssueResource{}, false
	}
	return resource, true
}
