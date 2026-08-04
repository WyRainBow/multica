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

func postRetro(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.CreateRetro(recorder, newRequest("POST", "/api/retros", body))
	return recorder
}

func decodeRetro(t *testing.T, recorder *httptest.ResponseRecorder) RetroResponse {
	t.Helper()
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("retro request: unexpected %d: %s", recorder.Code, recorder.Body.String())
	}
	var retro RetroResponse
	if err := json.NewDecoder(recorder.Body).Decode(&retro); err != nil {
		t.Fatalf("decode retro: %v", err)
	}
	return retro
}

func retroRequest(t *testing.T, method, id string, body any) *http.Request {
	t.Helper()
	req := newRequest(method, "/api/retros/"+id, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func cleanupRetro(t *testing.T, id string) {
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM retro WHERE id = $1`, id)
	})
}

// A retro is written against a requirement, and reading it back has to say
// which one — that link is the whole point of writing it there.
func TestCreateRetro_LinksToItsRequirement(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "retro parent", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	retro := decodeRetro(t, postRetro(t, map[string]any{
		"title":    "单聊并发接续技术学习复盘",
		"content":  "## 学到什么\n\nrouter 要按 root 串行",
		"issue_id": issueID,
	}))
	cleanupRetro(t, retro.ID)

	if retro.IssueID == nil || *retro.IssueID != issueID {
		t.Fatalf("issue_id = %v, want %s", retro.IssueID, issueID)
	}
	if retro.Title != "单聊并发接续技术学习复盘" {
		t.Fatalf("title = %q", retro.Title)
	}
}

// A lesson can come from reading or from an incident. Requiring a requirement
// would make the ones worth keeping impossible to record.
func TestCreateRetro_WorksWithoutARequirement(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	retro := decodeRetro(t, postRetro(t, map[string]any{
		"title":   "读了一篇文章",
		"content": "笔记",
	}))
	cleanupRetro(t, retro.ID)

	if retro.IssueID != nil {
		t.Fatalf("issue_id = %v, want nil", *retro.IssueID)
	}
}

// One requirement can leave several unrelated lessons behind — that is why
// this is a table and not a field on the issue.
func TestListRetrosForIssue_ReturnsEveryRetroOldestFirst(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "retro parent multi", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	first := decodeRetro(t, postRetro(t, map[string]any{
		"title": "第一课", "content": "a", "issue_id": issueID,
	}))
	cleanupRetro(t, first.ID)
	// Ensure a distinct created_at so the ordering assertion is meaningful.
	time.Sleep(10 * time.Millisecond)
	second := decodeRetro(t, postRetro(t, map[string]any{
		"title": "第二课", "content": "b", "issue_id": issueID,
	}))
	cleanupRetro(t, second.ID)

	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/retros", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListRetrosForIssue(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Retros []RetroResponse `json:"retros"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Retros) != 2 {
		t.Fatalf("got %d retros, want 2", len(response.Retros))
	}
	// Read together they are a narrative of how the work went, which reads
	// forwards.
	if response.Retros[0].ID != first.ID || response.Retros[1].ID != second.ID {
		t.Fatalf("retros not in creation order")
	}
}

// The workspace list is "what have I learned lately", so it reads newest
// first — the opposite of the per-issue view, on purpose.
func TestListRetros_NewestFirst(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	older := decodeRetro(t, postRetro(t, map[string]any{"title": "older", "content": ""}))
	cleanupRetro(t, older.ID)
	time.Sleep(10 * time.Millisecond)
	newer := decodeRetro(t, postRetro(t, map[string]any{"title": "newer", "content": ""}))
	cleanupRetro(t, newer.ID)

	recorder := httptest.NewRecorder()
	testHandler.ListRetros(recorder, newRequest("GET",
		"/api/retros?workspace_id="+testWorkspaceID+"&limit=200", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Retros []RetroResponse `json:"retros"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var newerAt, olderAt int
	for i, retro := range response.Retros {
		switch retro.ID {
		case newer.ID:
			newerAt = i
		case older.ID:
			olderAt = i
		}
	}
	if newerAt >= olderAt {
		t.Fatalf("newer retro at %d is not ahead of older at %d", newerAt, olderAt)
	}
	if response.Total < 2 {
		t.Fatalf("total = %d, want at least 2", response.Total)
	}
}

func TestCreateRetro_RejectsUnusableInput(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name string
		body map[string]any
	}{
		{"blank title", map[string]any{"title": "   ", "content": "x"}},
		{"oversized title", map[string]any{
			"title": strings.Repeat("字", maxRetroTitleLength+1), "content": "x",
		}},
		{"unknown issue", map[string]any{
			"title": "t", "content": "", "issue_id": "00000000-0000-0000-0000-000000000000",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := postRetro(t, tc.body)
			if recorder.Code < 400 || recorder.Code >= 500 {
				t.Fatalf("status = %d, want a 4xx: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// The title cap is a character count, not a byte count: a CJK title would
// otherwise hit it at a third of the intended length.
func TestCreateRetro_TitleLimitCountsCharacters(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	retro := decodeRetro(t, postRetro(t, map[string]any{
		"title":   strings.Repeat("字", maxRetroTitleLength),
		"content": "",
	}))
	cleanupRetro(t, retro.ID)
	if len([]rune(retro.Title)) != maxRetroTitleLength {
		t.Fatalf("title was rejected or truncated at %d characters", len([]rune(retro.Title)))
	}
}

// Absent, explicit null and a value are three different intents on update.
func TestUpdateRetro_DistinguishesAbsentFromNullIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "retro relink", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	retro := decodeRetro(t, postRetro(t, map[string]any{
		"title": "t", "content": "c", "issue_id": issueID,
	}))
	cleanupRetro(t, retro.ID)

	// Absent issue_id: editing the text must not silently detach the retro.
	recorder := httptest.NewRecorder()
	testHandler.UpdateRetro(recorder, retroRequest(t, "PUT", retro.ID,
		map[string]any{"title": "renamed"}))
	updated := decodeRetro(t, recorder)
	if updated.IssueID == nil || *updated.IssueID != issueID {
		t.Fatalf("absent issue_id dropped the link: %v", updated.IssueID)
	}
	if updated.Title != "renamed" {
		t.Fatalf("title = %q, want renamed", updated.Title)
	}

	// Explicit null: detach.
	recorder = httptest.NewRecorder()
	testHandler.UpdateRetro(recorder, retroRequest(t, "PUT", retro.ID,
		json.RawMessage(`{"issue_id":null}`)))
	detached := decodeRetro(t, recorder)
	if detached.IssueID != nil {
		t.Fatalf("explicit null did not detach: %v", *detached.IssueID)
	}
}

// A retro id from another workspace must read as not-found, never as a
// forbidden resource that exists.
func TestGetRetro_HidesOtherWorkspaces(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id
	`, "retro-other", fmt.Sprintf("retro-other-%d", time.Now().UnixNano())).
		Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var foreignRetro string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO retro (workspace_id, author_type, author_id, title)
		VALUES ($1, 'member', $2, 'foreign') RETURNING id
	`, otherWorkspaceID, testUserID).Scan(&foreignRetro); err != nil {
		t.Fatalf("create foreign retro: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM retro WHERE workspace_id = $1`, otherWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	recorder := httptest.NewRecorder()
	testHandler.GetRetro(recorder, retroRequest(t, "GET", foreignRetro, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestDeleteRetro_RemovesIt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	retro := decodeRetro(t, postRetro(t, map[string]any{"title": "temp", "content": ""}))

	recorder := httptest.NewRecorder()
	testHandler.DeleteRetro(recorder, retroRequest(t, "DELETE", retro.ID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.GetRetro(recorder, retroRequest(t, "GET", retro.ID, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("retro still readable after delete: %d", recorder.Code)
	}
}

// Deleting a requirement must not take its retros with it — the lesson
// outlives the ticket, which is the reason this is not an issue field.
func TestRetro_SurvivesItsRequirementBeingDeleted(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "retro orphan source", "done", "none")
	retro := decodeRetro(t, postRetro(t, map[string]any{
		"title": "lesson", "content": "kept", "issue_id": issueID,
	}))
	cleanupRetro(t, retro.ID)

	deleteTestIssue(t, issueID)

	recorder := httptest.NewRecorder()
	testHandler.GetRetro(recorder, retroRequest(t, "GET", retro.ID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("retro was lost with its issue: %d", recorder.Code)
	}
	if got := decodeRetro(t, recorder); got.Content != "kept" {
		t.Fatalf("content = %q", got.Content)
	}
}
