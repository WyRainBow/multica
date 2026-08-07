package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A resource row is rendered as a clickable link, so what the server accepts
// into that column is a security question, not just a validation one.

func postResource(t *testing.T, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/resources", body)
	req = withURLParam(req, "id", issueID)
	testHandler.CreateIssueResource(recorder, req)
	return recorder
}

func decodeResource(t *testing.T, recorder *httptest.ResponseRecorder) IssueResourceResponse {
	t.Helper()
	if recorder.Code != http.StatusCreated {
		t.Fatalf("CreateIssueResource: expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var resource IssueResourceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resource); err != nil {
		t.Fatalf("decode resource: %v", err)
	}
	return resource
}

func listResources(t *testing.T, issueID string) []IssueResourceResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/resources", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueResources(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ListIssueResources: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Resources []IssueResourceResponse `json:"resources"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resources: %v", err)
	}
	return resp.Resources
}

func TestCreateIssueResource_StoresTheLink(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource basics", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeResource(t, postResource(t, issueID, map[string]any{
		"url":   "https://example.feishu.cn/docx/abc123",
		"title": "智能纪要：沟通会",
	}))
	if created.URL != "https://example.feishu.cn/docx/abc123" {
		t.Fatalf("url = %q", created.URL)
	}
	if created.Title != "智能纪要：沟通会" {
		t.Fatalf("title = %q", created.Title)
	}
	if got := listResources(t, issueID); len(got) != 1 || got[0].ID != created.ID {
		t.Fatalf("list returned %d resources, want the one just created", len(got))
	}
}

// The row is a clickable link. A scheme that executes rather than navigates
// would turn "add a resource" into running code in whoever opens the issue.
func TestCreateIssueResource_RejectsNonHTTPSchemes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource scheme guard", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"ftp://example.com/x",
		// No host: "https:notaurl" parses but points nowhere.
		"https:notaurl",
		"   ",
		"",
	} {
		recorder := postResource(t, issueID, map[string]any{"url": bad})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("url %q: expected 400, got %d: %s", bad, recorder.Code, recorder.Body.String())
		}
	}
	if got := listResources(t, issueID); len(got) != 0 {
		t.Fatalf("a rejected url was stored anyway: %v", got)
	}
}

// A title is optional: the point is attaching the page, and demanding a name
// first is the friction that stops people attaching it at all.
func TestCreateIssueResource_TitleIsOptional(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource untitled", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeResource(t, postResource(t, issueID, map[string]any{
		"url": "https://example.com/page",
	}))
	if created.Title != "" {
		t.Fatalf("title = %q, want empty", created.Title)
	}
}

// Order is arranged by hand, so it has to be stable and appendable.
func TestCreateIssueResource_AppendsInOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource order", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	first := decodeResource(t, postResource(t, issueID, map[string]any{"url": "https://example.com/1"}))
	second := decodeResource(t, postResource(t, issueID, map[string]any{"url": "https://example.com/2"}))
	if second.Position <= first.Position {
		t.Fatalf("positions did not advance: %d then %d", first.Position, second.Position)
	}

	got := listResources(t, issueID)
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("list is not in insertion order: %v", got)
	}
}

func TestUpdateIssueResource_RenamesWithoutResendingTheURL(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource rename", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	created := decodeResource(t, postResource(t, issueID, map[string]any{
		"url": "https://example.com/page", "title": "旧标题",
	}))

	recorder := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/resources/"+created.ID,
		map[string]any{"title": "新标题"})
	req = withURLParams(req, "id", issueID, "resourceId", created.ID)
	testHandler.UpdateIssueResource(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("UpdateIssueResource: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var updated IssueResourceResponse
	json.NewDecoder(recorder.Body).Decode(&updated)
	if updated.Title != "新标题" {
		t.Fatalf("title = %q", updated.Title)
	}
	if updated.URL != "https://example.com/page" {
		t.Fatalf("url changed to %q when only the title was sent", updated.URL)
	}
}

// An empty PUT would look like a successful no-op while the caller believes
// something changed — most likely a misspelled field name.
func TestUpdateIssueResource_RefusesAnEmptyUpdate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource empty update", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	created := decodeResource(t, postResource(t, issueID, map[string]any{"url": "https://example.com/x"}))

	recorder := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/resources/"+created.ID, map[string]any{})
	req = withURLParams(req, "id", issueID, "resourceId", created.ID)
	testHandler.UpdateIssueResource(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on an empty update, got %d", recorder.Code)
	}
}

// A resource id from another issue must not be reachable through this issue's
// path — otherwise the path segment is decoration rather than a scope.
func TestIssueResource_CannotBeReachedThroughAnotherIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ownerID := createTestIssue(t, "resource owner", "todo", "none")
	otherID := createTestIssue(t, "resource other", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, ownerID); deleteTestIssue(t, otherID) })
	created := decodeResource(t, postResource(t, ownerID, map[string]any{"url": "https://example.com/owned"}))

	recorder := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+otherID+"/resources/"+created.ID, nil)
	req = withURLParams(req, "id", otherID, "resourceId", created.ID)
	testHandler.DeleteIssueResource(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 reaching across issues, got %d", recorder.Code)
	}
	if got := listResources(t, ownerID); len(got) != 1 {
		t.Fatalf("the resource was deleted through another issue's path")
	}
}

func TestDeleteIssueResource_RemovesIt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource delete", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	created := decodeResource(t, postResource(t, issueID, map[string]any{"url": "https://example.com/gone"}))

	recorder := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+issueID+"/resources/"+created.ID, nil)
	req = withURLParams(req, "id", issueID, "resourceId", created.ID)
	testHandler.DeleteIssueResource(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := listResources(t, issueID); len(got) != 0 {
		t.Fatalf("resource survived the delete: %v", got)
	}
}

// A finished issue's BODY is frozen; attaching a page to it is not editing the
// record, it is filing something next to it. Same reasoning that keeps comments
// working on a done issue.
func TestCreateIssueResource_WorksOnAFinishedIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "resource on finished", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	putIssue(t, issueID, map[string]any{"status": "done"})

	recorder := postResource(t, issueID, map[string]any{"url": "https://example.com/postmortem"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 on a finished issue, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
