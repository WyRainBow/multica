package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GET /api/comments/{commentId} exists so a comment id copied off a card in
// the app is worth something on its own. Reaching the same comment through the
// list endpoint costs the issue's whole thread, which is the token bill this
// route is here to avoid.

func getComment(t *testing.T, commentID string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/comments/"+commentID, nil)
	req = withURLParam(req, "commentId", commentID)
	testHandler.GetComment(recorder, req)
	return recorder
}

func TestGetComment_ReturnsTheComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "comment get", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "读回来的应该正好是这一条",
	}))

	recorder := getComment(t, created.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GetComment: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var got CommentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("id = %s, want %s", got.ID, created.ID)
	}
	if got.Content != "读回来的应该正好是这一条" {
		t.Fatalf("content = %q, want the created body", got.Content)
	}
	if got.IssueID != issueID {
		t.Fatalf("issue_id = %s, want %s", got.IssueID, issueID)
	}
}

// A reply is reachable the same way as a root. Nothing about the route depends
// on the comment being top-level, and parent_id has to survive so a reader can
// walk back up to the thread it belongs to.
func TestGetComment_ReturnsARepliesParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "comment get reply", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	root := decodeComment(t, postComment(t, issueID, map[string]any{"content": "root"}))
	reply := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":   "reply",
		"parent_id": root.ID,
	}))

	recorder := getComment(t, reply.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GetComment: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var got CommentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if got.ParentID == nil || *got.ParentID != root.ID {
		t.Fatalf("parent_id = %v, want %s", got.ParentID, root.ID)
	}
}

// A well-formed id that is not in this workspace reads as 404, never 403: a
// permission error would confirm the comment exists, which is exactly what a
// caller holding a stray id must not learn.
func TestGetComment_UnknownIDIs404(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	recorder := getComment(t, "00000000-0000-0000-0000-000000000000")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown comment, got %d: %s",
			recorder.Code, recorder.Body.String())
	}
}

func TestGetComment_MalformedIDIs400(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	recorder := getComment(t, "not-a-uuid")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed comment id, got %d: %s",
			recorder.Code, recorder.Body.String())
	}
}
