package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// seedArchiveTree builds:
//
//	root
//	 ├── mid
//	 │    └── leaf     <- grandchild; archiving must reach it
//	 └── sibling
//	other              <- untouched control
func seedArchiveTree(t *testing.T) (map[string]string, string) {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("issue-archive-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"issue_archive_test":%q}`, token)

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

	insert := func(title string, parentID *string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				parent_issue_id, position, number, metadata
			) VALUES ($1, $2, 'done', 'none', 'member', $3, $4, 0, $5, $6::jsonb)
			RETURNING id
		`, testWorkspaceID, token+" "+title, testUserID, parentID, nextNumber(), metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	ids := map[string]string{}
	ids["root"] = insert("root", nil)
	ids["mid"] = insert("mid", &[]string{ids["root"]}[0])
	ids["leaf"] = insert("leaf", &[]string{ids["mid"]}[0])
	ids["sibling"] = insert("sibling", &[]string{ids["root"]}[0])
	ids["other"] = insert("other", nil)
	return ids, metadata
}

func archivedAt(t *testing.T, id string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT archived_at FROM issue WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatalf("read archived_at: %v", err)
	}
	return at
}

func postIssueArchive(t *testing.T, id, action string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+id+"/"+action, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if action == "archive" {
		testHandler.ArchiveIssue(recorder, req)
	} else {
		testHandler.UnarchiveIssue(recorder, req)
	}
	return recorder
}

// Archiving a requirement takes its whole subtree off the board. Parent-only
// archiving would strand the children in the list without the card that
// explained them.
func TestArchiveIssue_MovesTheWholeSubtree(t *testing.T) {
	ids, _ := seedArchiveTree(t)

	recorder := postIssueArchive(t, ids["root"], "archive")
	if recorder.Code != http.StatusOK {
		t.Fatalf("archive status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode archive response: %v", err)
	}
	if len(response.Issues) != 4 {
		t.Fatalf("archived %d issues, want 4 (root + mid + leaf + sibling)", len(response.Issues))
	}
	for _, issue := range response.Issues {
		if issue.ArchivedAt == nil {
			t.Fatalf("issue %s came back without archived_at", issue.Identifier)
		}
	}

	for _, key := range []string{"root", "mid", "leaf", "sibling"} {
		if archivedAt(t, ids[key]) == nil {
			t.Fatalf("%s should be archived", key)
		}
	}
	if archivedAt(t, ids["other"]) != nil {
		t.Fatalf("an unrelated issue was archived")
	}
}

// Archiving must not touch status: that is the whole reason it is a separate
// column. A restored issue has to come back as whatever it was.
func TestArchiveIssue_LeavesStatusAlone(t *testing.T) {
	ids, _ := seedArchiveTree(t)

	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET status = 'cancelled' WHERE id = $1`, ids["mid"]); err != nil {
		t.Fatalf("set status: %v", err)
	}

	postIssueArchive(t, ids["root"], "archive")
	postIssueArchive(t, ids["root"], "unarchive")

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM issue WHERE id = $1`, ids["mid"]).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("status = %q after archive round-trip, want cancelled", status)
	}
	if archivedAt(t, ids["mid"]) != nil {
		t.Fatalf("mid should be restored")
	}
}

// Archiving a mid-tree node leaves its ancestors alone — the parent is still
// live work.
func TestArchiveIssue_DoesNotClimbToAncestors(t *testing.T) {
	ids, _ := seedArchiveTree(t)

	postIssueArchive(t, ids["mid"], "archive")

	if archivedAt(t, ids["mid"]) == nil || archivedAt(t, ids["leaf"]) == nil {
		t.Fatalf("mid and leaf should be archived")
	}
	if archivedAt(t, ids["root"]) != nil {
		t.Fatalf("root is still live work and must not be archived")
	}
	if archivedAt(t, ids["sibling"]) != nil {
		t.Fatalf("sibling is outside the archived subtree")
	}
}

// Re-archiving is a conflict, not a silent rewrite of the original timestamp:
// "when did this leave the board" has to stay answerable.
func TestArchiveIssue_RejectsRepeatedTransitions(t *testing.T) {
	ids, _ := seedArchiveTree(t)

	postIssueArchive(t, ids["root"], "archive")
	first := archivedAt(t, ids["root"])

	recorder := postIssueArchive(t, ids["root"], "archive")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("second archive status = %d, want 409", recorder.Code)
	}
	if second := archivedAt(t, ids["root"]); second == nil || !second.Equal(*first) {
		t.Fatalf("archived_at was rewritten by the rejected call")
	}

	postIssueArchive(t, ids["root"], "unarchive")
	if recorder := postIssueArchive(t, ids["root"], "unarchive"); recorder.Code != http.StatusConflict {
		t.Fatalf("second unarchive status = %d, want 409", recorder.Code)
	}
}

// Archived issues drop out of the list unless the caller opts in.
func TestListIssues_HidesArchivedUnlessRequested(t *testing.T) {
	ids, metadata := seedArchiveTree(t)
	postIssueArchive(t, ids["mid"], "archive")

	list := func(query string) []string {
		t.Helper()
		path := fmt.Sprintf("/api/issues?workspace_id=%s&limit=100&metadata=%s%s",
			testWorkspaceID, url.QueryEscape(metadata), query)
		recorder := httptest.NewRecorder()
		testHandler.ListIssues(recorder, newRequest("GET", path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("list status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			Issues []IssueResponse `json:"issues"`
			Total  int64           `json:"total"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if int64(len(response.Issues)) != response.Total {
			t.Fatalf("total = %d but %d rows returned; the count ignores the archive filter",
				response.Total, len(response.Issues))
		}
		out := make([]string, 0, len(response.Issues))
		for _, issue := range response.Issues {
			out = append(out, issue.ID)
		}
		sort.Strings(out)
		return out
	}

	visible := list("")
	want := []string{ids["root"], ids["sibling"], ids["other"]}
	sort.Strings(want)
	if fmt.Sprint(visible) != fmt.Sprint(want) {
		t.Fatalf("default list = %v, want %v", visible, want)
	}

	if got := list("&include_archived=true"); len(got) != 5 {
		t.Fatalf("include_archived list returned %d issues, want 5", len(got))
	}
}

// Same rule on the board/table surface, which uses its own query builder.
func TestIssueTableRows_HidesArchivedUnlessRequested(t *testing.T) {
	ids, _ := seedArchiveTree(t)
	postIssueArchive(t, ids["mid"], "archive")

	rows := func(includeArchived bool) map[string]bool {
		t.Helper()
		recorder := httptest.NewRecorder()
		testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Filters: issueTableFiltersRequest{IncludeArchived: includeArchived},
				Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 100},
		}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("rows status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var response issueTableRowsResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode rows: %v", err)
		}
		seen := map[string]bool{}
		for _, row := range response.Rows {
			seen[row.Issue.ID] = true
		}
		return seen
	}

	if visible := rows(false); visible[ids["mid"]] || visible[ids["leaf"]] {
		t.Fatalf("archived issues leaked onto the board")
	}
	if all := rows(true); !all[ids["mid"]] || !all[ids["leaf"]] {
		t.Fatalf("include_archived did not bring the archived subtree back")
	}
}
