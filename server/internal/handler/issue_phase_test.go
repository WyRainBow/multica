package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func seedPhaseIssue(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("issue-phase-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"issue_phase_test":%q}`, token)

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg,
			`DELETE FROM issue_phase WHERE issue_id IN (SELECT id FROM issue WHERE metadata @> $1::jsonb)`,
			metadata)
		_, _ = testPool.Exec(bg,
			`DELETE FROM comment WHERE issue_id IN (SELECT id FROM issue WHERE metadata @> $1::jsonb)`,
			metadata)
		_, _ = testPool.Exec(bg, `DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
	})

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, metadata
		) VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5::jsonb)
		RETURNING id
	`, testWorkspaceID, token+" issue", testUserID, number, metadata).Scan(&id); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return id
}

func phaseRequest(t *testing.T, method, path, issueID, phaseID string, body any,
	handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest(method, path, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	if phaseID != "" {
		rctx.URLParams.Add("phaseId", phaseID)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handler(recorder, req)
	return recorder
}

func createPhase(t *testing.T, issueID, name string) IssuePhaseResponse {
	t.Helper()
	recorder := phaseRequest(t, "POST", "/api/issues/x/phases", issueID, "",
		map[string]any{"name": name}, testHandler.CreateIssuePhase)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create phase status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var phase IssuePhaseResponse
	if err := json.NewDecoder(recorder.Body).Decode(&phase); err != nil {
		t.Fatalf("decode phase: %v", err)
	}
	return phase
}

func listPhases(t *testing.T, issueID string) []IssuePhaseResponse {
	t.Helper()
	recorder := phaseRequest(t, "GET", "/api/issues/x/phases", issueID, "", nil,
		testHandler.ListIssuePhases)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list phases status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Phases []IssuePhaseResponse `json:"phases"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode phases: %v", err)
	}
	return response.Phases
}

// Stations are a route: reading them back out of order says nothing. Creating
// without a position appends, which is what adding a station almost always
// means.
func TestIssuePhases_AppendInTrackOrder(t *testing.T) {
	issueID := seedPhaseIssue(t)

	createPhase(t, issueID, "开始")
	createPhase(t, issueID, "已冻结")
	createPhase(t, issueID, "实施中")

	names := []string{}
	for _, phase := range listPhases(t, issueID) {
		names = append(names, phase.Name)
	}
	want := []string{"开始", "已冻结", "实施中"}
	if len(names) != len(want) {
		t.Fatalf("phases = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("phases = %v, want %v", names, want)
		}
	}
}

// Timestamps are derived from the transition, never accepted from the caller —
// one somebody has to remember to type is one that is wrong.
func TestIssuePhases_EnterDerivesTheTimestamp(t *testing.T) {
	issueID := seedPhaseIssue(t)
	phase := createPhase(t, issueID, "实施中")

	if phase.EnteredAt != nil {
		t.Fatalf("a new phase should not be entered yet")
	}

	recorder := phaseRequest(t, "POST", "/api/issues/x/phases/y/enter", issueID, phase.ID,
		nil, testHandler.EnterIssuePhase)
	if recorder.Code != http.StatusOK {
		t.Fatalf("enter status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var entered IssuePhaseResponse
	if err := json.NewDecoder(recorder.Body).Decode(&entered); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entered.EnteredAt == nil {
		t.Fatalf("entered_at was not set")
	}
}

// "When did we get here" has one answer even if the issue bounced back later,
// so re-entering keeps the original arrival time.
func TestIssuePhases_ReEnterKeepsTheFirstArrival(t *testing.T) {
	issueID := seedPhaseIssue(t)
	phase := createPhase(t, issueID, "实施中")

	first := phaseRequest(t, "POST", "/e", issueID, phase.ID, nil, testHandler.EnterIssuePhase)
	var one IssuePhaseResponse
	_ = json.NewDecoder(first.Body).Decode(&one)

	second := phaseRequest(t, "POST", "/e", issueID, phase.ID, nil, testHandler.EnterIssuePhase)
	var two IssuePhaseResponse
	_ = json.NewDecoder(second.Body).Decode(&two)

	if one.EnteredAt == nil || two.EnteredAt == nil || *one.EnteredAt != *two.EnteredAt {
		t.Fatalf("entered_at changed on re-entry: %v -> %v", one.EnteredAt, two.EnteredAt)
	}
}

// Re-entering a finished station makes it current again — the work came back.
func TestIssuePhases_ReEnterClearsCompletion(t *testing.T) {
	issueID := seedPhaseIssue(t)
	phase := createPhase(t, issueID, "实施中")

	phaseRequest(t, "POST", "/e", issueID, phase.ID, nil, testHandler.EnterIssuePhase)
	phaseRequest(t, "POST", "/c", issueID, phase.ID, nil, testHandler.CompleteIssuePhase)

	recorder := phaseRequest(t, "POST", "/e", issueID, phase.ID, nil, testHandler.EnterIssuePhase)
	var reentered IssuePhaseResponse
	if err := json.NewDecoder(recorder.Body).Decode(&reentered); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reentered.CompletedAt != nil {
		t.Fatalf("completed_at survived re-entry: %v", *reentered.CompletedAt)
	}
}

// Completing a station never entered would record a route the work did not
// take.
func TestIssuePhases_RefusesCompletingAnUnenteredPhase(t *testing.T) {
	issueID := seedPhaseIssue(t)
	phase := createPhase(t, issueID, "等待部署")

	recorder := phaseRequest(t, "POST", "/c", issueID, phase.ID, nil,
		testHandler.CompleteIssuePhase)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

func TestIssuePhases_RejectsABlankName(t *testing.T) {
	issueID := seedPhaseIssue(t)
	recorder := phaseRequest(t, "POST", "/p", issueID, "",
		map[string]any{"name": "   "}, testHandler.CreateIssuePhase)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// A phase id from another issue must not be reachable through this issue's
// path — otherwise the issue in the URL means nothing.
func TestIssuePhases_RefusesAPhaseFromAnotherIssue(t *testing.T) {
	issueA := seedPhaseIssue(t)
	issueB := seedPhaseIssue(t)
	phaseOnB := createPhase(t, issueB, "实施中")

	recorder := phaseRequest(t, "POST", "/e", issueA, phaseOnB.ID, nil,
		testHandler.EnterIssuePhase)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

// The reason the feature exists: a comment gets an owning station, and the
// list says how much each station holds.
func TestIssuePhases_CommentCountsPerPhase(t *testing.T) {
	issueID := seedPhaseIssue(t)
	phase := createPhase(t, issueID, "已冻结")

	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, phase_id)
		VALUES ($1, $2, 'member', $3, 'a decision made while frozen', $4)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID, phase.ID).Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	phases := listPhases(t, issueID)
	if len(phases) != 1 {
		t.Fatalf("expected one phase, got %d", len(phases))
	}
	if phases[0].CommentCount != 1 {
		t.Fatalf("comment_count = %d, want 1", phases[0].CommentCount)
	}
}

// Deleting a station must not delete what was said there. The comments fall
// back to ungrouped — where every comment written before phases existed
// already sits.
func TestIssuePhases_DeleteDetachesCommentsInsteadOfRemovingThem(t *testing.T) {
	issueID := seedPhaseIssue(t)
	phase := createPhase(t, issueID, "已冻结")

	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, phase_id)
		VALUES ($1, $2, 'member', $3, 'still worth keeping', $4)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID, phase.ID).Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	recorder := phaseRequest(t, "DELETE", "/p", issueID, phase.ID, nil,
		testHandler.DeleteIssuePhase)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d: %s", recorder.Code, recorder.Body.String())
	}

	var content string
	var phaseID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content, phase_id FROM comment WHERE id = $1`, commentID).
		Scan(&content, &phaseID); err != nil {
		t.Fatalf("comment was deleted along with its phase: %v", err)
	}
	if phaseID != nil {
		t.Fatalf("comment still points at the deleted phase")
	}
}
