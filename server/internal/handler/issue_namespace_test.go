package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/issuenamespace"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// COC-338: every issue is created with a fixed document directory, held open
// by placeholder cards until somebody writes the documents. The placeholders
// are addressable but are not documents: they must stay out of every list,
// search, brief, and numbering derivation, and the ONLY test for one anywhere
// is the is_placeholder column.

// namespaceFixture creates an issue with a body and returns its id, key, and
// the parsed directory.
func namespaceFixture(t *testing.T, title, body string) (string, issuenamespace.Namespace) {
	t.Helper()
	return namespaceFixtureWithStatus(t, title, body, "todo")
}

// namespaceFixtureWithStatus is namespaceFixture with the status the issue is
// born at, which is what decides whether it gets placeholders at all.
func namespaceFixtureWithStatus(t *testing.T, title, body, status string) (string, issuenamespace.Namespace) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"description": body,
		"status":      status,
		"priority":    "none",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM card WHERE issue_id = $1`, issue.ID)
		deleteTestIssue(t, issue.ID)
	})
	return issue.ID, readNamespace(t, issue.ID)
}

func readNamespace(t *testing.T, issueID string) issuenamespace.Namespace {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetIssueNamespace(w, withURLParam(newRequest("GET", "/api/issues/"+issueID+"/namespace", nil), "id", issueID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssueNamespace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ns issuenamespace.Namespace
	if err := json.NewDecoder(w.Body).Decode(&ns); err != nil {
		t.Fatalf("decode namespace: %v", err)
	}
	return ns
}

func slotState(t *testing.T, ns issuenamespace.Namespace, name string) issuenamespace.State {
	t.Helper()
	for _, slot := range ns.Slots {
		if slot.Name == name {
			return slot
		}
	}
	t.Fatalf("slot %q missing from namespace %+v", name, ns.Slots)
	return issuenamespace.State{}
}

// cardRowsForIssue reads the table directly, placeholders included, so an
// assertion about what is stored cannot be satisfied by a filtered query.
func cardRowsForIssue(t *testing.T, issueID string) []db.Card {
	t.Helper()
	rows, err := testHandler.Queries.ListIssueNamespaceCards(context.Background(), db.ListIssueNamespaceCardsParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		t.Fatalf("list namespace cards: %v", err)
	}
	return rows
}

func setIssueStatus(t *testing.T, issueID, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, withURLParam(
		newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID))
	if w.Code != http.StatusOK {
		t.Fatalf("status → %s: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// Matrix #1 — a new issue arrives with every slot already open, and with the
// body it was created with preserved as R0. R0 is a real document: it has
// content from the moment it is written, and marking it a placeholder would
// hide the only copy of the original body.
func TestCreateIssue_SeedsTheDocumentDirectoryAndBodySnapshot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	body := "建卡当刻的正文，R0 要装的就是这一段。"
	issueID, ns := namespaceFixture(t, "namespace skeleton seed", body)

	if len(ns.Slots) != len(issuenamespace.Slots) {
		t.Fatalf("slots = %d, want %d", len(ns.Slots), len(issuenamespace.Slots))
	}
	for _, want := range issuenamespace.Slots {
		state := slotState(t, ns, want.Name)
		if !state.Exists {
			t.Errorf("slot %s: exists = false, want true", want.Name)
		}
		if state.Kind != ns.Key+"/"+want.Name {
			t.Errorf("slot %s: kind = %q, want %q", want.Name, state.Kind, ns.Key+"/"+want.Name)
		}
		if want.Name == "snapshots" {
			// R0 lives under snapshots, so that folder is already answered.
			continue
		}
		if !state.Placeholder {
			t.Errorf("slot %s: placeholder = false on a fresh issue", want.Name)
		}
	}

	snapshotKind := ns.Key + "/" + issuenamespace.BodySnapshotKind
	var r0 *db.Card
	for i, card := range cardRowsForIssue(t, issueID) {
		if card.Kind == snapshotKind {
			r0 = &cardRowsForIssue(t, issueID)[i]
		}
	}
	if r0 == nil {
		t.Fatalf("no card at %q", snapshotKind)
	}
	if r0.IsPlaceholder {
		t.Errorf("%s: is_placeholder = true, want false — R0 is real content", snapshotKind)
	}
	if r0.Content != body {
		t.Errorf("%s: content = %q, want the creation body %q", snapshotKind, r0.Content, body)
	}
}

// Matrix #2 — the ordinary document surfaces. A placeholder in the workspace
// card list or in search is scaffolding presented as somebody's note.
func TestCardReads_NeverShowPlaceholders(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace hidden from lists", "正文")

	// The whole-workspace list.
	w := httptest.NewRecorder()
	testHandler.ListCards(w, newRequest("GET", "/api/cards?limit=200", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListCards: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Cards []CardResponse `json:"cards"`
		Total int64          `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&list)
	for _, card := range list.Cards {
		if card.IsPlaceholder {
			t.Errorf("ListCards returned placeholder %s (%s)", card.ID, card.Kind)
		}
	}

	// The folder filter, which is how the docs page opens an issue's directory.
	w = httptest.NewRecorder()
	testHandler.ListCards(w, newRequest("GET", "/api/cards?kind="+ns.Key, nil))
	json.NewDecoder(w.Body).Decode(&list)
	for _, card := range list.Cards {
		if card.IsPlaceholder {
			t.Errorf("kind filter returned placeholder %s (%s)", card.ID, card.Kind)
		}
	}
	if list.Total != 1 {
		t.Errorf("kind=%s total = %d, want 1 (R0 only)", ns.Key, list.Total)
	}

	// Search. The placeholder titles carry 待补, which is exactly the string a
	// text-matching implementation would leak on.
	w = httptest.NewRecorder()
	testHandler.ListCards(w, newRequest("GET", "/api/cards?q=%E5%BE%85%E8%A1%A5", nil))
	json.NewDecoder(w.Body).Decode(&list)
	for _, card := range list.Cards {
		if card.IsPlaceholder {
			t.Errorf("search returned placeholder %s (%s)", card.ID, card.Kind)
		}
	}

	// The issue's own document list.
	w = httptest.NewRecorder()
	testHandler.ListCardsForIssue(w, withURLParam(newRequest("GET", "/api/issues/"+issueID+"/cards", nil), "id", issueID))
	var forIssue struct {
		Cards []CardResponse `json:"cards"`
	}
	json.NewDecoder(w.Body).Decode(&forIssue)
	if len(forIssue.Cards) != 1 {
		t.Fatalf("issue cards = %d, want 1 (R0 only): %+v", len(forIssue.Cards), forIssue.Cards)
	}
	if forIssue.Cards[0].Kind != ns.Key+"/"+issuenamespace.BodySnapshotKind {
		t.Errorf("issue cards[0].kind = %q, want the body snapshot", forIssue.Cards[0].Kind)
	}
}

