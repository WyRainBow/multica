package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type doneReviewRequiredResponse struct {
	Code   string `json:"code"`
	Issues []struct {
		IssueID string `json:"issue_id"`
		Threads []struct {
			ThreadRootID   string `json:"thread_root_id"`
			LastActivityAt string `json:"last_activity_at"`
		} `json:"threads"`
	} `json:"issues"`
}

func seedDoneReviewComment(t *testing.T, issueID, commentType string, parentID *string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type, parent_id)
		VALUES ($1, $2, 'member', $3, 'review me', $4, $5)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID, commentType, parentID).Scan(&id)
	if err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	return id
}

func updateIssueForDoneReview(t *testing.T, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest("PUT", "/api/issues/"+issueID, body)
	r = withURLParam(r, "id", issueID)
	testHandler.UpdateIssue(w, r)
	return w
}

func decodeDoneReviewRequired(t *testing.T, w *httptest.ResponseRecorder) doneReviewRequiredResponse {
	t.Helper()
	var response doneReviewRequiredResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode done-review response: %v (%s)", err, w.Body.String())
	}
	return response
}

func TestUpdateIssueDoneRequiresDispositionForEveryUnresolvedCommentThread(t *testing.T) {
	issueID := createTestIssue(t, "done review gate", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	rootID := seedDoneReviewComment(t, issueID, "comment", nil)

	w := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	response := decodeDoneReviewRequired(t, w)
	if response.Code != "comment_review_required" || len(response.Issues) != 1 || len(response.Issues[0].Threads) != 1 {
		t.Fatalf("unexpected blocker response: %+v", response)
	}
	if response.Issues[0].Threads[0].ThreadRootID != rootID {
		t.Fatalf("thread root = %s, want %s", response.Issues[0].Threads[0].ThreadRootID, rootID)
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "todo" {
		t.Fatalf("blocked update changed status to %q", status)
	}
}

func TestUpdateIssueDoneAcceptsExplicitKeepAndInvalidatesItAfterReply(t *testing.T) {
	issueID := createTestIssue(t, "done review keep snapshot", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	rootID := seedDoneReviewComment(t, issueID, "comment", nil)

	blocked := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	response := decodeDoneReviewRequired(t, blocked)
	snapshot := response.Issues[0].Threads[0].LastActivityAt

	w := updateIssueForDoneReview(t, issueID, map[string]any{
		"status": "done",
		"comment_review": map[string]any{
			"summary": "Reviewed the open discussion and intentionally kept it open.",
			"dispositions": []map[string]any{{
				"thread_root_id":   rootID,
				"last_activity_at": snapshot,
				"action":           "keep_unresolved",
			}},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("reviewed done status = %d, want 200: %s", w.Code, w.Body.String())
	}

	// An unchanged keep receipt survives a reopen, so long-lived pinned/ledger
	// threads do not demand the same decision again.
	if reopened := updateIssueForDoneReview(t, issueID, map[string]any{"status": "todo"}); reopened.Code != http.StatusOK {
		t.Fatalf("reopen status = %d: %s", reopened.Code, reopened.Body.String())
	}
	if redone := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"}); redone.Code != http.StatusOK {
		t.Fatalf("unchanged reviewed thread blocked again: %d %s", redone.Code, redone.Body.String())
	}

	if reopened := updateIssueForDoneReview(t, issueID, map[string]any{"status": "todo"}); reopened.Code != http.StatusOK {
		t.Fatalf("second reopen status = %d: %s", reopened.Code, reopened.Body.String())
	}
	time.Sleep(time.Millisecond)
	seedDoneReviewComment(t, issueID, "comment", &rootID)
	if stale := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"}); stale.Code != http.StatusConflict {
		t.Fatalf("new reply did not invalidate keep receipt: %d %s", stale.Code, stale.Body.String())
	}
}

func TestUpdateIssueDoneIgnoresNonCommentRootsAndAlreadyResolvedThreads(t *testing.T) {
	issueID := createTestIssue(t, "done review exclusions", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	seedDoneReviewComment(t, issueID, "system", nil)
	rootID := seedDoneReviewComment(t, issueID, "comment", nil)
	resolveCommentHTTP(t, rootID)

	w := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if w.Code != http.StatusOK {
		t.Fatalf("non-comment/resolved roots blocked done: %d %s", w.Code, w.Body.String())
	}
}

func TestUpdateIssueDoneResolvesTheChosenConclusionAndWritesOneAggregateActivity(t *testing.T) {
	issueID := createTestIssue(t, "done review conclusion", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	rootID := seedDoneReviewComment(t, issueID, "comment", nil)
	replyID := seedDoneReviewComment(t, issueID, "comment", &rootID)

	blocked := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	response := decodeDoneReviewRequired(t, blocked)
	snapshot := response.Issues[0].Threads[0].LastActivityAt
	w := updateIssueForDoneReview(t, issueID, map[string]any{
		"status": "done",
		"comment_review": map[string]any{
			"summary": "The reply contains the agreed conclusion.",
			"dispositions": []map[string]any{{
				"thread_root_id":        rootID,
				"last_activity_at":      snapshot,
				"action":                "resolve",
				"resolution_comment_id": replyID,
			}},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("resolve-and-done status = %d: %s", w.Code, w.Body.String())
	}
	if !commentResolved(t, replyID) || commentResolved(t, rootID) {
		t.Fatalf("chosen reply was not the sole resolution")
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM activity_log
		WHERE issue_id = $1 AND action = 'comments_reviewed_before_done'
	`, issueID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("aggregate review activities = %d, want 1", count)
	}
}

func TestUpdateIssueDoneAllowsMixedResolveAndKeep(t *testing.T) {
	issueID := createTestIssue(t, "done review mixed dispositions", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	resolveRootID := seedDoneReviewComment(t, issueID, "comment", nil)
	resolutionID := seedDoneReviewComment(t, issueID, "comment", &resolveRootID)
	keepRootID := seedDoneReviewComment(t, issueID, "comment", nil)

	blocked := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	response := decodeDoneReviewRequired(t, blocked)
	snapshots := make(map[string]string, len(response.Issues[0].Threads))
	for _, thread := range response.Issues[0].Threads {
		snapshots[thread.ThreadRootID] = thread.LastActivityAt
	}

	w := updateIssueForDoneReview(t, issueID, map[string]any{
		"status": "done",
		"comment_review": map[string]any{
			"summary": "Resolved the actionable conclusion and kept the remaining discussion open.",
			"dispositions": []map[string]any{
				{
					"thread_root_id":        resolveRootID,
					"last_activity_at":      snapshots[resolveRootID],
					"action":                "resolve",
					"resolution_comment_id": resolutionID,
				},
				{
					"thread_root_id":   keepRootID,
					"last_activity_at": snapshots[keepRootID],
					"action":           "keep_unresolved",
				},
			},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("mixed review done status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !commentResolved(t, resolutionID) || commentResolved(t, keepRootID) {
		t.Fatalf("mixed review did not preserve resolve/keep decisions")
	}
}

func TestUpdateIssueDoneRejectsDuplicateAndUnknownCurrentIssueDispositions(t *testing.T) {
	issueID := createTestIssue(t, "done review disposition validation", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	rootID := seedDoneReviewComment(t, issueID, "comment", nil)
	blocked := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	response := decodeDoneReviewRequired(t, blocked)
	snapshot := response.Issues[0].Threads[0].LastActivityAt

	duplicate := updateIssueForDoneReview(t, issueID, map[string]any{
		"status": "done",
		"comment_review": map[string]any{
			"summary": "The thread was reviewed twice by mistake.",
			"dispositions": []map[string]any{
				{"thread_root_id": rootID, "last_activity_at": snapshot, "action": "keep_unresolved"},
				{"thread_root_id": rootID, "last_activity_at": snapshot, "action": "keep_unresolved"},
			},
		},
	})
	if duplicate.Code != http.StatusBadRequest {
		t.Fatalf("duplicate disposition status = %d, want 400: %s", duplicate.Code, duplicate.Body.String())
	}

	unknown := updateIssueForDoneReview(t, issueID, map[string]any{
		"status": "done",
		"comment_review": map[string]any{
			"summary": "The referenced thread does not exist.",
			"dispositions": []map[string]any{{
				"issue_id":         issueID,
				"thread_root_id":   "00000000-0000-0000-0000-000000000000",
				"last_activity_at": snapshot,
				"action":           "keep_unresolved",
			}},
		},
	})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown current-issue disposition status = %d, want 400: %s", unknown.Code, unknown.Body.String())
	}
}

func TestBatchUpdateDoneAcceptsExplicitDispositionsForOtherIssues(t *testing.T) {
	first := createTestIssue(t, "batch review disposition first", "todo", "none")
	second := createTestIssue(t, "batch review disposition second", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, first); deleteTestIssue(t, second) })
	firstRoot := seedDoneReviewComment(t, first, "comment", nil)
	secondRoot := seedDoneReviewComment(t, second, "comment", nil)

	initial := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(initial, newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{first, second},
		"updates":   map[string]any{"status": "done"},
	}))
	if initial.Code != http.StatusConflict {
		t.Fatalf("initial batch status = %d, want 409: %s", initial.Code, initial.Body.String())
	}
	var response doneReviewRequiredResponse
	if err := json.Unmarshal(initial.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	snapshots := make(map[string]string, 2)
	for _, issue := range response.Issues {
		for _, thread := range issue.Threads {
			snapshots[issue.IssueID+":"+thread.ThreadRootID] = thread.LastActivityAt
		}
	}

	retry := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(retry, newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{first, second},
		"updates": map[string]any{
			"status": "done",
			"comment_review": map[string]any{
				"summary": "Reviewed both issue discussions.",
				"dispositions": []map[string]any{
					{"issue_id": first, "thread_root_id": firstRoot, "last_activity_at": snapshots[first+":"+firstRoot], "action": "keep_unresolved"},
					{"issue_id": second, "thread_root_id": secondRoot, "last_activity_at": snapshots[second+":"+secondRoot], "action": "keep_unresolved"},
				},
			},
		},
	}))
	if retry.Code != http.StatusOK {
		t.Fatalf("explicit multi-issue batch status = %d, want 200: %s", retry.Code, retry.Body.String())
	}
}

func TestDoneCommentReviewRecheckSeesReplyAddedAfterFirstEvaluation(t *testing.T) {
	issueID := createTestIssue(t, "done review final snapshot", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	rootID := seedDoneReviewComment(t, issueID, "comment", nil)
	resolutionID := seedDoneReviewComment(t, issueID, "comment", &rootID)
	prefix := testHandler.getIssuePrefix(context.Background(), parseUUID(testWorkspaceID))
	initial := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	response := decodeDoneReviewRequired(t, initial)
	review := &DoneCommentReviewRequest{
		Summary: "Resolved the conclusion while a new reply races the final check.",
		Dispositions: []DoneCommentDisposition{{
			ThreadRootID: rootID, LastActivityAt: response.Issues[0].Threads[0].LastActivityAt,
			Action: "resolve", ResolutionCommentID: resolutionID,
		}},
	}
	issue, err := testHandler.Queries.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := testHandler.evaluateDoneCommentReview(context.Background(), issue, issueToResponse(issue, prefix).Identifier, review)
	if err != nil || first.Blocker != nil {
		t.Fatalf("initial review evaluation failed: %+v %v", first, err)
	}

	// Make the race observable through the real write path: resolving the
	// chosen conclusion inserts a fresh reply before UpdateIssue's final
	// recheck runs. This is scoped to the generated comment id and is removed
	// before the test exits, so it cannot affect neighboring tests.
	ctx := context.Background()
	_, err = testPool.Exec(ctx, fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION test_done_review_reply_after_resolve() RETURNS trigger
		LANGUAGE plpgsql AS $fn$
		BEGIN
			IF NEW.id = '%s'::uuid AND OLD.resolved_at IS NULL AND NEW.resolved_at IS NOT NULL THEN
				INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type, parent_id, created_at)
				VALUES (NEW.workspace_id, NEW.issue_id, 'member', '%s'::uuid,
				        'reply inserted during done review', 'comment', '%s'::uuid, clock_timestamp());
			END IF;
			RETURN NEW;
		END;
		$fn$;
		DROP TRIGGER IF EXISTS test_done_review_reply_after_resolve_trigger ON comment;
		CREATE TRIGGER test_done_review_reply_after_resolve_trigger
		AFTER UPDATE OF resolved_at ON comment
		FOR EACH ROW EXECUTE FUNCTION test_done_review_reply_after_resolve();
	`, resolutionID, testUserID, rootID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DROP TRIGGER IF EXISTS test_done_review_reply_after_resolve_trigger ON comment;
			DROP FUNCTION IF EXISTS test_done_review_reply_after_resolve();
		`)
	})

	retried := updateIssueForDoneReview(t, issueID, map[string]any{
		"status": "done",
		"comment_review": map[string]any{
			"summary": review.Summary,
			"dispositions": []map[string]any{{
				"thread_root_id": rootID, "last_activity_at": review.Dispositions[0].LastActivityAt,
				"action": "resolve", "resolution_comment_id": resolutionID,
			}},
		},
	})
	if retried.Code != http.StatusConflict {
		t.Fatalf("reply racing final check status = %d, want 409: %s", retried.Code, retried.Body.String())
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "todo" {
		t.Fatalf("reply racing final check changed status to %q", status)
	}
}

func TestBatchUpdateDoneBlocksEveryIssueBeforeChangingAny(t *testing.T) {
	first := createTestIssue(t, "batch done review first", "todo", "none")
	second := createTestIssue(t, "batch done review second", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, first); deleteTestIssue(t, second) })
	seedDoneReviewComment(t, second, "comment", nil)

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{first, second},
		"updates":   map[string]any{"status": "done"},
	})
	testHandler.BatchUpdateIssues(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("batch status = %d, want 409: %s", w.Code, w.Body.String())
	}
	for _, issueID := range []string{first, second} {
		var status string
		if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "todo" {
			t.Fatalf("issue %s changed despite batch blocker: %s", issueID, status)
		}
	}
}
