package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/progressledger"
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
// how ledgers start lying.
//
// Three accounts, three write paths, on purpose:
//
//   - facts    branch / head / merge SHAs, written only by `worktree sync`
//              running inside the checkout, never typed by hand
//   - session  who is driving and what is next, one slot, edited in place
//   - entries  append-only lines of what happened, never edited
//
// Storage lives in a git repository of YAML files, not in this database — see
// internal/progressledger for why. The HTTP shapes below are unchanged by that
// move, which is the point: the CLI, the hooks and the UI all kept working
// across it.

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
	// discussion is a role a checkout cannot have: it is an account for a card
	// that produced a decision rather than a branch. Those used to leave no
	// trace at all, because the ledger only accepted rows that named a tree.
	worktreeRoles = map[string]bool{
		"base": true, "feature": true, "integration": true,
		"release": true, "hotfix": true, "discussion": true,
	}
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

// ledger returns the store, constructing the default one on first use so a
// Handler built without the field (tests, older wiring) still works.
func (h *Handler) ledger() *progressledger.Store {
	if h.ProgressLedger == nil {
		h.ProgressLedger = progressledger.NewStore("")
	}
	return h.ProgressLedger
}

type WorktreeSessionResponse struct {
	Agent     string `json:"agent"`
	Resume    string `json:"resume"`
	Owner     string `json:"owner"`
	SessionID string `json:"session_id"`
	// WaitingForHuman is recorded by whoever stopped, never derived. See
	// progressledger.Session.
	WaitingForHuman bool    `json:"waiting_for_human"`
	NextAction      string  `json:"next_action"`
	UpdatedAt       *string `json:"updated_at"`
}

type WorktreeResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	// Issue is the card this account belongs to, as the identifier a person
	// reads (COC-348). IssueID is the same card as a UUID, for the UI to link
	// with. Either may be empty: an account can predate its card.
	Issue      string `json:"issue"`
	IssueID    string `json:"issue_id"`
	Path       string `json:"path"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	BaseRef    string `json:"base_ref"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	HeadSHA    string `json:"head_sha"`
	MergedSHA  string `json:"merged_sha"`
	MergedInto string `json:"merged_into"`
	Dirty      bool   `json:"dirty"`
	// When the repo last confirmed the facts above, as opposed to when someone
	// last claimed them. A row with an old verified_at is visibly stale rather
	// than quietly wrong.
	VerifiedAt *string                 `json:"verified_at"`
	Session    WorktreeSessionResponse `json:"session"`
	DependsOn  []string                `json:"depends_on"`
	Artifacts  []string                `json:"artifacts"`
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
	Issue       string  `json:"issue"`
	Kind        string  `json:"kind"`
	Body        string  `json:"body"`
	SHA         string  `json:"sha"`
	AuthorType  string  `json:"author_type"`
	AuthorID    string  `json:"author_id"`
	CreatedAt   string  `json:"created_at"`
}

func ledgerTimePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func ledgerTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func worktreeToResponse(rec progressledger.Record) WorktreeResponse {
	var parent *string
	if rec.Parent != "" {
		p := rec.Parent
		parent = &p
	}
	depends := rec.DependsOn
	if depends == nil {
		depends = []string{}
	}
	artifacts := rec.Artifacts
	if artifacts == nil {
		artifacts = []string{}
	}
	return WorktreeResponse{
		ID:          rec.Key,
		WorkspaceID: rec.WorkspaceID,
		Name:        rec.Name,
		Issue:       rec.Issue,
		IssueID:     rec.IssueID,
		Path:        rec.Path,
		Repo:        rec.Repo,
		Branch:      rec.Branch,
		BaseRef:     rec.BaseRef,
		Role:        rec.Role,
		Status:      rec.Status,
		HeadSHA:     rec.Facts.HeadSHA,
		MergedSHA:   rec.Facts.MergedSHA,
		MergedInto:  rec.Facts.MergedInto,
		Dirty:       rec.Facts.Dirty,
		VerifiedAt:  ledgerTimePtr(rec.Facts.VerifiedAt),
		Session: WorktreeSessionResponse{
			Agent:           rec.Session.Agent,
			Resume:          rec.Session.Resume,
			Owner:           rec.Session.Owner,
			SessionID:       rec.Session.SessionID,
			WaitingForHuman: rec.Session.WaitingForHuman,
			NextAction:      rec.Session.NextAction,
			UpdatedAt:       ledgerTimePtr(rec.Session.UpdatedAt),
		},
		DependsOn:  depends,
		Artifacts:  artifacts,
		ParentID:   parent,
		EntryCount: int64(len(rec.Log)),
		CreatedAt:  ledgerTime(rec.CreatedAt),
		UpdatedAt:  ledgerTime(rec.UpdatedAt),
	}
}