// Matrix #3 — the agent brief. daemon.go builds IssueDocs from exactly this
// query, so an agent that picks the issue up must not be handed six empty
// documents and told they are the artefacts of the last round.
func TestAgentBriefDocs_NeverShowPlaceholders(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, _ := namespaceFixture(t, "namespace hidden from brief", "正文")

	docs, err := testHandler.Queries.ListCardsForIssue(context.Background(), db.ListCardsForIssueParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		t.Fatalf("list cards for issue: %v", err)
	}
	for _, doc := range docs {
		if doc.IsPlaceholder {
			t.Errorf("brief doc list contains placeholder %s", doc.Kind)
		}
	}
	if len(docs) != 1 {
		t.Fatalf("brief docs = %d, want 1 (R0 only)", len(docs))
	}
}

// Matrix #4 — decision numbering. The decisions folder is held open by a
// placeholder; if it reached the derivation it would take D1 and push the
// first real decision to D2, renumbering an issue's decisions from under
// everyone who already cited them.
func TestDecisionDerivation_IsNotPollutedByThePlaceholder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace decisions numbering", "正文")

	card := decodeCard(t, postCard(t, map[string]any{
		"issue_id": issueID,
		"kind":     ns.Key + "/decisions/D1-storage",
		"title":    "D1 用局部索引",
		"content": "<!-- decision\nquestion: 索引怎么建\nsummary: 局部索引\n-->\n\n" +
			"局部索引只覆盖占位行。",
	}))
	cleanupCard(t, card.ID)

	docs, err := testHandler.Queries.ListCardsForIssue(context.Background(), db.ListCardsForIssueParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		t.Fatalf("list cards for issue: %v", err)
	}
	forDerivation := make([]struct{ ID, Kind, Content string }, 0, len(docs))
	for _, doc := range docs {
		if doc.Kind == ns.Key+"/decisions" {
			t.Errorf("the decisions placeholder reached the derivation input")
		}
		forDerivation = append(forDerivation, struct{ ID, Kind, Content string }{
			ID: uuidToString(doc.ID), Kind: doc.Kind, Content: doc.Content,
		})
	}
	decisions, _ := DeriveIssueDecisions(forDerivation)
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1: %+v", len(decisions), decisions)
	}
	// The number is read off the card's own path segment; nothing else may
	// occupy a slot in that sequence.
	if !strings.HasPrefix(decisions[0].ID, "D1") {
		t.Errorf("first decision = %s, want D1… — the placeholder took a number", decisions[0].ID)
	}
	if decisions[0].DocID != card.ID {
		t.Errorf("first decision doc = %s, want the written card %s", decisions[0].DocID, card.ID)
	}
}

