package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func postCard(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.CreateCard(recorder, newRequest("POST", "/api/cards", body))
	return recorder
}

func decodeCard(t *testing.T, recorder *httptest.ResponseRecorder) CardResponse {
	t.Helper()
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("card request: unexpected %d: %s", recorder.Code, recorder.Body.String())
	}
	var card CardResponse
	if err := json.NewDecoder(recorder.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	return card
}

func cardRequest(t *testing.T, method, id string, body any) *http.Request {
	t.Helper()
	req := newRequest(method, "/api/cards/"+id, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func cleanupCard(t *testing.T, id string) {
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM card WHERE id = $1`, id)
	})
}

// A card is written against a requirement, and reading it back has to say
// which one — that link is the whole point of writing it there.
func TestCreateCard_LinksToItsRequirement(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "card parent", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	card := decodeCard(t, postCard(t, map[string]any{
		"title":    "单聊并发接续技术学习复盘",
		"content":  "## 学到什么\n\nrouter 要按 root 串行",
		"issue_id": issueID,
	}))
	cleanupCard(t, card.ID)

	if card.IssueID == nil || *card.IssueID != issueID {
		t.Fatalf("issue_id = %v, want %s", card.IssueID, issueID)
	}
	if card.Title != "单聊并发接续技术学习复盘" {
		t.Fatalf("title = %q", card.Title)
	}
}

// A lesson can come from reading or from an incident. Requiring a requirement
// would make the ones worth keeping impossible to record.
func TestCreateCard_WorksWithoutARequirement(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	card := decodeCard(t, postCard(t, map[string]any{
		"title":   "读了一篇文章",
		"content": "笔记",
	}))
	cleanupCard(t, card.ID)

	if card.IssueID != nil {
		t.Fatalf("issue_id = %v, want nil", *card.IssueID)
	}
}

// One requirement can leave several unrelated lessons behind — that is why
// this is a table and not a field on the issue.
func TestListCardsForIssue_ReturnsEveryCardOldestFirst(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "card parent multi", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	first := decodeCard(t, postCard(t, map[string]any{
		"title": "第一课", "content": "a", "issue_id": issueID,
	}))
	cleanupCard(t, first.ID)
	// Ensure a distinct created_at so the ordering assertion is meaningful.
	time.Sleep(10 * time.Millisecond)
	second := decodeCard(t, postCard(t, map[string]any{
		"title": "第二课", "content": "b", "issue_id": issueID,
	}))
	cleanupCard(t, second.ID)

	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/cards", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListCardsForIssue(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Cards []CardResponse `json:"cards"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Three, not two: since COC-338 every issue is created with its body
	// already filed as a document (the R0 snapshot), so the two written here
	// are not the whole list. What this test is about is the order of the ones
	// written, so it reads their positions rather than the length.
	firstAt, secondAt := -1, -1
	for i, card := range response.Cards {
		switch card.ID {
		case first.ID:
			firstAt = i
		case second.ID:
			secondAt = i
		}
	}
	if firstAt < 0 || secondAt < 0 {
		t.Fatalf("written cards missing from the list: %+v", response.Cards)
	}
	// Read together they are a narrative of how the work went, which reads
	// forwards.
	if firstAt > secondAt {
		t.Fatalf("cards not in creation order")
	}
}

// The workspace list is "what have I learned lately", so it reads newest
// first — the opposite of the per-issue view, on purpose.
func TestListCards_NewestFirst(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	older := decodeCard(t, postCard(t, map[string]any{"title": "older", "content": ""}))
	cleanupCard(t, older.ID)
	time.Sleep(10 * time.Millisecond)
	newer := decodeCard(t, postCard(t, map[string]any{"title": "newer", "content": ""}))
	cleanupCard(t, newer.ID)

	recorder := httptest.NewRecorder()
	testHandler.ListCards(recorder, newRequest("GET",
		"/api/cards?workspace_id="+testWorkspaceID+"&limit=200", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Cards []CardResponse `json:"cards"`
		Total int64          `json:"total"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var newerAt, olderAt int
	for i, card := range response.Cards {
		switch card.ID {
		case newer.ID:
			newerAt = i
		case older.ID:
			olderAt = i
		}
	}
	if newerAt >= olderAt {
		t.Fatalf("newer card at %d is not ahead of older at %d", newerAt, olderAt)
	}
	if response.Total < 2 {
		t.Fatalf("total = %d, want at least 2", response.Total)
	}
}

// A blank title with a body is a valid card: the note is the point, naming it
// is optional. Demanding a name first is the friction that stops people
// writing anything down.
func TestCreateCard_AcceptsABlankTitle(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	recorder := postCard(t, map[string]any{"title": "", "content": "just the thought"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	var card CardResponse
	if err := json.NewDecoder(recorder.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if card.Title != "" || card.Content != "just the thought" {
		t.Fatalf("stored title=%q content=%q", card.Title, card.Content)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM card WHERE id = $1`, card.ID)
	})
}

func TestCreateCard_RejectsUnusableInput(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name string
		body map[string]any
	}{
		{"nothing at all", map[string]any{"title": "   ", "content": "  "}},
		{"oversized title", map[string]any{
			"title": strings.Repeat("字", maxCardTitleLength+1), "content": "x",
		}},
		{"unknown issue", map[string]any{
			"title": "t", "content": "", "issue_id": "00000000-0000-0000-0000-000000000000",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := postCard(t, tc.body)
			if recorder.Code < 400 || recorder.Code >= 500 {
				t.Fatalf("status = %d, want a 4xx: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// The title cap is a character count, not a byte count: a CJK title would
// otherwise hit it at a third of the intended length.
func TestCreateCard_TitleLimitCountsCharacters(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	card := decodeCard(t, postCard(t, map[string]any{
		"title":   strings.Repeat("字", maxCardTitleLength),
		"content": "",
	}))
	cleanupCard(t, card.ID)
	if len([]rune(card.Title)) != maxCardTitleLength {
		t.Fatalf("title was rejected or truncated at %d characters", len([]rune(card.Title)))
	}
}

// Absent, explicit null and a value are three different intents on update.
func TestUpdateCard_DistinguishesAbsentFromNullIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "card relink", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	card := decodeCard(t, postCard(t, map[string]any{
		"title": "t", "content": "c", "issue_id": issueID,
	}))
	cleanupCard(t, card.ID)

	// Absent issue_id: editing the text must not silently detach the card.
	recorder := httptest.NewRecorder()
	testHandler.UpdateCard(recorder, cardRequest(t, "PUT", card.ID,
		map[string]any{"title": "renamed"}))
	updated := decodeCard(t, recorder)
	if updated.IssueID == nil || *updated.IssueID != issueID {
		t.Fatalf("absent issue_id dropped the link: %v", updated.IssueID)
	}
	if updated.Title != "renamed" {
		t.Fatalf("title = %q, want renamed", updated.Title)
	}

	// Explicit null: detach.
	recorder = httptest.NewRecorder()
	testHandler.UpdateCard(recorder, cardRequest(t, "PUT", card.ID,
		json.RawMessage(`{"issue_id":null}`)))
	detached := decodeCard(t, recorder)
	if detached.IssueID != nil {
		t.Fatalf("explicit null did not detach: %v", *detached.IssueID)
	}
}

// A card id from another workspace must read as not-found, never as a
// forbidden resource that exists.
func TestGetCard_HidesOtherWorkspaces(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id
	`, "card-other", fmt.Sprintf("card-other-%d", time.Now().UnixNano())).
		Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var foreignCard string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO card (workspace_id, author_type, author_id, title)
		VALUES ($1, 'member', $2, 'foreign') RETURNING id
	`, otherWorkspaceID, testUserID).Scan(&foreignCard); err != nil {
		t.Fatalf("create foreign card: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM card WHERE workspace_id = $1`, otherWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	recorder := httptest.NewRecorder()
	testHandler.GetCard(recorder, cardRequest(t, "GET", foreignCard, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestDeleteCard_RemovesIt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	card := decodeCard(t, postCard(t, map[string]any{"title": "temp", "content": ""}))

	recorder := httptest.NewRecorder()
	testHandler.DeleteCard(recorder, cardRequest(t, "DELETE", card.ID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.GetCard(recorder, cardRequest(t, "GET", card.ID, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("card still readable after delete: %d", recorder.Code)
	}
}

// Deleting a requirement must not take its cards with it — the lesson
// outlives the ticket, which is the reason this is not an issue field.
func TestCard_SurvivesItsRequirementBeingDeleted(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "card orphan source", "done", "none")
	card := decodeCard(t, postCard(t, map[string]any{
		"title": "lesson", "content": "kept", "issue_id": issueID,
	}))
	cleanupCard(t, card.ID)

	deleteTestIssue(t, issueID)

	recorder := httptest.NewRecorder()
	testHandler.GetCard(recorder, cardRequest(t, "GET", card.ID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("card was lost with its issue: %d", recorder.Code)
	}
	if got := decodeCard(t, recorder); got.Content != "kept" {
		t.Fatalf("content = %q", got.Content)
	}
}

// Round docs are write-once: past the correction grace, update and delete
// both return 409 naming the supersede path (COC-281).
func TestUpdateCard_RoundDocFrozenAfterGrace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	card := decodeCard(t, postCard(t, map[string]any{
		"title": "R1", "content": "conclusion", "kind": "COC-999/rounds/R1-方案评审",
	}))
	cleanupCard(t, card.ID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE card SET created_at = now() - interval '2 hours' WHERE id = $1`, card.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	recorder := httptest.NewRecorder()
	testHandler.UpdateCard(recorder, cardRequest(t, "PUT", card.ID,
		map[string]any{"title": "rewritten"}))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("update past grace = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler.DeleteCard(recorder, cardRequest(t, "DELETE", card.ID, nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete past grace = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateCard_RoundDocEditableWithinGrace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	card := decodeCard(t, postCard(t, map[string]any{
		"title": "R1", "content": "conclusion", "kind": "COC-999/rounds/R1-方案评审",
	}))
	cleanupCard(t, card.ID)

	recorder := httptest.NewRecorder()
	testHandler.UpdateCard(recorder, cardRequest(t, "PUT", card.ID,
		map[string]any{"title": "typo fix"}))
	updated := decodeCard(t, recorder)
	if updated.Title != "typo fix" {
		t.Fatalf("within grace update failed: %d %s", recorder.Code, recorder.Body.String())
	}
}

// A spec doc freezes with its terminal issue; a plain unkinded card on the
// same issue stays editable (backward compatibility).
func TestUpdateCard_SpecDocFrozenOnTerminalIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "spec freeze", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	spec := decodeCard(t, postCard(t, map[string]any{
		"title": "spec", "content": "plan", "kind": "COC-999/spec", "issue_id": issueID,
	}))
	cleanupCard(t, spec.ID)
	plain := decodeCard(t, postCard(t, map[string]any{
		"title": "note", "content": "n", "issue_id": issueID,
	}))
	cleanupCard(t, plain.ID)

	recorder := httptest.NewRecorder()
	testHandler.UpdateCard(recorder, cardRequest(t, "PUT", spec.ID,
		map[string]any{"title": "edit"}))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("terminal spec update = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.UpdateCard(recorder, cardRequest(t, "PUT", plain.ID,
		map[string]any{"title": "still editable"}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("plain card on terminal issue = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}
