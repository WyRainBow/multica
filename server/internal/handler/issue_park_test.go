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

// seedParkTree builds:
//
//	parent
//	 ├── child        <- the optimization noticed mid-flight
//	 └── sibling      <- ordinary sub-issue, must stay put
func seedParkTree(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("issue-park-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"issue_park_test":%q}`, token)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
	})

	nextNumber := func() int {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		return number
	}

	insert := func(title string, parentID *string, stage *int) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				parent_issue_id, stage, position, number, metadata
			) VALUES ($1, $2, 'in_progress', 'none', 'member', $3, $4, $5, 0, $6, $7::jsonb)
			RETURNING id
		`, testWorkspaceID, token+" "+title, testUserID, parentID, stage,
			nextNumber(), metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	ids := map[string]string{}
	ids["parent"] = insert("parent", nil, nil)
	stage := 1
	ids["child"] = insert("child", &[]string{ids["parent"]}[0], &stage)
	ids["sibling"] = insert("sibling", &[]string{ids["parent"]}[0], &stage)
	return ids
}

func postIssuePark(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+id+"/park", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.ParkIssue(recorder, req)
	return recorder
}

func getParkedFrom(t *testing.T, id string) []IssueResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+id+"/parked", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.ListParkedFromIssue(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list parked status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode parked list: %v", err)
	}
	return response.Issues
}

func parkIssueOK(t *testing.T, id string) IssueResponse {
	t.Helper()
	recorder := postIssuePark(t, id)
	if recorder.Code != http.StatusOK {
		t.Fatalf("park status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var parked IssueResponse
	if err := json.NewDecoder(recorder.Body).Decode(&parked); err != nil {
		t.Fatalf("decode park response: %v", err)
	}
	return parked
}

// The whole point: parking detaches, so archiving the finished parent no
// longer takes the parked issue with it.
func TestParkIssue_DetachesAndRecordsTheOrigin(t *testing.T) {
	ids := seedParkTree(t)

	parked := parkIssueOK(t, ids["child"])

	if parked.ParentIssueID != nil {
		t.Fatalf("parked issue still has parent %q", *parked.ParentIssueID)
	}
	if parked.ParkedFromIssueID == nil || *parked.ParkedFromIssueID != ids["parent"] {
		t.Fatalf("parked_from_issue_id = %v, want %q", parked.ParkedFromIssueID, ids["parent"])
	}
	if parked.Status != "backlog" {
		t.Fatalf("status = %q, want backlog", parked.Status)
	}
}

// A stage is a barrier group among siblings. Keeping it after the issue has
// left the family would leave the parent waiting on a member that is no longer
// part of it.
func TestParkIssue_ClearsTheStage(t *testing.T) {
	ids := seedParkTree(t)

	parked := parkIssueOK(t, ids["child"])

	if parked.Stage != nil {
		t.Fatalf("stage = %v, want nil after parking", *parked.Stage)
	}
}

// Archiving the parent must no longer reach the parked issue — this is the
// exact loss the feature exists to prevent.
func TestParkIssue_SurvivesArchivingTheOrigin(t *testing.T) {
	ids := seedParkTree(t)
	parkIssueOK(t, ids["child"])

	recorder := postIssueArchive(t, ids["parent"], "archive")
	if recorder.Code != http.StatusOK {
		t.Fatalf("archive status = %d: %s", recorder.Code, recorder.Body.String())
	}

	if at := archivedAt(t, ids["child"]); at != nil {
		t.Fatalf("parked issue was archived with its former parent (archived_at = %v)", at)
	}
	// Control: the sibling that stayed in the family is archived as before.
	if at := archivedAt(t, ids["sibling"]); at == nil {
		t.Fatalf("sibling should still be archived with the parent")
	}
}

// Parking twice must keep pointing at where the thought started, not at the
// most recent waypoint: an issue can be re-attached to some other parent and
// lifted out again, and the first origin is the one that explains it.
func TestParkIssue_KeepsTheFirstOrigin(t *testing.T) {
	ids := seedParkTree(t)
	parkIssueOK(t, ids["child"])

	// Re-attach under a different parent, then park again.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET parent_issue_id = $1 WHERE id = $2`,
		ids["sibling"], ids["child"]); err != nil {
		t.Fatalf("re-attach: %v", err)
	}

	parked := parkIssueOK(t, ids["child"])

	if parked.ParkedFromIssueID == nil || *parked.ParkedFromIssueID != ids["parent"] {
		t.Fatalf("parked_from_issue_id = %v, want the original parent %q",
			parked.ParkedFromIssueID, ids["parent"])
	}
}

// Parking a top-level issue records no origin rather than pointing at itself.
// It still means something — "not current work" — so it is allowed.
func TestParkIssue_TopLevelRecordsNoOrigin(t *testing.T) {
	ids := seedParkTree(t)

	parked := parkIssueOK(t, ids["parent"])

	if parked.ParkedFromIssueID != nil {
		t.Fatalf("parked_from_issue_id = %q, want null for a top-level issue",
			*parked.ParkedFromIssueID)
	}
	if parked.Status != "backlog" {
		t.Fatalf("status = %q, want backlog", parked.Status)
	}
}

// Parking an archived issue is refused: it would silently resurrect a decision
// that was already made about the issue.
func TestParkIssue_RefusesAnArchivedIssue(t *testing.T) {
	ids := seedParkTree(t)
	if recorder := postIssueArchive(t, ids["child"], "archive"); recorder.Code != http.StatusOK {
		t.Fatalf("archive status = %d", recorder.Code)
	}

	if recorder := postIssuePark(t, ids["child"]); recorder.Code != http.StatusConflict {
		t.Fatalf("park status = %d, want 409", recorder.Code)
	}
}

// The reverse view — what this requirement left open — is what makes a parked
// issue findable from the work that produced it.
func TestListParkedFromIssue_ReturnsWhatWasLiftedOut(t *testing.T) {
	ids := seedParkTree(t)

	if before := getParkedFrom(t, ids["parent"]); len(before) != 0 {
		t.Fatalf("expected no parked issues before parking, got %d", len(before))
	}

	parkIssueOK(t, ids["child"])

	after := getParkedFrom(t, ids["parent"])
	if len(after) != 1 {
		t.Fatalf("parked list has %d issues, want 1", len(after))
	}
	if after[0].ID != ids["child"] {
		t.Fatalf("parked list returned %q, want %q", after[0].ID, ids["child"])
	}
	// The sibling never left the family, so it must not appear here.
	for _, issue := range after {
		if issue.ID == ids["sibling"] {
			t.Fatalf("sibling appears in the parked list")
		}
	}
}

// An archived parked issue is done with and drops out of the reverse view;
// otherwise the list only ever grows.
func TestListParkedFromIssue_ExcludesArchived(t *testing.T) {
	ids := seedParkTree(t)
	parkIssueOK(t, ids["child"])

	if recorder := postIssueArchive(t, ids["child"], "archive"); recorder.Code != http.StatusOK {
		t.Fatalf("archive status = %d", recorder.Code)
	}

	if after := getParkedFrom(t, ids["parent"]); len(after) != 0 {
		t.Fatalf("archived parked issue still listed (%d rows)", len(after))
	}
}