// Matrix #5 — round numbering and the round-closure gate. `<KEY>/rounds` is a
// folder held open, not a closed round: a gate that counted it would let an
// issue finish with its review station never actually closed, and the next
// round would open at R2 with no R1 behind it.
func TestRoundDerivation_IsNotPollutedByThePlaceholder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace rounds numbering", "正文")

	docs, err := testHandler.Queries.ListCardsForIssue(context.Background(), db.ListCardsForIssueParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		t.Fatalf("list cards for issue: %v", err)
	}
	prefix := roundDocKindFor(ns.Key)
	for _, doc := range docs {
		if doc.IsPlaceholder {
			t.Errorf("round derivation input contains placeholder %s", doc.Kind)
		}
		if strings.HasPrefix(doc.Kind, prefix) {
			t.Errorf("round derivation sees %s, which is not a closed round", doc.Kind)
		}
	}

	// And the gate itself: with a review station opened and no round closed,
	// done stays refused even though the rounds folder "exists".
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue_phase SET entered_at = now() WHERE issue_id = $1 AND name = '代码评审'`,
		issueID); err != nil {
		t.Fatalf("open review station: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if _, blocked := testHandler.checkRoundClosureForDone(context.Background(), issue, ns.Key); !blocked {
		t.Error("round closure gate passed with only the rounds placeholder present")
	}
}

// Matrix #6 — the namespace endpoint is the one read that shows the slots
// nobody has written into yet, and it says which of the two each slot is.
func TestGetIssueNamespace_ShowsEverySlotAndWhichAreStillPending(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace endpoint shape", "正文")

	if ns.Root != ns.Key || ns.IssueID != issueID {
		t.Errorf("namespace root/issue = %q/%q, want %q/%q", ns.Root, ns.IssueID, ns.Key, issueID)
	}
	design := slotState(t, ns, "design")
	if !design.Placeholder || design.CardID == "" {
		t.Errorf("design = %+v, want a pending slot with an addressable card", design)
	}
	snapshots := slotState(t, ns, "snapshots")
	if snapshots.Placeholder {
		t.Error("snapshots reads as pending although R0 is filed under it")
	}
	if snapshots.Count != 1 {
		t.Errorf("snapshots count = %d, want 1 (R0)", snapshots.Count)
	}

	card := decodeCard(t, postCard(t, map[string]any{
		"issue_id": issueID, "kind": ns.Key + "/design", "title": "技术方案", "content": "真实内容",
	}))
	cleanupCard(t, card.ID)
	after := slotState(t, readNamespace(t, issueID), "design")
	if after.Placeholder {
		t.Error("design still reads as pending after a real document landed")
	}
	if after.Count != 1 {
		t.Errorf("design count = %d, want 1", after.Count)
	}
}

// Matrix #7 — promotion in place. The slot keeps its card id and flips to a
// real document in one statement, so nothing observes it as neither pending
// nor written, and a link taken from the namespace still resolves.
func TestWritingARealDocument_PromotesThePlaceholderInPlace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace promote in place", "正文")
	before := slotState(t, ns, "requirements")

	card := decodeCard(t, postCard(t, map[string]any{
		"issue_id": issueID,
		"kind":     ns.Key + "/requirements",
		"title":    "需求底稿",
		"content":  "真正的需求底稿正文",
	}))
	cleanupCard(t, card.ID)

	if card.ID != before.CardID {
		t.Errorf("card id = %s, want the placeholder's %s — the slot was replaced, not promoted", card.ID, before.CardID)
	}
	if card.IsPlaceholder {
		t.Error("promoted card still reports is_placeholder")
	}
	if card.Content != "真正的需求底稿正文" {
		t.Errorf("content = %q", card.Content)
	}

	// Exactly one row at that kind: a promote that inserted instead would
	// leave the placeholder behind, invisible and permanent.
	count := 0
	for _, row := range cardRowsForIssue(t, issueID) {
		if row.Kind == ns.Key+"/requirements" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("rows at %s/requirements = %d, want 1", ns.Key, count)
	}
}

// Matrix #8 — a finished issue drops the slots it never filled. Six 待补
// entries on a done card read as unfinished work; the honest record is the
// documents that were actually written, and those stay.
func TestTerminalStatus_PrunesOnlyTheSlotsStillPending(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace prune on done", "正文")
	real := decodeCard(t, postCard(t, map[string]any{
		"issue_id": issueID, "kind": ns.Key + "/spec", "title": "Spec", "content": "冻结的 spec",
	}))
	cleanupCard(t, real.ID)

	setIssueStatus(t, issueID, "done")

	for _, row := range cardRowsForIssue(t, issueID) {
		if row.IsPlaceholder {
			t.Errorf("placeholder %s survived done", row.Kind)
		}
	}
	kinds := map[string]bool{}
	for _, row := range cardRowsForIssue(t, issueID) {
		kinds[row.Kind] = true
	}
	if !kinds[ns.Key+"/spec"] {
		t.Error("the written spec was deleted with the placeholders")
	}
	if !kinds[ns.Key+"/"+issuenamespace.BodySnapshotKind] {
		t.Error("R0 was deleted with the placeholders")
	}
}

// Matrix #9 — reopening restores what is missing and overwrites nothing. The
// questions are askable again; the answers already given are not up for
// rewriting.
func TestReopen_RestoresMissingSlotsWithoutTouchingWrittenOnes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, ns := namespaceFixture(t, "namespace reseed on reopen", "正文")
	real := decodeCard(t, postCard(t, map[string]any{
		"issue_id": issueID, "kind": ns.Key + "/design", "title": "技术方案", "content": "已经写好的方案",
	}))
	cleanupCard(t, real.ID)

	setIssueStatus(t, issueID, "cancelled")
	setIssueStatus(t, issueID, "in_progress")

	reopened := readNamespace(t, issueID)
	for _, want := range issuenamespace.Slots {
		if !slotState(t, reopened, want.Name).Exists {
			t.Errorf("slot %s missing after reopen", want.Name)
		}
	}
	design := slotState(t, reopened, "design")
	if design.Placeholder {
		t.Error("the written design was overwritten by a fresh placeholder")
	}
	if design.CardID != real.ID {
		t.Errorf("design card id = %s, want the written document %s", design.CardID, real.ID)
	}
	var kept db.Card
	for _, row := range cardRowsForIssue(t, issueID) {
		if row.Kind == ns.Key+"/design" {
			kept = row
		}
	}
	if kept.Content != "已经写好的方案" {
		t.Errorf("design content = %q, want it untouched", kept.Content)
	}
	// One row per slot: a reseed that re-created what already existed would
	// double the directory.
	perKind := map[string]int{}
	for _, row := range cardRowsForIssue(t, issueID) {
		perKind[row.Kind]++
	}
	for kind, n := range perKind {
		if n > 1 {
			t.Errorf("kind %s has %d rows after reopen, want 1", kind, n)
		}
	}
}

// refusedCommitTx makes the issue-creation transaction fail at the last
// moment, after the directory has been written on it.
type refusedCommitTx struct{ pgx.Tx }

func (t refusedCommitTx) Commit(context.Context) error { return errors.New("commit refused") }

type refusedCommitTxStarter struct{ pool *pgxpool.Pool }

func (s refusedCommitTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return refusedCommitTx{tx}, nil
}

// Matrix #10 — the directory is part of the create transaction, not a follow-up
// to it. A create that fails leaves no issue AND no half-built directory; the
// alternative is orphan placeholders belonging to an issue that never existed.
func TestFailedIssueCreate_LeavesNoSkeletonBehind(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var before int64
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM card WHERE workspace_id = $1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count cards: %v", err)
	}

	svc := service.NewIssueService(db.New(testPool), refusedCommitTxStarter{pool: testPool}, nil, analytics.NoopClient{}, nil)
	_, err := svc.Create(ctx, service.IssueCreateParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       "namespace rollback probe",
		Description: pgtype.Text{String: "正文", Valid: true},
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   parseUUID(testUserID),
	}, service.IssueCreateOpts{})
	if err == nil {
		t.Fatal("create succeeded although commit was refused")
	}

	var after int64
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM card WHERE workspace_id = $1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	if after != before {
		t.Errorf("cards = %d after a failed create, was %d — the skeleton outlived the transaction", after, before)
	}
	var issues int64
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = 'namespace rollback probe'`,
		testWorkspaceID).Scan(&issues); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if issues != 0 {
		t.Errorf("issue rows = %d after a failed create, want 0", issues)
	}
}

