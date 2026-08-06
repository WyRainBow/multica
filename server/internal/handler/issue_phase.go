package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssuePhaseResponse is one station in a requirement's life.
//
// A container, not a status. `status` answers "where is this now" and forgets
// everything it passed through; a phase stays, holding what happened while the
// issue was in it.
type IssuePhaseResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
	Name        string `json:"name"`
	Position    int32  `json:"position"`
	// Both derived from transitions rather than typed in — a timestamp
	// somebody has to remember to fill is a timestamp that is wrong.
	EnteredAt   *string `json:"entered_at"`
	CompletedAt *string `json:"completed_at"`
	// How many comments hang under this phase. Present on list reads; the
	// point of the feature is seeing where the discussion actually happened.
	CommentCount int64  `json:"comment_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

const maxPhaseNameLength = 100

// phasePositionStep leaves room to insert a station between two others without
// renumbering their neighbours. Same convention as issue.position.
const phasePositionStep = 1000

func issuePhaseToResponse(p db.IssuePhase, commentCount int64) IssuePhaseResponse {
	return IssuePhaseResponse{
		ID:           uuidToString(p.ID),
		WorkspaceID:  uuidToString(p.WorkspaceID),
		IssueID:      uuidToString(p.IssueID),
		Name:         p.Name,
		Position:     p.Position,
		EnteredAt:    timestampToPtr(p.EnteredAt),
		CompletedAt:  timestampToPtr(p.CompletedAt),
		CommentCount: commentCount,
		CreatedAt:    timestampToString(p.CreatedAt),
		UpdatedAt:    timestampToString(p.UpdatedAt),
	}
}

type createIssuePhaseRequest struct {
	Name string `json:"name"`
	// Omitted appends to the end of the track, which is what adding a station
	// almost always means.
	Position *int32 `json:"position"`
}

// CreateIssuePhase handles POST /api/issues/{id}/phases.
func (h *Handler) CreateIssuePhase(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req createIssuePhaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "phase name must not be blank")
		return
	}
	// Characters, not bytes: these names are Chinese.
	if len([]rune(name)) > maxPhaseNameLength {
		writeError(w, http.StatusBadRequest, "phase name is too long")
		return
	}

	// The route, in track order. Read once and used twice: to place the new
	// station and to refuse a duplicate name.
	existing, err := h.Queries.ListIssuePhases(r.Context(), db.ListIssuePhasesParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("read issue phases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create phase")
		return
	}

	// One station per name. A route with two stations called 实施中 cannot say
	// which one a comment belongs to, and the count on each stops meaning
	// anything. Checked here for a readable refusal; a unique index backs it
	// up so two concurrent creates cannot both slip through.
	for _, phase := range existing {
		if strings.EqualFold(strings.TrimSpace(phase.Name), name) {
			writeError(w, http.StatusConflict,
				"this issue already has a phase named "+phase.Name)
			return
		}
	}

	position := int32(0)
	if req.Position != nil {
		position = *req.Position
	} else {
		position = nextPhasePosition(existing, name)
	}

	phase, err := h.Queries.CreateIssuePhase(r.Context(), db.CreateIssuePhaseParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		Name:        name,
		Position:    position,
	})
	if err != nil {
		// The unique index caught what the check above raced past.
		if strings.Contains(err.Error(), "idx_issue_phase_unique_name") {
			writeError(w, http.StatusConflict, "this issue already has a phase with that name")
			return
		}
		slog.Warn("create issue phase failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create phase")
		return
	}
	writeJSON(w, http.StatusCreated, issuePhaseToResponse(phase, 0))
}

// ListIssuePhases handles GET /api/issues/{id}/phases.
func (h *Handler) ListIssuePhases(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	phases, err := h.Queries.ListIssuePhases(r.Context(), db.ListIssuePhasesParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list issue phases failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list phases")
		return
	}

	// Counts in one query rather than one per phase: a route with eight
	// stations would otherwise be nine round trips to render one header row.
	counts := map[string]int64{}
	rows, err := h.Queries.CountCommentsPerPhase(r.Context(), db.CountCommentsPerPhaseParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		// Best effort: a missing count is a zero on screen, not a failed page.
		slog.Warn("count comments per phase failed",
			append(logger.RequestAttrs(r), "error", err)...)
	} else {
		for _, row := range rows {
			counts[uuidToString(row.PhaseID)] = row.CommentCount
		}
	}

	resp := make([]IssuePhaseResponse, 0, len(phases))
	for _, phase := range phases {
		resp = append(resp, issuePhaseToResponse(phase, counts[uuidToString(phase.ID)]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"phases": resp})
}

// EnterIssuePhase handles POST /api/issues/{id}/phases/{phaseId}/enter.
func (h *Handler) EnterIssuePhase(w http.ResponseWriter, r *http.Request) {
	phase, ok := h.loadIssuePhaseForUser(w, r)
	if !ok {
		return
	}
	updated, err := h.Queries.EnterIssuePhase(r.Context(), db.EnterIssuePhaseParams{
		ID:          phase.ID,
		WorkspaceID: phase.WorkspaceID,
	})
	if err != nil {
		slog.Warn("enter issue phase failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to enter phase")
		return
	}
	writeJSON(w, http.StatusOK, issuePhaseToResponse(updated, 0))
}

// CompleteIssuePhase handles POST /api/issues/{id}/phases/{phaseId}/complete.
func (h *Handler) CompleteIssuePhase(w http.ResponseWriter, r *http.Request) {
	phase, ok := h.loadIssuePhaseForUser(w, r)
	if !ok {
		return
	}
	updated, err := h.Queries.CompleteIssuePhase(r.Context(), db.CompleteIssuePhaseParams{
		ID:          phase.ID,
		WorkspaceID: phase.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The query's WHERE demands entered_at; no row means it was never
		// entered. Completing it would record a route the work never took.
		writeError(w, http.StatusConflict, "phase has not been entered yet")
		return
	}
	if err != nil {
		slog.Warn("complete issue phase failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete phase")
		return
	}
	writeJSON(w, http.StatusOK, issuePhaseToResponse(updated, 0))
}

type updateIssuePhaseRequest struct {
	Name     *string `json:"name"`
	Position *int32  `json:"position"`
}

// UpdateIssuePhase handles PUT /api/issues/{id}/phases/{phaseId}.
func (h *Handler) UpdateIssuePhase(w http.ResponseWriter, r *http.Request) {
	phase, ok := h.loadIssuePhaseForUser(w, r)
	if !ok {
		return
	}
	var req updateIssuePhaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var name pgtype.Text
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "phase name must not be blank")
			return
		}
		if len([]rune(trimmed)) > maxPhaseNameLength {
			writeError(w, http.StatusBadRequest, "phase name is too long")
			return
		}
		name = pgtype.Text{String: trimmed, Valid: true}
	}
	var position pgtype.Int4
	if req.Position != nil {
		position = pgtype.Int4{Int32: *req.Position, Valid: true}
	}

	updated, err := h.Queries.UpdateIssuePhase(r.Context(), db.UpdateIssuePhaseParams{
		ID:          phase.ID,
		WorkspaceID: phase.WorkspaceID,
		Name:        name,
		Position:    position,
	})
	if err != nil {
		slog.Warn("update issue phase failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update phase")
		return
	}
	writeJSON(w, http.StatusOK, issuePhaseToResponse(updated, 0))
}

// DeleteIssuePhase handles DELETE /api/issues/{id}/phases/{phaseId}.
//
// The comments filed under it go with it, and so do their replies. A phase is
// a container: deleting one deletes what it held, the same way deleting a
// folder takes its files.
//
// An earlier version detached them instead, on the reasoning that a discussion
// still happened even if the station is gone. That left orphaned remarks with
// no context, which is worse than not having them — and it made "delete this
// phase" the one destructive action that quietly did not destroy anything.
// Irreversible, so the client confirms first.
func (h *Handler) DeleteIssuePhase(w http.ResponseWriter, r *http.Request) {
	phase, ok := h.loadIssuePhaseForUser(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteCommentsInPhase(r.Context(), db.DeleteCommentsInPhaseParams{
		PhaseID:     phase.ID,
		WorkspaceID: phase.WorkspaceID,
	}); err != nil {
		slog.Warn("delete comments in phase failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete phase")
		return
	}
	if err := h.Queries.DeleteIssuePhase(r.Context(), db.DeleteIssuePhaseParams{
		ID:          phase.ID,
		WorkspaceID: phase.WorkspaceID,
	}); err != nil {
		slog.Warn("delete issue phase failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete phase")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setCommentPhaseRequest struct {
	// Explicit null ungroups the comment; omitting the field is rejected so a
	// malformed body cannot silently detach it.
	PhaseID json.RawMessage `json:"phase_id"`
}

// SetCommentPhase handles PUT /api/comments/{commentId}/phase.
func (h *Handler) SetCommentPhase(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	commentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "commentId")
	if !ok {
		return
	}

	var req setCommentPhaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.PhaseID) == 0 {
		writeError(w, http.StatusBadRequest, "phase_id is required (null to ungroup)")
		return
	}

	var phaseID pgtype.UUID
	if string(req.PhaseID) != "null" {
		var raw string
		if err := json.Unmarshal(req.PhaseID, &raw); err != nil {
			writeError(w, http.StatusBadRequest, "invalid phase_id")
			return
		}
		parsed, ok := parseUUIDOrBadRequest(w, raw, "phase_id")
		if !ok {
			return
		}
		// Confirm the phase is in this workspace before pointing a comment at
		// it — otherwise a comment could be filed under another tenant's
		// station.
		if _, err := h.Queries.GetIssuePhase(r.Context(), db.GetIssuePhaseParams{
			ID:          parsed,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "phase not found in this workspace")
			return
		}
		phaseID = parsed
	}

	updated, err := h.Queries.SetCommentPhase(r.Context(), db.SetCommentPhaseParams{
		ID:          commentID,
		WorkspaceID: wsUUID,
		PhaseID:     phaseID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if err != nil {
		slog.Warn("set comment phase failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to set comment phase")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       uuidToString(updated.ID),
		"phase_id": uuidToPtr(updated.PhaseID),
	})
}

// loadIssuePhaseForUser resolves {phaseId} within the caller's workspace and
// confirms it belongs to the issue in the path, so a phase id cannot be used
// to reach across issues.
func (h *Handler) loadIssuePhaseForUser(
	w http.ResponseWriter,
	r *http.Request,
) (db.IssuePhase, bool) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.IssuePhase{}, false
	}
	phaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "phaseId"), "phaseId")
	if !ok {
		return db.IssuePhase{}, false
	}
	phase, err := h.Queries.GetIssuePhase(r.Context(), db.GetIssuePhaseParams{
		ID:          phaseID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "phase not found")
		} else {
			slog.Warn("load issue phase failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load phase")
		}
		return db.IssuePhase{}, false
	}
	if phase.IssueID != issue.ID {
		writeError(w, http.StatusNotFound, "phase not found")
		return db.IssuePhase{}, false
	}
	return phase, true
}
