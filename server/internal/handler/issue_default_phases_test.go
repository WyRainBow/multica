package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuephase"
)

// Every new issue starts with a route. Seeded inside the create transaction,
// so the interesting question is not "can a phase be created" — issue_phase.go
// already covers that — but whether an issue can exist without one.

func createIssueViaAPI(t *testing.T, body map[string]any) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, created.ID) })
	return created
}

func listPhaseNames(t *testing.T, issueID string) []string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/phases", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssuePhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssuePhases: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Phases []IssuePhaseResponse `json:"phases"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode phases: %v", err)
	}
	names := make([]string, 0, len(resp.Phases))
	for _, phase := range resp.Phases {
		names = append(names, phase.Name)
	}
	return names
}

func TestCreateIssue_SeedsTheDefaultRoute(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	created := createIssueViaAPI(t, map[string]any{"title": "issue with a route"})

	got := listPhaseNames(t, created.ID)
	if len(got) != len(issuephase.DefaultRoute) {
		t.Fatalf("phases = %v, want %v", got, issuephase.DefaultRoute)
	}
	// Order is the point: the stations are a route, and reading them out of
	// order says nothing. ListIssuePhases sorts by position, so this also pins
	// that the seeded positions ascend.
	for i, want := range issuephase.DefaultRoute {
		if got[i] != want {
			t.Fatalf("phase %d = %q, want %q (full: %v)", i, got[i], want, got)
		}
	}
}

// A sub-issue is an issue. Carving out an exception would make "does this have
// stations" depend on where it sits in a tree.
func TestCreateIssue_SeedsTheRouteOnSubIssuesToo(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	parent := createIssueViaAPI(t, map[string]any{"title": "parent with a child"})
	child := createIssueViaAPI(t, map[string]any{
		"title":           "child issue",
		"parent_issue_id": parent.ID,
	})

	if got := listPhaseNames(t, child.ID); len(got) != len(issuephase.DefaultRoute) {
		t.Fatalf("sub-issue phases = %v, want %v", got, issuephase.DefaultRoute)
	}
}

// Seeded, not entered. State is derived from the two timestamps, and a brand
// new issue has not arrived anywhere yet — marking the first station entered
// would record a route the work has not taken.
func TestCreateIssue_SeededPhasesStartPending(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	created := createIssueViaAPI(t, map[string]any{"title": "route starts pending"})

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+created.ID+"/phases", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ListIssuePhases(w, req)

	var resp struct {
		Phases []IssuePhaseResponse `json:"phases"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	for _, phase := range resp.Phases {
		if phase.EnteredAt != nil {
			t.Fatalf("phase %q was seeded already entered (%v)", phase.Name, *phase.EnteredAt)
		}
		if phase.CompletedAt != nil {
			t.Fatalf("phase %q was seeded already completed (%v)", phase.Name, *phase.CompletedAt)
		}
	}
}

// The route is seeded, then owned by the user: applying the template again, or
// adding a station by hand, still has to hit the duplicate guard rather than
// producing two stations that read the same.
func TestCreateIssue_SeededRouteStillRejectsADuplicateName(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	created := createIssueViaAPI(t, map[string]any{"title": "route is still guarded"})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+created.ID+"/phases",
		map[string]any{"name": issuephase.DefaultRoute[0]})
	req = withURLParam(req, "id", created.ID)
	testHandler.CreateIssuePhase(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("re-adding a seeded phase: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