// Matrix #11 — an issue born finished gets R0 and nothing else.
//
// Placeholders exist to be cleaned up when the issue crosses into a terminal
// status. A card created straight into `done` or `cancelled` never crosses
// that boundary, so its placeholders would stand empty forever — the exact
// "no empty documents once finished" invariant they were put there to serve.
// R0 is a different thing and still gets written: it is the original body, not
// an empty slot, and it is the only copy.
func TestCreateIssue_InATerminalStatus_SeedsR0ButNoPlaceholders(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, status := range []string{"done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			body := "建卡即" + status + "，正文仍然要留下来。"
			issueID, ns := namespaceFixtureWithStatus(t, "namespace born "+status, body, status)

			rows := cardRowsForIssue(t, issueID)
			for _, row := range rows {
				if row.IsPlaceholder {
					t.Errorf("placeholder %s was seeded on an issue created as %s — nothing will ever clean it up", row.Kind, status)
				}
			}

			snapshotKind := ns.Key + "/" + issuenamespace.BodySnapshotKind
			var r0 *db.Card
			for i := range rows {
				if rows[i].Kind == snapshotKind {
					r0 = &rows[i]
				}
			}
			if r0 == nil {
				t.Fatalf("no card at %q: the creation body was dropped", snapshotKind)
			}
			if r0.IsPlaceholder {
				t.Errorf("%s: is_placeholder = true, want false — R0 is real content", snapshotKind)
			}
			if r0.Content != body {
				t.Errorf("%s: content = %q, want the creation body %q", snapshotKind, r0.Content, body)
			}
			if len(rows) != 1 {
				t.Errorf("cards = %d, want 1 (R0 only): %+v", len(rows), rows)
			}

			// And the endpoint says so: the slots read as never opened, not as
			// pending work somebody still owes on a finished issue.
			for _, want := range issuenamespace.Slots {
				state := slotState(t, ns, want.Name)
				if state.Placeholder {
					t.Errorf("slot %s reads as 待补 on an issue created as %s", want.Name, status)
				}
				if want.Name == "snapshots" {
					if !state.Exists || state.Count != 1 {
						t.Errorf("snapshots = %+v, want it holding R0", state)
					}
					continue
				}
				if state.Exists {
					t.Errorf("slot %s: exists = true, want false — nothing was seeded there", want.Name)
				}
			}
		})
	}
}