func worktreeEntryToResponse(rec progressledger.Record, index int) WorktreeEntryResponse {
	e := rec.Log[index]
	var issueID *string
	if e.IssueID != "" {
		id := e.IssueID
		issueID = &id
	}
	return WorktreeEntryResponse{
		ID:          rec.EntryID(index),
		WorkspaceID: rec.WorkspaceID,
		WorktreeID:  rec.Key,
		IssueID:     issueID,
		Issue:       e.Issue,
		Kind:        e.Kind,
		Body:        e.Body,
		SHA:         e.SHA,
		AuthorType:  e.AuthorType,
		AuthorID:    e.AuthorID,
		CreatedAt:   ledgerTime(e.At),
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
// segment is the account key, its addressable name, or the card identifier it
// belongs to, because the CLI and the agents driving it address accounts the
// way a person does.
func (h *Handler) loadWorktreeForUser(w http.ResponseWriter, r *http.Request) (progressledger.Record, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	raw := strings.TrimSpace(chi.URLParam(r, "id"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "worktree id is required")
		return progressledger.Record{}, false
	}
	rec, err := h.ledger().Find(workspaceID, raw)
	if err != nil {
		if errors.Is(err, progressledger.ErrNotFound) {
			writeError(w, http.StatusNotFound, "worktree not found")
		} else {
			slog.Warn("load worktree failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load worktree")
		}
		return progressledger.Record{}, false
	}
	return rec, true
}

// writeLedgerError turns a store failure into a response. A missing ledger
// directory is a 503 with the path in it, not a 500: the operator can fix it,
// and telling them where it looked is the whole fix.
func writeLedgerError(w http.ResponseWriter, r *http.Request, action string, err error) {
	if errors.Is(err, progressledger.ErrUnavailable) {
		writeError(w, http.StatusServiceUnavailable,
			"the progress ledger repository is not on this machine: "+err.Error())
		return
	}
	slog.Warn(action+" failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to "+action)
}

// --- worktree CRUD ---

// ListWorktrees handles GET /api/worktrees.
func (h *Handler) ListWorktrees(w http.ResponseWriter, r *http.Request) {
	records, err := h.ledger().List(h.resolveWorkspaceID(r))
	if err != nil {
		writeLedgerError(w, r, "list worktrees", err)
		return
	}
	resp := make([]WorktreeResponse, 0, len(records))
	for _, rec := range records {
		resp = append(resp, worktreeToResponse(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"worktrees": resp})
}

// GetWorktree handles GET /api/worktrees/{id}.
func (h *Handler) GetWorktree(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, worktreeToResponse(rec))
}

type createWorktreeRequest struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Repo      string   `json:"repo"`
	Branch    string   `json:"branch"`
	BaseRef   string   `json:"base_ref"`
	Role      string   `json:"role"`
	Status    string   `json:"status"`
	Issue     string   `json:"issue"`
	DependsOn []string `json:"depends_on"`
	ParentID  *string  `json:"parent_id"`
}

// CreateWorktree handles POST /api/worktrees.
//
// One account per card. When the card already has one, this adopts it —
// repointing the branch and recording the change — instead of opening a second
// account for the same work. Two accounts for one card is how `coc-341` and
// `coc-341-tab` ended up telling different stories about the same card.
func (h *Handler) CreateWorktree(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
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
		writeError(w, http.StatusBadRequest, "role must be one of base, feature, integration, release, hotfix, discussion")
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
	parent, ok := h.resolveWorktreeParent(w, r, workspaceID, req.ParentID, "")
	if !ok {
		return
	}

	// The card comes from the request, then from the branch, then from the
	// name. Deriving it is worth the guess: the alternative is an account that
	// no card points at, which is invisible on the page where it matters.
	identifier := strings.ToUpper(strings.TrimSpace(req.Issue))
	if identifier == "" {
		identifier = progressledger.GuessIssueIdentifier(refs["branch"])
	}
	if identifier == "" {
		identifier = progressledger.GuessIssueIdentifier(name)
	}
	issueUUID := ""
	if identifier != "" {
		if issue, found := h.lookupIssueQuietly(r, identifier); found {
			identifier = h.issueIdentifier(r.Context(), issue)
			issueUUID = uuidToString(issue.ID)
		}
	}

	store := h.ledger()
	var rec progressledger.Record
	adopted := false
	if identifier != "" {
		if existing, err := store.FindByIssue(workspaceID, identifier); err == nil {
			rec = existing
			adopted = true
		}
	}
	if !adopted {
		if _, err := store.FindByName(workspaceID, name); err == nil {
			writeError(w, http.StatusConflict, "a worktree with that name already exists")
			return
		}
		key := identifier
		if key == "" {
			key = name
		}
		rec = progressledger.Record{Key: progressledger.SanitizeKey(key)}
		if rec.Key == "" {
			writeError(w, http.StatusBadRequest, "name cannot be used as a ledger key")
			return
		}
	}

	rec.WorkspaceID = workspaceID
	rec.Name = name
	rec.Issue = identifier
	rec.IssueID = issueUUID
	rec.Path = refs["path"]
	rec.Repo = refs["repo"]
	rec.Branch = refs["branch"]
	rec.BaseRef = refs["base_ref"]
	rec.Role = role
	rec.Status = status
	rec.Parent = parent
	if req.DependsOn != nil {
		rec.DependsOn = normalizeDependsOn(req.DependsOn)
	}

	authorType, authorID := h.resolveActor(r, requestUserID(r), workspaceID)
	verb := "registered"
	if adopted {
		verb = "re-pointed"
	}
	rec.Log = append(rec.Log, progressledger.Entry{
		At:         time.Now(),
		Kind:       "branch",
		Body:       verb + " " + name + " on " + firstNonEmpty(refs["branch"], "(no branch)"),
		Issue:      identifier,
		IssueID:    issueUUID,
		AuthorType: authorType,
		AuthorID:   authorID,
	})

	if err := store.Save(&rec, "progress("+rec.Key+"): "+verb+" "+name); err != nil {
		writeLedgerError(w, r, "create worktree", err)
		return
	}
	code := http.StatusCreated
	if adopted {
		code = http.StatusOK
	}
	writeJSON(w, code, worktreeToResponse(rec))
}

type updateWorktreeRequest struct {
	Name      *string   `json:"name"`
	Path      *string   `json:"path"`
	Repo      *string   `json:"repo"`
	Branch    *string   `json:"branch"`
	BaseRef   *string   `json:"base_ref"`
	Role      *string   `json:"role"`
	Status    *string   `json:"status"`
	Issue     *string   `json:"issue"`
	DependsOn *[]string `json:"depends_on"`
	Artifacts *[]string `json:"artifacts"`
	ParentID  *string   `json:"parent_id"`
}

// UpdateWorktree handles PUT /api/worktrees/{id}.
func (h *Handler) UpdateWorktree(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req updateWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	if req.Name != nil {
		name, err := validateWorktreeName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !strings.EqualFold(name, rec.Name) {
			if _, err := h.ledger().FindByName(workspaceID, name); err == nil {
				writeError(w, http.StatusConflict, "a worktree with that name already exists")
				return
			}
		}
		rec.Name = name
	}
	// A present-but-empty ref clears it, which is a real intent ("this tree has
	// no base yet"), so the pointer decides whether the field was sent at all.
	optionalRefs := []struct {
		field string
		value *string
		dest  *string
	}{
		{"path", req.Path, &rec.Path},
		{"repo", req.Repo, &rec.Repo},
		{"branch", req.Branch, &rec.Branch},
		{"base_ref", req.BaseRef, &rec.BaseRef},
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
		*ref.dest = value
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		if !worktreeRoles[role] {
			writeError(w, http.StatusBadRequest, "role must be one of base, feature, integration, release, hotfix, discussion")
			return
		}
		rec.Role = role
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !worktreeStatuses[status] {
			writeError(w, http.StatusBadRequest, "status must be one of active, blocked, merged, archived")
			return
		}
		rec.Status = status
	}
	if req.Issue != nil {
		identifier := strings.ToUpper(strings.TrimSpace(*req.Issue))
		rec.Issue = identifier
		rec.IssueID = ""
		if identifier != "" {
			if issue, found := h.lookupIssueQuietly(r, identifier); found {
				rec.Issue = h.issueIdentifier(r.Context(), issue)
				rec.IssueID = uuidToString(issue.ID)
			}
		}
	}
	if req.DependsOn != nil {
		rec.DependsOn = normalizeDependsOn(*req.DependsOn)
	}
	if req.Artifacts != nil {
		rec.Artifacts = normalizeDependsOn(*req.Artifacts)
	}
	if req.ParentID != nil {
		if strings.TrimSpace(*req.ParentID) == "" {
			rec.Parent = ""
		} else {
			parent, ok := h.resolveWorktreeParent(w, r, workspaceID, req.ParentID, rec.Key)
			if !ok {
				return
			}
			rec.Parent = parent
		}
	}

	if err := h.ledger().Save(&rec, "progress("+rec.Key+"): update"); err != nil {
		writeLedgerError(w, r, "update worktree", err)
		return
	}
	writeJSON(w, http.StatusOK, worktreeToResponse(rec))
}

// DeleteWorktree handles DELETE /api/worktrees/{id}. The whole account goes,
// log included — which is why the documented way to finish a tree is to archive
// it, not to remove it.
func (h *Handler) DeleteWorktree(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	if err := h.ledger().Remove(h.resolveWorkspaceID(r), rec.Key, "progress("+rec.Key+"): removed"); err != nil {
		writeLedgerError(w, r, "delete worktree", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveWorktreeParent validates a parent pointer: it must name an account in
// the same workspace, and following it upwards must not arrive back at self. A
// cycle here would hang every renderer that walks the stack, and the walk is
// short enough that checking it on write costs nothing.
func (h *Handler) resolveWorktreeParent(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	raw *string,
	selfKey string,
) (string, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", true
	}
	store := h.ledger()
	parent, err := store.Find(workspaceID, strings.TrimSpace(*raw))
	if err != nil {
		if errors.Is(err, progressledger.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "parent worktree not found")
		} else {
			writeLedgerError(w, r, "resolve parent worktree", err)
		}
		return "", false
	}
	if selfKey != "" && parent.Key == selfKey {
		writeError(w, http.StatusBadRequest, "a worktree cannot be its own parent")
		return "", false
	}

	// Walk up from the proposed parent. Bounded by hops rather than by trust: a
	// cycle already in the files would otherwise spin here forever.
	cursor := parent.Parent
	for hops := 0; cursor != "" && hops < 64; hops++ {
		if selfKey != "" && cursor == selfKey {
			writeError(w, http.StatusBadRequest, "parent_id would create a cycle")
			return "", false
		}
		next, err := store.Find(workspaceID, cursor)
		if err != nil {
			break
		}
		cursor = next.Parent
	}
	return parent.Key, true
}

// --- session slot ---

type updateWorktreeSessionRequest struct {
	Agent           *string `json:"agent"`
	Resume          *string `json:"resume"`
	Owner           *string `json:"owner"`
	SessionID       *string `json:"session_id"`
	NextAction      *string `json:"next_action"`
	WaitingForHuman *bool   `json:"waiting_for_human"`
}

// UpdateWorktreeSession handles PUT /api/worktrees/{id}/session — the
// navigation account: which session is driving this tree and what it is waiting
// on. One slot per tree, overwritten in place. It replaces the pinned per-issue
// session comment, whose problem was never the content but the copies: the same
// pointer restated on every card, going stale card by card.
func (h *Handler) UpdateWorktreeSession(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req updateWorktreeSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	fields := []struct {
		name  string
		max   int
		value *string
		dest  *string
	}{
		{"agent", maxWorktreeNameLength, req.Agent, &rec.Session.Agent},
		{"resume", maxWorktreeResumeLength, req.Resume, &rec.Session.Resume},
		{"owner", maxWorktreeNameLength, req.Owner, &rec.Session.Owner},
		{"session_id", maxWorktreeRefLength, req.SessionID, &rec.Session.SessionID},
		{"next_action", maxWorktreeRefLength, req.NextAction, &rec.Session.NextAction},
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
		*f.dest = value
		touched = true
	}
	if req.WaitingForHuman != nil {
		rec.Session.WaitingForHuman = *req.WaitingForHuman
		touched = true
	}
	if !touched {
		writeError(w, http.StatusBadRequest,
			"nothing to update; send agent, resume, owner, session_id, next_action or waiting_for_human")
		return
	}
	now := time.Now()
	rec.Session.UpdatedAt = &now

	if err := h.ledger().Save(&rec, "session("+rec.Key+"): "+firstNonEmpty(rec.Session.Agent, "update")); err != nil {
		writeLedgerError(w, r, "update session", err)
		return
	}
	writeJSON(w, http.StatusOK, worktreeToResponse(rec))
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
	rec, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	var req syncWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Branch != nil {
		branch, err := validateWorktreeRef("branch", *req.Branch)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		rec.Branch = branch
	}
	shas := []struct {
		field string
		value *string
		dest  *string
	}{
		{"head_sha", req.HeadSHA, &rec.Facts.HeadSHA},
		{"merged_sha", req.MergedSHA, &rec.Facts.MergedSHA},
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
		*s.dest = value
	}
	if req.MergedInto != nil {
		mergedInto, err := validateWorktreeRef("merged_into", *req.MergedInto)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		rec.Facts.MergedInto = mergedInto
	}
	if req.Dirty != nil {
		rec.Facts.Dirty = *req.Dirty
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !worktreeStatuses[status] {
			writeError(w, http.StatusBadRequest, "status must be one of active, blocked, merged, archived")
			return
		}
		rec.Status = status
	}
	now := time.Now()
	rec.Facts.VerifiedAt = &now

	if err := h.ledger().Save(&rec, "sync("+rec.Key+"): "+firstNonEmpty(rec.Branch, "no branch")); err != nil {
		writeLedgerError(w, r, "sync worktree", err)
		return
	}
	writeJSON(w, http.StatusOK, worktreeToResponse(rec))
}

// --- entries ---

func worktreeEntryLimit(raw string) int {
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
	return n
}

// ListWorktreeEntries handles GET /api/worktrees/{id}/entries. Newest first: a
// ledger is read from the top, and the window that matters is "since the last
// merge", not the whole history.
func (h *Handler) ListWorktreeEntries(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadWorktreeForUser(w, r)
	if !ok {
		return
	}
	limit := worktreeEntryLimit(r.URL.Query().Get("limit"))
	resp := make([]WorktreeEntryResponse, 0, limit)
	for i := len(rec.Log) - 1; i >= 0 && len(resp) < limit; i-- {
		resp = append(resp, worktreeEntryToResponse(rec, i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": resp})
}

// ListRecentWorktreeEntries handles GET /api/worktree-entries — the
// workspace-wide feed, so a reader can open on what moved lately without
// fanning out one request per tree.
func (h *Handler) ListRecentWorktreeEntries(w http.ResponseWriter, r *http.Request) {
	records, err := h.ledger().List(h.resolveWorkspaceID(r))
	if err != nil {
		writeLedgerError(w, r, "list entries", err)
		return
	}
	all := make([]datedEntry, 0, 64)
	for _, rec := range records {
		for i := range rec.Log {
			all = append(all, datedEntry{at: rec.Log[i].At, resp: worktreeEntryToResponse(rec, i)})
		}
	}
	sortDatedDesc(all)
	limit := worktreeEntryLimit(r.URL.Query().Get("limit"))
	resp := make([]WorktreeEntryResponse, 0, limit)
	for _, d := range all {
		if len(resp) >= limit {
			break
		}
		resp = append(resp, d.resp)
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
	rec, ok := h.loadWorktreeForUser(w, r)
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
	identifier, issueUUID := "", ""
	if req.IssueID != nil && strings.TrimSpace(*req.IssueID) != "" {
		issue, ok := h.loadIssueForUser(w, r, strings.TrimSpace(*req.IssueID))
		if !ok {
			return
		}
		identifier = h.issueIdentifier(r.Context(), issue)
		issueUUID = uuidToString(issue.ID)
	}

	workspaceID := h.resolveWorkspaceID(r)
	authorType, authorID := h.resolveActor(r, requestUserID(r), workspaceID)
	rec.Log = append(rec.Log, progressledger.Entry{
		At:         time.Now(),
		Kind:       kind,
		Body:       body,
		SHA:        sha,
		Issue:      identifier,
		IssueID:    issueUUID,
		AuthorType: authorType,
		AuthorID:   authorID,
	})
	// A line that names a card, on an account that names none, adopts it. The
	// first `worktree log --issue COC-N` is usually the moment the tree and the
	// card actually meet.
	if rec.Issue == "" && identifier != "" {
		rec.Issue = identifier
		rec.IssueID = issueUUID
	}

	if err := h.ledger().Save(&rec, "progress("+rec.Key+"): "+kind); err != nil {
		writeLedgerError(w, r, "add entry", err)
		return
	}
	writeJSON(w, http.StatusCreated, worktreeEntryToResponse(rec, len(rec.Log)-1))
}

// --- the card's side of the account ---

// IssueSessionResponse is one session that worked on a card: who was driving,
// how to resume them, and whether they are waiting on a person right now.
type IssueSessionResponse struct {
	Worktree        string  `json:"worktree"`
	WorktreeID      string  `json:"worktree_id"`
	Role            string  `json:"role"`
	Status          string  `json:"status"`
	Branch          string  `json:"branch"`
	Agent           string  `json:"agent"`
	SessionID       string  `json:"session_id"`
	Resume          string  `json:"resume"`
	Owner           string  `json:"owner"`
	NextAction      string  `json:"next_action"`
	WaitingForHuman bool    `json:"waiting_for_human"`
	UpdatedAt       *string `json:"updated_at"`
	// Direct is true when the account belongs to this card, false when it only
	// mentions it in a log line. A card usually has one of the first kind and
	// any number of the second.
	Direct bool `json:"direct"`
}

// ListIssueWorktreeSessions handles GET /api/issues/{id}/sessions.
//
// The card's view of the ledger: which sessions have touched this card and
// where they left off. It reads the same accounts the worktree commands write,
// so nothing here can go stale independently — there is no second copy to
// forget to update, which is the failure the pinned session comment had.
func (h *Handler) ListIssueWorktreeSessions(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	records, err := h.ledger().List(workspaceID)
	if err != nil {
		writeLedgerError(w, r, "list sessions", err)
		return
	}
	issueUUID := uuidToString(issue.ID)
	identifier := h.issueIdentifier(r.Context(), issue)
	resp := make([]IssueSessionResponse, 0, 4)
	for _, rec := range records {
		direct := strings.EqualFold(rec.Issue, identifier) ||
			(rec.IssueID != "" && rec.IssueID == issueUUID)
		mentioned := direct
		if !mentioned {
			for _, e := range rec.Log {
				if strings.EqualFold(e.Issue, identifier) || (e.IssueID != "" && e.IssueID == issueUUID) {
					mentioned = true
					break
				}
			}
		}
		if !mentioned {
			continue
		}
		// An account with nothing in the session slot has no session to show.
		// Listing it would put an empty row on the card for every tree that
		// ever mentioned it.
		if rec.Session.Agent == "" && rec.Session.SessionID == "" && rec.Session.Resume == "" &&
			rec.Session.NextAction == "" {
			continue
		}
		resp = append(resp, IssueSessionResponse{
			Worktree:        rec.Name,
			WorktreeID:      rec.Key,
			Role:            rec.Role,
			Status:          rec.Status,
			Branch:          rec.Branch,
			Agent:           rec.Session.Agent,
			SessionID:       rec.Session.SessionID,
			Resume:          rec.Session.Resume,
			Owner:           rec.Session.Owner,
			NextAction:      rec.Session.NextAction,
			WaitingForHuman: rec.Session.WaitingForHuman,
			UpdatedAt:       ledgerTimePtr(rec.Session.UpdatedAt),
			Direct:          direct,
		})
	}
	// The card's own account first, then whatever else touched it, newest
	// session first inside each group.
	sortIssueSessions(resp)
	writeJSON(w, http.StatusOK, map[string]any{"sessions": resp})
}

// --- small helpers ---

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// normalizeDependsOn trims, drops blanks and de-duplicates while keeping the
// order the caller sent, so a list stays diffable across writes.
func normalizeDependsOn(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

// issueIdentifier renders the card the way a person reads it (COC-348). The row
// carries only the number; the prefix belongs to the workspace.
func (h *Handler) issueIdentifier(ctx context.Context, issue db.Issue) string {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	if prefix == "" {
		return ""
	}
	return prefix + "-" + strconv.Itoa(int(issue.Number))
}

// lookupIssueQuietly resolves a card reference without writing an error
// response. The ledger accepts accounts for cards that do not exist yet — a
// branch cut before the card was filed is normal — so a miss here is a fact to
// record, not a request to reject.
func (h *Handler) lookupIssueQuietly(r *http.Request, ref string) (db.Issue, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Issue{}, false
	}
	rec := httptest.NewRecorder()
	issue, ok := h.loadIssueForUser(rec, r, ref)
	if !ok {
		return db.Issue{}, false
	}
	return issue, true
}

// datedEntry carries an entry alongside its timestamp so the cross-account feed
// can be ordered without re-parsing the rendered string.
type datedEntry struct {
	at   time.Time
	resp WorktreeEntryResponse
}

// sortDatedDesc puts the newest line first across every account.
func sortDatedDesc(all []datedEntry) {
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
}

// sortIssueSessions puts the card's own account first, then the accounts that
// only mention it, newest session first inside each group.
func sortIssueSessions(rows []IssueSessionResponse) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Direct != rows[j].Direct {
			return rows[i].Direct
		}
		return derefString(rows[i].UpdatedAt) > derefString(rows[j].UpdatedAt)
	})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
