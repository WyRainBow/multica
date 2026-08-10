package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pinCommentHTTP drives POST/DELETE /api/comments/{id}/pin and returns the
// status, so a case can assert a rejection as readily as a success.
func pinCommentHTTP(t *testing.T, commentID string, pin bool) int {
	t.Helper()
	w := httptest.NewRecorder()
	method := "POST"
	if !pin {
		method = "DELETE"
	}
	r := newRequest(method, "/api/comments/"+commentID+"/pin", nil)
	r = withURLParam(r, "commentId", commentID)
	if pin {
		testHandler.PinComment(w, r)
	} else {
		testHandler.UnpinComment(w, r)
	}
	return w.Code
}

// Pinning answers "start here" on an issue that has collected more threads than
// anyone re-reads. These pin the three rules that make that answer trustworthy:
// only a root can be it, saying it twice does not move it, and it is
// independent of whether the discussion is over.

func TestPinComment_RootOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "pin root only", "", "")
	rootID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": "the question"})
	replyID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content": "an answer", "parent_id": rootID,
	})

	if status := pinCommentHTTP(t, rootID, true); status != http.StatusOK {
		t.Fatalf("pin root = %d, want 200", status)
	}
	// A reply is not somewhere a reader starts, and a pinned fragment whose
	// question lives elsewhere on the page is worse than no pin at all.
	if status := pinCommentHTTP(t, replyID, true); status != http.StatusBadRequest {
		t.Fatalf("pin reply = %d, want 400", status)
	}

	var pinned bool
	if err := testPool.QueryRow(ctx,
		`SELECT pinned_at IS NOT NULL FROM comment WHERE id = $1`, replyID).Scan(&pinned); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if pinned {
		t.Fatal("the rejected reply was pinned anyway")
	}
}

// Re-pinning must keep a thread's place. Pinned threads sort by pinned_at, so a
// second pin that refreshed the timestamp would silently jump the thread to the
// front of the band — a reorder nobody asked for.
func TestPinComment_IsIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "pin idempotent", "", "")
	rootID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": "root"})

	pinCommentHTTP(t, rootID, true)
	var first string
	if err := testPool.QueryRow(ctx,
		`SELECT pinned_at::text FROM comment WHERE id = $1`, rootID).Scan(&first); err != nil {
		t.Fatalf("read first pinned_at: %v", err)
	}

	if status := pinCommentHTTP(t, rootID, true); status != http.StatusOK {
		t.Fatalf("second pin = %d, want 200", status)
	}
	var second string
	if err := testPool.QueryRow(ctx,
		`SELECT pinned_at::text FROM comment WHERE id = $1`, rootID).Scan(&second); err != nil {
		t.Fatalf("read second pinned_at: %v", err)
	}
	if first != second {
		t.Fatalf("pinned_at moved on a duplicate pin: %s -> %s", first, second)
	}
}

// Pinning and resolving answer different questions — "start here" and "is this
// over" — and a thread is frequently both. Neither may clear the other.
func TestPinComment_IsIndependentOfResolution(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "pin and resolve coexist", "", "")
	rootID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": "root"})

	pinCommentHTTP(t, rootID, true)
	resolveCommentHTTP(t, rootID)

	var pinned, resolved bool
	if err := testPool.QueryRow(ctx,
		`SELECT pinned_at IS NOT NULL, resolved_at IS NOT NULL FROM comment WHERE id = $1`,
		rootID).Scan(&pinned, &resolved); err != nil {
		t.Fatalf("read comment: %v", err)
	}
	if !pinned || !resolved {
		t.Fatalf("pinned=%v resolved=%v — a pinned thread that concludes must stay pinned", pinned, resolved)
	}

	if status := pinCommentHTTP(t, rootID, false); status != http.StatusOK {
		t.Fatalf("unpin = %d, want 200", status)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT pinned_at IS NOT NULL, resolved_at IS NOT NULL FROM comment WHERE id = $1`,
		rootID).Scan(&pinned, &resolved); err != nil {
		t.Fatalf("re-read comment: %v", err)
	}
	if pinned || !resolved {
		t.Fatalf("after unpin: pinned=%v resolved=%v — unpinning must not reopen the discussion", pinned, resolved)
	}
}