// Matrix #12 — a non-terminal status is unaffected. `backlog` is the status
// closest to "finished" that is still open work, and it must still arrive with
// the full directory: the skip is about terminal statuses, not about statuses
// nobody has started yet.
func TestCreateIssue_InBacklog_StillSeedsTheFullDirectory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	body := "backlog 的正文"
	issueID, ns := namespaceFixtureWithStatus(t, "namespace born backlog", body, "backlog")

	for _, want := range issuenamespace.Slots {
		state := slotState(t, ns, want.Name)
		if !state.Exists {
			t.Errorf("slot %s: exists = false on a backlog issue", want.Name)
		}
		if want.Name == "snapshots" {
			// R0 lives under snapshots, so that folder is already answered.
			continue
		}
		if !state.Placeholder {
			t.Errorf("slot %s: placeholder = false on a fresh backlog issue", want.Name)
		}
	}
	placeholders := 0
	for _, row := range cardRowsForIssue(t, issueID) {
		if row.IsPlaceholder {
			placeholders++
		}
	}
	if placeholders != len(issuenamespace.Slots) {
		t.Errorf("placeholders = %d, want %d", placeholders, len(issuenamespace.Slots))
	}
}

// Matrix #13 — reopening an issue that was born finished builds the directory
// it never had. Skipping the placeholders at creation is a deferral, not a
// decision that this issue has no slots: the moment the work is open again the
// six questions are askable, and Reseed is what has to answer for that.
func TestReopen_OfAnIssueBornTerminal_BuildsTheSkeleton(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	body := "建卡即 done 的正文"
	issueID, ns := namespaceFixtureWithStatus(t, "namespace reopen from born-done", body, "done")

	setIssueStatus(t, issueID, "in_progress")

	reopened := readNamespace(t, issueID)
	for _, want := range issuenamespace.Slots {
		state := slotState(t, reopened, want.Name)
		if !state.Exists {
			t.Errorf("slot %s missing after reopen", want.Name)
		}
		if want.Name == "snapshots" {
			continue
		}
		if !state.Placeholder {
			t.Errorf("slot %s: placeholder = false after reopen, want a pending slot", want.Name)
		}
		if state.CardID == "" {
			t.Errorf("slot %s: no addressable card after reopen", want.Name)
		}
	}

	// R0 is not rebuilt: it records the body at creation, and writing it again
	// now would file a claim about the past that is false.
	r0Rows, perKind := 0, map[string]int{}
	for _, row := range cardRowsForIssue(t, issueID) {
		perKind[row.Kind]++
		if row.Kind == ns.Key+"/"+issuenamespace.BodySnapshotKind {
			r0Rows++
			if row.Content != body {
				t.Errorf("R0 content = %q, want the creation body %q", row.Content, body)
			}
		}
	}
	if r0Rows != 1 {
		t.Errorf("R0 rows = %d, want 1", r0Rows)
	}
	for kind, n := range perKind {
		if n > 1 {
			t.Errorf("kind %s has %d rows after reopen, want 1", kind, n)
		}
	}
}
