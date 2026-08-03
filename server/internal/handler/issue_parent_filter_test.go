package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// seedParentFilterTree builds this shape and returns the ids by label:
//
//	root      (no parent)
//	 ├── mid
//	 │    └── leaf        <- grandchild, the case direct-children filtering misses
//	 └── sibling
//	other     (unrelated top-level issue)
func seedParentFilterTree(t *testing.T) (map[string]string, string) {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("parent-filter-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"parent_filter_test":%q}`, token)

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
			) VALUES ($1, $2, 'todo', 'none', 'member', $3, $4, 0, $5, $6::jsonb)
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

// Filtering by a parent must keep the parent itself plus its WHOLE subtree.
// Direct-children-only would drop `leaf`, which is the case the requirement
// lens exists for.
func TestIssueTableRows_ParentFilterKeepsWholeSubtree(t *testing.T) {
	ids, _ := seedParentFilterTree(t)

	rows := func(parentIDs ...string) []string {
		t.Helper()
		recorder := httptest.NewRecorder()
		testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Filters: issueTableFiltersRequest{ParentIDs: parentIDs},
				Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group:     issueTableGroupSpec{Kind: "none"},
			Hierarchy: issueTableHierarchyRequest{Enabled: false},
			Page:      issueTablePageRequest{Limit: 100},
		}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("rows status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var response issueTableRowsResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode rows: %v", err)
		}
		got := make([]string, 0, len(response.Rows))
		for _, row := range response.Rows {
			got = append(got, row.Issue.ID)
		}
		sort.Strings(got)
		return got
	}

	assertIDs := func(name string, got []string, want ...string) {
		t.Helper()
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}

	assertIDs("root subtree", rows(ids["root"]),
		ids["root"], ids["mid"], ids["leaf"], ids["sibling"])
	assertIDs("mid subtree", rows(ids["mid"]), ids["mid"], ids["leaf"])
	assertIDs("leaf subtree", rows(ids["leaf"]), ids["leaf"])

	// OR across selected parents, and overlapping subtrees are not duplicated.
	assertIDs("union of disjoint subtrees", rows(ids["mid"], ids["other"]),
		ids["mid"], ids["leaf"], ids["other"])
	assertIDs("overlapping subtrees dedupe", rows(ids["root"], ids["mid"]),
		ids["root"], ids["mid"], ids["leaf"], ids["sibling"])
}

// A parent id from another workspace must never widen the window.
func TestIssueTableRows_ParentFilterIsWorkspaceScoped(t *testing.T) {
	ids, _ := seedParentFilterTree(t)
	ctx := context.Background()

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id
	`, "parent-filter-other", fmt.Sprintf("parent-filter-other-%d", time.Now().UnixNano())).
		Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	var foreignParent string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number
		) VALUES ($1, 'foreign parent', 'todo', 'none', 'member', $2, 0, 1)
		RETURNING id
	`, otherWorkspaceID, testUserID).Scan(&foreignParent); err != nil {
		t.Fatalf("create foreign issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, otherWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{ParentIDs: []string{foreignParent, ids["mid"]}},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group:     issueTableGroupSpec{Kind: "none"},
		Hierarchy: issueTableHierarchyRequest{Enabled: false},
		Page:      issueTablePageRequest{Limit: 100},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("rows status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response issueTableRowsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	for _, row := range response.Rows {
		if row.Issue.ID == foreignParent {
			t.Fatalf("foreign-workspace issue leaked into the window")
		}
	}
	if len(response.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (mid + leaf)", len(response.Rows))
	}
}

// A malformed parent id is a client error, not a 500 or a silently ignored
// filter that would show the user more rows than they asked for.
func TestIssueTableRows_ParentFilterRejectsInvalidUUID(t *testing.T) {
	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{ParentIDs: []string{"not-a-uuid"}},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  issueTablePageRequest{Limit: 10},
	}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

// The picker only offers issues that actually have children, and each entry's
// count is its subtree size including itself.
func TestListParentIssues_OffersParentsWithSubtreeSizes(t *testing.T) {
	ids, _ := seedParentFilterTree(t)

	recorder := httptest.NewRecorder()
	testHandler.ListParentIssues(recorder, newRequest("GET",
		"/api/issues/parents?workspace_id="+testWorkspaceID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Issues []ParentIssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode parents: %v", err)
	}

	sizes := map[string]int64{}
	for _, entry := range response.Issues {
		sizes[entry.ID] = entry.SubtreeSize
		if entry.Identifier == "" {
			t.Fatalf("entry %s has no identifier", entry.ID)
		}
	}

	if got := sizes[ids["root"]]; got != 4 {
		t.Fatalf("root subtree size = %d, want 4", got)
	}
	if got := sizes[ids["mid"]]; got != 2 {
		t.Fatalf("mid subtree size = %d, want 2", got)
	}
	if _, offered := sizes[ids["leaf"]]; offered {
		t.Fatalf("leaf has no children and must not be offered as a parent")
	}
	if _, offered := sizes[ids["other"]]; offered {
		t.Fatalf("childless top-level issue must not be offered as a parent")
	}
}

// Facet counts drive the numbers next to each picker row, so they must agree
// with what selecting that parent actually returns.
func TestIssueTableFacets_ParentCountsMatchSubtreeSizes(t *testing.T) {
	ids, metadata := seedParentFilterTree(t)

	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableFacets(recorder, newRequest("POST", "/api/issues/table/facets", issueTableFacetsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{Properties: nil},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Facets: []issueTableFacetSpec{{Kind: "parent"}},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response issueTableFacetsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode facets: %v", err)
	}
	if len(response.Facets) != 1 || response.Facets[0].Kind != "parent" {
		t.Fatalf("facets = %+v, want one parent facet", response.Facets)
	}

	counts := map[string]int{}
	for _, value := range response.Facets[0].Values {
		counts[value.Key] = int(value.Count)
	}
	if counts[ids["root"]] != 4 {
		t.Fatalf("root facet count = %d, want 4", counts[ids["root"]])
	}
	if counts[ids["mid"]] != 2 {
		t.Fatalf("mid facet count = %d, want 2", counts[ids["mid"]])
	}
	if _, present := counts[ids["leaf"]]; present {
		t.Fatalf("childless issue must not appear as a parent facet value")
	}
	_ = metadata
}
