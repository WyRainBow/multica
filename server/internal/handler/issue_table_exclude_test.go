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

// excludeFixture builds one issue per interesting shape, so a single seed can
// answer every "everything except X" question:
//
//	backlog   — backlog status, no project, no assignee, no label
//	todo      — todo status, project A, label A, assigned
//	done      — done status, no project, no assignee, no label
//	child     — todo status, sub-issue of todo
type excludeFixture struct {
	ids      map[string]string
	metadata string
	project  string
	label    string
}

func seedExcludeFixture(t *testing.T) excludeFixture {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("table-exclude-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"table_exclude_test":%q}`, token)

	var project string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, token+" project").Scan(&project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	var label string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_label (workspace_id, name, color)
		VALUES ($1, $2, '#ef4444') RETURNING id
	`, testWorkspaceID, token+" label").Scan(&label); err != nil {
		t.Fatalf("create label: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
		_, _ = testPool.Exec(bg, `DELETE FROM issue_label WHERE id = $1`, label)
		_, _ = testPool.Exec(bg, `DELETE FROM project WHERE id = $1`, project)
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

	insert := func(title, status string, projectID, assignee, parent *string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				assignee_type, assignee_id, project_id, parent_issue_id,
				position, number, metadata
			) VALUES (
				$1, $2, $3, 'none', 'member', $4,
				CASE WHEN $5::uuid IS NULL THEN NULL ELSE 'member' END, $5,
				$6, $7, 0, $8, $9::jsonb
			)
			RETURNING id
		`, testWorkspaceID, token+" "+title, status, testUserID, assignee, projectID,
			parent, nextNumber(), metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	user := testUserID
	ids := map[string]string{}
	ids["backlog"] = insert("backlog", "backlog", nil, nil, nil)
	ids["todo"] = insert("todo", "todo", &project, &user, nil)
	ids["done"] = insert("done", "done", nil, nil, nil)
	ids["child"] = insert("child", "todo", nil, nil, &[]string{ids["todo"]}[0])

	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $2)`,
		ids["todo"], label); err != nil {
		t.Fatalf("attach label: %v", err)
	}

	return excludeFixture{ids: ids, metadata: metadata, project: project, label: label}
}

// rowsMatching runs the table query and returns the fixture's issue ids that
// came back, so assertions read as set comparisons rather than index math.
func (f excludeFixture) rowsMatching(t *testing.T, filters issueTableFiltersRequest) []string {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows",
		issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Filters: filters,
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

	byID := map[string]string{}
	for name, id := range f.ids {
		byID[id] = name
	}
	var got []string
	for _, row := range response.Rows {
		if name, ok := byID[row.Issue.ID]; ok {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	return got
}

// rowsMatchingRaw sends the filters as a literal JSON object.
//
// Needed for the explicitly-empty cases: the typed request struct tags these
// lists `omitempty`, so an empty slice never reaches the wire and the server
// sees "absent" instead of "empty" — the exact distinction under test.
func (f excludeFixture) rowsMatchingRaw(t *testing.T, filters map[string]any) []string {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows",
		map[string]any{
			"query": map[string]any{
				"scope":   map[string]any{"kind": "workspace"},
				"filters": filters,
				"sort":    map[string]any{"field": "position", "direction": "asc"},
			},
			"group":     map[string]any{"kind": "none"},
			"hierarchy": map[string]any{"enabled": false},
			"page":      map[string]any{"limit": 100},
		}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("rows status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response issueTableRowsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	byID := map[string]string{}
	for name, id := range f.ids {
		byID[id] = name
	}
	var got []string
	for _, row := range response.Rows {
		if name, ok := byID[row.Issue.ID]; ok {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	return got
}

func assertSameSet(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("matched %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("matched %v, want %v", got, want)
		}
	}
}

// The headline case: "everything except backlog", which is the reason the
// feature exists — ticking the other six statuses by hand is both tedious and
// wrong the moment a new status is added.
func TestIssueTableExclude_Status(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		Statuses:   []string{"backlog"},
		StatusMode: issueTableFilterExclude,
	})

	assertSameSet(t, got, "todo", "done", "child")
}

// An absent mode must behave exactly as before, so a client that predates
// exclusion keeps working against a newer backend.
func TestIssueTableExclude_AbsentModeStillIncludes(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{Statuses: []string{"backlog"}})

	assertSameSet(t, got, "backlog")
}

// The NULL trap. `NOT (project_id = ANY(...))` is NULL for an issue with no
// project, and a NULL predicate drops the row — so a naive negation would hide
// every project-less issue while claiming to exclude only one project.
func TestIssueTableExclude_ProjectKeepsIssuesWithNoProject(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		ProjectIDs:  []string{f.project},
		ProjectMode: issueTableFilterExclude,
	})

	// Everything but the one issue actually in that project — including the
	// three that have no project at all.
	assertSameSet(t, got, "backlog", "done", "child")
}

// Same trap for assignee: excluding a person must not also hide everything
// that is unassigned.
func TestIssueTableExclude_AssigneeKeepsUnassigned(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		Assignees:    []issueTableActorRef{{Type: "member", ID: testUserID}},
		AssigneeMode: issueTableFilterExclude,
	})

	assertSameSet(t, got, "backlog", "done", "child")
}

// Excluding a label must keep every issue that carries no labels at all.
func TestIssueTableExclude_LabelKeepsUnlabelled(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		LabelIDs:  []string{f.label},
		LabelMode: issueTableFilterExclude,
	})

	assertSameSet(t, got, "backlog", "done", "child")
}

// Excluding a requirement drops its whole subtree, mirroring the including
// form — which keeps the parent plus everything below it.
func TestIssueTableExclude_ParentDropsTheWholeSubtree(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		ParentIDs:  []string{f.ids["todo"]},
		ParentMode: issueTableFilterExclude,
	})

	// Neither the requirement nor its child.
	assertSameSet(t, got, "backlog", "done")
}

// Excluding nothing removes nothing. The including form treats an explicitly
// empty assignee list as "match none"; the excluding form must not inherit
// that, or turning the mode to exclude with no selection would blank the list.
func TestIssueTableExclude_EmptyAssigneeListIsNoFilter(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatchingRaw(t, map[string]any{
		"assignees":     []any{},
		"assignee_mode": "exclude",
	})

	assertSameSet(t, got, "backlog", "todo", "done", "child")
}

// Control for the case above: the including form still means "match none".
func TestIssueTableExclude_EmptyAssigneeListStillMatchesNoneWhenIncluding(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatchingRaw(t, map[string]any{"assignees": []any{}})

	assertSameSet(t, got)
}

// Two excluding categories intersect: each removes its own set.
func TestIssueTableExclude_CombinesAcrossCategories(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		Statuses:    []string{"backlog"},
		StatusMode:  issueTableFilterExclude,
		ProjectIDs:  []string{f.project},
		ProjectMode: issueTableFilterExclude,
	})

	assertSameSet(t, got, "done", "child")
}

// One category can include while another excludes.
func TestIssueTableExclude_MixesWithIncludingCategories(t *testing.T) {
	f := seedExcludeFixture(t)

	got := f.rowsMatching(t, issueTableFiltersRequest{
		Statuses:    []string{"todo"},
		ProjectIDs:  []string{f.project},
		ProjectMode: issueTableFilterExclude,
	})

	// todo status, minus the one in the excluded project.
	assertSameSet(t, got, "child")
}

func TestIssueTableExclude_RejectsAnUnknownMode(t *testing.T) {
	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows",
		issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope: issueTableScope{Kind: "workspace"},
				Filters: issueTableFiltersRequest{
					Statuses:   []string{"backlog"},
					StatusMode: issueTableFilterMode("maybe"),
				},
				Sort: issueTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group:     issueTableGroupSpec{Kind: "none"},
			Hierarchy: issueTableHierarchyRequest{Enabled: false},
			Page:      issueTablePageRequest{Limit: 10},
		}))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// The fingerprint decides whether a cached page can be reused. Include and
// exclude return different rows, so they must not share one.
func TestIssueTableExclude_ChangesTheQueryFingerprint(t *testing.T) {
	base := issueTableQuerySpec{
		Scope:   issueTableScope{Kind: "workspace"},
		Filters: issueTableFiltersRequest{Statuses: []string{"backlog"}},
		Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
	}
	excluding := base
	excluding.Filters.StatusMode = issueTableFilterExclude

	including, err := canonicalIssueTableFingerprint(testWorkspaceID, base)
	if err != nil {
		t.Fatalf("fingerprint including: %v", err)
	}
	excluded, err := canonicalIssueTableFingerprint(testWorkspaceID, excluding)
	if err != nil {
		t.Fatalf("fingerprint excluding: %v", err)
	}
	if including == excluded {
		t.Fatalf("include and exclude share fingerprint %q", including)
	}
}
