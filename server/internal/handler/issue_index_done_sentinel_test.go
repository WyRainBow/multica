package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateIssueDoneIgnoresProgressUpdateRootThread is the sentinel for the
// automatic ledger index `multica issue create` now posts on every new card.
//
// Two properties have to hold together, and this test is the only place both
// are checked against the real API rather than against a hand-seeded row:
//
//  1. `progress_update` is a type a client is actually allowed to author. The
//     obvious alternative, `system`, is not — POST /comments rejects it — so an
//     index posted as `system` could never be written from the CLI at all.
//  2. A `progress_update` thread root does not count as an unresolved review
//     thread. The done gate selects roots with type='comment'; if the index
//     were posted as `comment`, every card in the system would carry a
//     permanent unaddressed thread and `done` would demand a disposition
//     everywhere, forever.
//
// Break either one and this test fails. Its counterpart on the writing side is
// TestRunIssueCreatePostsPinnedIndexComment in cmd/multica, which pins the type
// the CLI actually sends.
func TestUpdateIssueDoneIgnoresProgressUpdateRootThread(t *testing.T) {
	issueID := createTestIssue(t, "progress_update root must not gate done", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "> 本卡索引，只列产物落点与当前状态，不含结论。",
		"type":    "progress_update",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST progress_update comment = %d, want 201 (the CLI index depends on this type being client-authorable): %s", w.Code, w.Body.String())
	}
	var posted CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&posted); err != nil {
		t.Fatalf("decode posted comment: %v", err)
	}
	if posted.Type != "progress_update" {
		t.Fatalf("stored comment type = %q, want progress_update", posted.Type)
	}
	if posted.ParentID != nil {
		t.Fatalf("index must be a thread root, got parent_id = %v", *posted.ParentID)
	}

	done := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if done.Code != http.StatusOK {
		t.Fatalf("a progress_update root blocked done with %d — the automatic index now gates every card: %s",
			done.Code, done.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("issue status = %q, want done", status)
	}
}
