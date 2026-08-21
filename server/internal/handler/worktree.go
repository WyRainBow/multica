package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The worktree ledger is the code-progress account: which checkout exists,
// which branch it carries, what it sits on, who is driving it right now, and
// what happened in it round by round.
//
// It deliberately does NOT mirror card state. An issue says how far a piece of
// work has got as a decision; a worktree says where the code is. The two drift
// apart routinely (a card can close while the branch is still open, a branch
// can merge before anyone updates the card) and inferring one from the other is
// how ledgers start lying. Cards are referenced by id from entries, never
// copied into them.
//
// Three accounts, three write paths, on purpose:
//
//   - facts    branch / head / merge SHAs, written only by `worktree sync`
//              running inside the checkout, never typed by hand
//   - session  who is driving and what is next, one slot, edited in place
//   - entries  append-only lines of what happened, never edited

const (
	maxWorktreeNameLength   = 64
	maxWorktreeRefLength    = 500
	maxWorktreeResumeLength = 1000
	maxWorktreeBodyLength   = 2000
	// Entry list windows. A ledger is read from the top; the whole history is
	// available by paging, but no single response carries it.
	defaultWorktreeEntryLimit = 50
	maxWorktreeEntryLimit     = 200
)

var (
	worktreeRoles      = map[string]bool{"base": true, "feature": true, "integration": true, "launch": true}
	worktreeStatuses   = map[string]bool{"active": true, "blocked": true, "merged": true, "archived": true}
	worktreeEntryKinds = map[string]bool{
		"progress": true, "branch": true, "merge": true,
		"blocked": true, "handoff": true, "verify": true,
	}
	// Full 40-character object names only. A short SHA is ambiguous and a
	// branch name is mutable; either would make a merge claim unverifiable
	// later, which is the one property this ledger exists to keep.
	worktreeSHARE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// Anything but whitespace: names carry Chinese as often as ASCII, but the
	// CLI addresses a tree by name as one argument, so a space would make
	// `worktree log my tree "..."` parse as a different command.
	worktreeNameRE = regexp.MustCompile(`^\S+$`)
)

type WorktreeSessionResponse struct {
	Agent      string  `json:"agent"`
	Resume     string  `json:"resume"`
	Owner      string  `json:"owner"`
	NextAction string  `json:"next_action"`
	UpdatedAt  *string `json:"updated_at"`
}

type WorktreeResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	BaseRef     string `json:"base_ref"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	HeadSHA     string `json:"head_sha"`
	MergedSHA   string `json:"merged_sha"`
	MergedInto  string `json:"merged_into"`
	Dirty       bool   `json:"dirty"`
	// When the repo last confirmed the facts above, as opposed to when someone
	// last claimed them. A row with an old verified_at is visibly stale rather
	// than quietly wrong.
	VerifiedAt *string                 `json:"verified_at"`
	Session    WorktreeSessionResponse `json:"session"`
	ParentID   *string                 `json:"parent_id"`
	EntryCount int64                   `json:"entry_count"`
	CreatedAt  string                  `json:"created_at"`
	UpdatedAt  string                  `json:"updated_at"`
}

type WorktreeEntryResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	WorktreeID  string  `json:"worktree_id"`
	IssueID     *string `json:"issue_id"`
	Kind        string  `json:"kind"`
	Body        string  `json:"body"`
	SHA         string  `json:"sha"`
	AuthorType  string  `json:"author_type"`
	AuthorID    string  `json:"author_id"`
	CreatedAt   string  `json:"created_at"`
}

func worktreeToResponse(t db.Worktree, entryCount int64) WorktreeResponse {
	return WorktreeResponse{
		ID:          uuidToString(t.ID),
		WorkspaceID: uuidToString(t.WorkspaceID),
		Name:        t.Name,
		Path:        t.Path,
		Repo:        t.Repo,
		Branch:      t.Branch,
		BaseRef:     t.BaseRef,
		Role:        t.Role,
		Status:      t.Status,
		HeadSHA:     t.HeadSha,
		MergedSHA:   t.MergedSha,
		MergedInto:  t.MergedInto,
		Dirty:       t.Dirty,
		VerifiedAt:  timestampToPtr(t.VerifiedAt),
		Session: WorktreeSessionResponse{
			Agent:      t.SessionAgent,
			Resume:     t.SessionResume,
			Owner:      t.SessionOwner,
			NextAction: t.NextAction,
			UpdatedAt:  timestampToPtr(t.SessionUpdatedAt),
		},
		ParentID:   uuidToPtr(t.ParentID),
		EntryCount: entryCount,
		CreatedAt:  timestampToString(t.CreatedAt),
		UpdatedAt:  timestampToString(t.UpdatedAt),
	}
}

func worktreeEntryToResponse(e db.WorktreeEntry) WorktreeEntryResponse {
	return WorktreeEntryResponse{
		ID:          uuidToString(e.ID),
		WorkspaceID: uuidToString(e.WorkspaceID),
		WorktreeID:  uuidToString(e.WorktreeID),
		IssueID:     uuidToPtr(e.IssueID),
		Kind:        e.Kind,
		Body:        e.Body,
		SHA:         e.Sha,
		AuthorType:  e.AuthorType,
		AuthorID:    uuidToString(e.AuthorID),
		CreatedAt:   timestampToString(e.CreatedAt),
	}
}

// --- validation ---

func validateWorktreeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len([]rune(name)) > maxWorktreeNameLength {
		return "", errors.New("name is too long")
	}
	if !worktreeNameRE.MatchString(name) {
		return "", errors.New("name cannot contain spaces")
	}
	return name, nil
}

func validateWorktreeRef(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) > maxWorktreeRefLength {
		return "", errors.New(field + " is too long")
	}
	return value, nil
}

// validateWorktreeSHA accepts a full object name or nothing at all. Empty is a
// real state ("not merged yet"); a half-remembered short SHA is not.
func validateWorktreeSHA(field, raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", nil
	}
	if !worktreeSHARE.MatchString(value) {
		return "", errors.New(field + " must be a full 40-character commit SHA")
	}
	return value, nil
}

// --- loaders ---

// loadWorktreeForUser resolves {id} inside the caller's workspace. The path
// segment is either a UUID or the tree's name, because the CLI and the agents
// driving it address trees the way a person does — by name. Every write below
// uses the resolved row's ID, never the raw path value.
func (h *Handler) loadWorktreeForUser(w http.ResponseWriter, r *http.Request) (db.Worktree, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Worktree{}, false
	}
	raw := chi.URLParam(r, "id")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "worktree id is required")
		return db.Worktree{}, false
	}

	if id, err := util.ParseUUID(raw); err == nil {
		tree, err := h.Queries.GetWorktree(r.Context(), db.GetWorktreeParams{ID: id, WorkspaceID: wsUUID})
		if err == nil {
			return tree, true
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("load worktree failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load worktree")
			return db.Worktree{}, false
		}
		// Fall through: a name that happens to look like a UUID is still a name.
	}

	tree, err := h.Queries.GetWorktreeByName(r.Context(), db.GetWorktreeByNameParams{
		WorkspaceID: wsUUID,
		Name:        raw,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "worktree not found")
		} else {
			slog.Warn("load worktree by name failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load worktree")
		}
		return db.Worktree{}, false
	}
	return tree, true
}

func (h *Handler) worktreeEntryCounts(ctx context.Context, wsUUID pgtype.UUID) map[string]int64 {
	counts := make(map[string]int64)
	rows, err := h.Queries.CountWorktreeEntries(ctx, wsUUID)
	if err != nil {
		// A missing count degrades the badge, not the page.
		return counts
	}
	for _, row := range rows {
		counts[uuidToString(row.WorktreeID)] = row.EntryCount
	}
	return counts
}

// --- worktree CRUD ---

// ListWorktrees handles GET /api/worktrees.
func (h *Handler) ListWorktrees(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	trees, err := h.Queries.ListWorktrees(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("list worktrees failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list worktrees")
		return
	}
	counts := h.worktreeEntryCounts(r.Context(), wsUUID)
	resp := make([]WorktreeResponse, 0, len(trees))
	for _, tree := range trees {
		resp = append(resp, worktreeToResponse(tree, counts[uuidToString(tree.ID)]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"worktrees": resp})
}

// GetWorktree handles GET /api/worktrees/{id}.
func (h *Handler) GetWorktree(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	counts := h.worktreeEntryCounts(r.Context(), tree.WorkspaceID)
	writeJSON(w, http.StatusOK, worktreeToResponse(tree, counts[uuidToString(tree.ID)]))
}

type createWorktreeRequest struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Repo     string  `json:"repo"`
	Branch   string  `json:"branch"`
	BaseRef  string  `json:"base_ref"`
	Role     string  `json:"role"`
	Status   string  `json:"status"`
	ParentID *string `json:"parent_id"`
}

// CreateWorktree handles POST /api/worktrees.
func (h *Handler) CreateWorktree(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var req createWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, err := validateWorktreeName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	refs := map[string]string{"path": req.Path, "repo": req.Repo, "branch": req.Branch, "base_ref": req.BaseRef}
	for field, raw := range refs {
		value, err := validateWorktreeRef(field, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		refs[field] = value
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "feature"
	}
	if !worktreeRoles[role] {
		writeError(w, http.StatusBadRequest, "role must be one of base, feature, integration, launch")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	if !worktreeStatuses[status] {
		writeError(w, http.StatusBadRequest, "status must be one of active, blocked, merged, archived")
		return
	}
	parentID, ok := h.resolveWorktreeParent(w, r, wsUUID, req.ParentID, pgtype.UUID{})
	if !ok {
		return
	}

	tree, err := h.Queries.CreateWorktree(r.Context(), db.CreateWorktreeParams{
		WorkspaceID: wsUUID,
		Name:        name,
		Path:        refs["path"],
		Repo:        refs["repo"],
		Branch:      refs["branch"],
		BaseRef:     refs["base_ref"],
		Role:        role,
		Status:      status,
		ParentID:    parentID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a worktree with that name already exists")
			return
		}
		slog.Warn("create worktree failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create worktree")
		return
	}
	writeJSON(w, http.StatusCreated, worktreeToResponse(tree, 0))
}

type updateWorktreeRequest struct {
	Name     *string `json:"name"`
	Path     *string `json:"path"`
	Repo     *string `json:"repo"`
	Branch   *string `json:"branch"`
	BaseRef  *string `json:"base_ref"`
	Role     *string `json:"role"`
	Status   *string `json:"status"`
	ParentID *string `json:"parent_id"`
}

// UpdateWorktree handles PUT /api/worktrees/{id}.
func (h *Handler) UpdateWorktree(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req updateWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorktreeParams{ID: tree.ID, WorkspaceID: tree.WorkspaceID}
	if req.Name != nil {
		name, err := validateWorktreeName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	// A present-but-empty ref clears it, which is a real intent ("this tree has
	// no base yet"), so validity tracks the pointer rather than the string.
	optionalRefs := []struct {
		field string
		value *string
		dest  *pgtype.Text
	}{
		{"path", req.Path, &params.Path},
		{"repo", req.Repo, &params.Repo},
		{"branch", req.Branch, &params.Branch},
		{"base_ref", req.BaseRef, &params.BaseRef},
	}
	for _, ref := range optionalRefs {
		if ref.value == nil {
			continue
		}
		value, err := validateWorktreeRef(ref.field, *ref.value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		*ref.dest = pgtype.Text{String: value, Valid: true}
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		if !worktreeRoles[role] {
			writeError(w, http.StatusBadRequest, "role must be one of base, feature, integration, launch")
			return
		}
		params.Role = pgtype.Text{String: role, Valid: true}
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !worktreeStatuses[status] {
			writeError(w, http.StatusBadRequest, "status must be one of active, blocked, merged, archived")
			return
		}
		params.Status = pgtype.Text{String: status, Valid: true}
	}
	if req.ParentID != nil {
		if strings.TrimSpace(*req.ParentID) == "" {
			params.ClearParent = true
		} else {
			parentID, ok := h.resolveWorktreeParent(w, r, tree.WorkspaceID, req.ParentID, tree.ID)
			if !ok {
				return
			}
			params.ParentID = parentID
		}
	}

	updated, err := h.Queries.UpdateWorktree(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a worktree with that name already exists")
			return
		}
		slog.Warn("update worktree failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update worktree")
		return
	}
	counts := h.worktreeEntryCounts(r.Context(), updated.WorkspaceID)
	writeJSON(w, http.StatusOK, worktreeToResponse(updated, counts[uuidToString(updated.ID)]))
}

// DeleteWorktree handles DELETE /api/worktrees/{id}. Entries go with it, in one
// transaction: this schema has no cascading deletes, so the parent operation
// owns the cleanup or the rows are orphaned.
func (h *Handler) DeleteWorktree(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete worktree")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteWorktreeEntriesForWorktree(r.Context(), db.DeleteWorktreeEntriesForWorktreeParams{
		WorktreeID:  tree.ID,
		WorkspaceID: tree.WorkspaceID,
	}); err != nil {
		slog.Warn("delete worktree entries failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete worktree")
		return
	}
	if err := qtx.DeleteWorktree(r.Context(), db.DeleteWorktreeParams{
		ID:          tree.ID,
		WorkspaceID: tree.WorkspaceID,
	}); err != nil {
		slog.Warn("delete worktree failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete worktree")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit worktree delete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete worktree")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveWorktreeParent validates a parent pointer: it must name a tree in the
// same workspace, and following it upwards must not arrive back at self. A
// cycle here would hang every renderer that walks the stack, and the walk is
// short enough that checking it on write costs nothing.
func (h *Handler) resolveWorktreeParent(
	w http.ResponseWriter,
	r *http.Request,
	wsUUID pgtype.UUID,
	raw *string,
	selfID pgtype.UUID,
) (pgtype.UUID, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return pgtype.UUID{}, true
	}
	parentID, err := util.ParseUUID(strings.TrimSpace(*raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "parent_id must be a UUID")
		return pgtype.UUID{}, false
	}
	if selfID.Valid && parentID == selfID {
		writeError(w, http.StatusBadRequest, "a worktree cannot be its own parent")
		return pgtype.UUID{}, false
	}

	// Walk up from the proposed parent. Bounded by the number of trees in the
	// workspace; a pre-existing cycle would otherwise spin here.
	cursor := parentID
	for hops := 0; cursor.Valid && hops < 64; hops++ {
		parent, err := h.Queries.GetWorktree(r.Context(), db.GetWorktreeParams{ID: cursor, WorkspaceID: wsUUID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "parent worktree not found")
				return pgtype.UUID{}, false
			}
			slog.Warn("resolve worktree parent failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to resolve parent worktree")
			return pgtype.UUID{}, false
		}
		if selfID.Valid && parent.ParentID == selfID {
			writeError(w, http.StatusBadRequest, "parent_id would create a cycle")
			return pgtype.UUID{}, false
		}
		cursor = parent.ParentID
	}
	return parentID, true
}

// --- session slot ---

type updateWorktreeSessionRequest struct {
	Agent      *string `json:"agent"`
	Resume     *string `json:"resume"`
	Owner      *string `json:"owner"`
	NextAction *string `json:"next_action"`
}

// UpdateWorktreeSession handles PUT /api/worktrees/{id}/session — the
// navigation account: which session is driving this tree and what it is waiting
// on. One slot per tree, overwritten in place. It replaces the pinned per-issue
// session comment, whose problem was never the content but the copies: the same
// pointer restated on every card, going stale card by card.
func (h *Handler) UpdateWorktreeSession(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req updateWorktreeSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateWorktreeSessionParams{ID: tree.ID, WorkspaceID: tree.WorkspaceID}
	fields := []struct {
		name  string
		max   int
		value *string
		dest  *pgtype.Text
	}{
		{"agent", maxWorktreeNameLength, req.Agent, &params.SessionAgent},
		{"resume", maxWorktreeResumeLength, req.Resume, &params.SessionResume},
		{"owner", maxWorktreeNameLength, req.Owner, &params.SessionOwner},
		{"next_action", maxWorktreeRefLength, req.NextAction, &params.NextAction},
	}
	touched := false
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		value := strings.TrimSpace(*f.value)
		if len([]rune(value)) > f.max {
			writeError(w, http.StatusBadRequest, f.name+" is too long")
			return
		}
		*f.dest = pgtype.Text{String: value, Valid: true}
		touched = true
	}
	if !touched {
		writeError(w, http.StatusBadRequest, "nothing to update; send agent, resume, owner or next_action")
		return
	}

	updated, err := h.Queries.UpdateWorktreeSession(r.Context(), params)
	if err != nil {
		slog.Warn("update worktree session failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}
	counts := h.worktreeEntryCounts(r.Context(), updated.WorkspaceID)
	writeJSON(w, http.StatusOK, worktreeToResponse(updated, counts[uuidToString(updated.ID)]))
}

// --- machine facts ---

type syncWorktreeRequest struct {
	Branch     *string `json:"branch"`
	HeadSHA    *string `json:"head_sha"`
	MergedSHA  *string `json:"merged_sha"`
	MergedInto *string `json:"merged_into"`
	Dirty      *bool   `json:"dirty"`
	Status     *string `json:"status"`
}

// SyncWorktree handles POST /api/worktrees/{id}/sync — the facts account,
// posted by the CLI from inside the checkout after asking git. Kept separate
// from the generic update so the provenance of these fields stays legible: they
// are measurements, and verified_at records when the measurement was taken.
func (h *Handler) SyncWorktree(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req syncWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.SyncWorktreeFactsParams{ID: tree.ID, WorkspaceID: tree.WorkspaceID}
	if req.Branch != nil {
		branch, err := validateWorktreeRef("branch", *req.Branch)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Branch = pgtype.Text{String: branch, Valid: true}
	}
	shas := []struct {
		field string
		value *string
		dest  *pgtype.Text
	}{
		{"head_sha", req.HeadSHA, &params.HeadSha},
		{"merged_sha", req.MergedSHA, &params.MergedSha},
	}
	for _, s := range shas {
		if s.value == nil {
			continue
		}
		value, err := validateWorktreeSHA(s.field, *s.value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		*s.dest = pgtype.Text{String: value, Valid: true}
	}
	if req.MergedInto != nil {
		mergedInto, err := validateWorktreeRef("merged_into", *req.MergedInto)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.MergedInto = pgtype.Text{String: mergedInto, Valid: true}
	}
	if req.Dirty != nil {
		params.Dirty = pgtype.Bool{Bool: *req.Dirty, Valid: true}
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !worktreeStatuses[status] {
			writeError(w, http.StatusBadRequest, "status must be one of active, blocked, merged, archived")
			return
		}
		params.Status = pgtype.Text{String: status, Valid: true}
	}

	updated, err := h.Queries.SyncWorktreeFacts(r.Context(), params)
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "commit SHAs must be full 40-character object names")
			return
		}
		slog.Warn("sync worktree failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to sync worktree")
		return
	}
	counts := h.worktreeEntryCounts(r.Context(), updated.WorkspaceID)
	writeJSON(w, http.StatusOK, worktreeToResponse(updated, counts[uuidToString(updated.ID)]))
}

// --- entries ---

func worktreeEntryLimit(raw string) int32 {
	if raw == "" {
		return defaultWorktreeEntryLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultWorktreeEntryLimit
	}
	if n > maxWorktreeEntryLimit {
		return maxWorktreeEntryLimit
	}
	return int32(n)
}

// ListWorktreeEntries handles GET /api/worktrees/{id}/entries.
func (h *Handler) ListWorktreeEntries(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorktreeEntries(r.Context(), db.ListWorktreeEntriesParams{
		WorkspaceID: tree.WorkspaceID,
		WorktreeID:  tree.ID,
		Limit:       worktreeEntryLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		slog.Warn("list worktree entries failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list entries")
		return
	}
	resp := make([]WorktreeEntryResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, worktreeEntryToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": resp})
}

// ListRecentWorktreeEntries handles GET /api/worktree-entries — the
// workspace-wide feed, so the ledger page can open on what moved lately without
// fanning out one request per tree.
func (h *Handler) ListRecentWorktreeEntries(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListRecentWorktreeEntries(r.Context(), db.ListRecentWorktreeEntriesParams{
		WorkspaceID: wsUUID,
		Limit:       worktreeEntryLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		slog.Warn("list recent worktree entries failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list entries")
		return
	}
	resp := make([]WorktreeEntryResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, worktreeEntryToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": resp})
}

type createWorktreeEntryRequest struct {
	Kind    string  `json:"kind"`
	Body    string  `json:"body"`
	SHA     string  `json:"sha"`
	IssueID *string `json:"issue_id"`
}

// CreateWorktreeEntry handles POST /api/worktrees/{id}/entries.
//
// Append only. There is no update or delete twin, and that is the feature: a
// round of work recorded here cannot be tidied out of the history later, so the
// ledger stays worth reading. A line written in error is corrected by writing
// the correction.
func (h *Handler) CreateWorktreeEntry(w http.ResponseWriter, r *http.Request) {
	tree, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req createWorktreeEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "progress"
	}
	if !worktreeEntryKinds[kind] {
		writeError(w, http.StatusBadRequest, "kind must be one of progress, branch, merge, blocked, handoff, verify")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	if len([]rune(body)) > maxWorktreeBodyLength {
		writeError(w, http.StatusBadRequest, "body is too long")
		return
	}
	sha, err := validateWorktreeSHA("sha", req.SHA)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The card an entry refers to is resolved through the issue loader so a
	// human identifier (COC-295) works here the way it does everywhere else,
	// and so an id from another workspace cannot be attached.
	var issueID pgtype.UUID
	if req.IssueID != nil && strings.TrimSpace(*req.IssueID) != "" {
		issue, ok := h.loadIssueForUser(w, r, strings.TrimSpace(*req.IssueID))
		if !ok {
			return
		}
		issueID = issue.ID
	}

	workspaceID := uuidToString(tree.WorkspaceID)
	authorType, authorID := h.resolveActor(r, requestUserID(r), workspaceID)
	entry, err := h.Queries.CreateWorktreeEntry(r.Context(), db.CreateWorktreeEntryParams{
		WorkspaceID: tree.WorkspaceID,
		WorktreeID:  tree.ID,
		IssueID:     issueID,
		Kind:        kind,
		Body:        body,
		Sha:         sha,
		AuthorType:  authorType,
		AuthorID:    parseUUID(authorID),
	})
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "sha must be a full 40-character commit SHA")
			return
		}
		slog.Warn("create worktree entry failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add entry")
		return
	}
	writeJSON(w, http.StatusCreated, worktreeEntryToResponse(entry))
}
