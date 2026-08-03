package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// seedSiblings creates a parent with children whose creation order and
// position order deliberately disagree, so a test can tell which one a query
// actually used.
func seedSiblings(t *testing.T) (parentID string, byLabel map[string]string) {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("sibling-order-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"sibling_order_test":%q}`, token)

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

	insert := func(title string, parent *string, position float64, status string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				parent_issue_id, position, number, metadata
			) VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, $7, $8::jsonb)
			RETURNING id
		`, testWorkspaceID, token+" "+title, status, testUserID, parent, position,
			nextNumber(), metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	parentID = insert("parent", nil, 0, "todo")
	byLabel = map[string]string{"parent": parentID}
	// Created first but positioned last, and vice versa: creation order and
	// position order are exact opposites, in two different statuses so the
	// per-(workspace, status) scoping of position is exercised too.
	byLabel["created-first"] = insert("created first", &parentID, 300, "todo")
	byLabel["created-second"] = insert("created second", &parentID, 200, "in_progress")
	byLabel["created-third"] = insert("created third", &parentID, 100, "todo")
	return parentID, byLabel
}

func listChildIDs(t *testing.T, parentID string) []string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+parentID+"/children", nil)
	req = withURLParam(req, "id", parentID)
	testHandler.ListChildIssues(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ListChildIssues: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode children: %v", err)
	}
	ids := make([]string, 0, len(response.Issues))
	for _, issue := range response.Issues {
		ids = append(ids, issue.ID)
	}
	return ids
}

// Children nobody has reordered keep the creation order this endpoint has
// always produced. That is what makes adopting position safe for existing
// data: an untouched tree does not silently rearrange itself.
func TestListChildIssues_TiesFallBackToCreationOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	parentID, ids := seedSiblings(t)

	// Flatten position, which is the state of every issue that predates
	// anyone dragging it.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET position = 0 WHERE parent_issue_id = $1`, parentID); err != nil {
		t.Fatalf("flatten positions: %v", err)
	}

	got := listChildIDs(t, parentID)
	want := []string{ids["created-first"], ids["created-second"], ids["created-third"]}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("children = %v, want %v (creation order)", got, want)
	}
}

// The batched variant feeds the swimlane's child lanes. It has to agree with
// the single-parent query, or the same children sort differently depending on
// which screen is open.
func TestListChildrenByParents_MatchesSingleParentOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	parentID, _ := seedSiblings(t)

	recorder := httptest.NewRecorder()
	testHandler.ListChildrenByParents(recorder, newRequest("GET",
		"/api/issues/children?workspace_id="+testWorkspaceID+"&parent_ids="+parentID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("ListChildrenByParents: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var batched struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&batched); err != nil {
		t.Fatalf("decode batched children: %v", err)
	}
	batchedIDs := make([]string, 0, len(batched.Issues))
	for _, issue := range batched.Issues {
		batchedIDs = append(batchedIDs, issue.ID)
	}

	if single := listChildIDs(t, parentID); fmt.Sprint(batchedIDs) != fmt.Sprint(single) {
		t.Fatalf("batched order %v != single-parent order %v", batchedIDs, single)
	}
}
