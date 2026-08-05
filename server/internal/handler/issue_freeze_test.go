package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// seedFreezeIssue creates one issue in the given status.
func seedFreezeIssue(t *testing.T, status string) string {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("issue-freeze-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"issue_freeze_test":%q}`, token)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
	})

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, description, status, priority,
			creator_type, creator_id, position, number, metadata
		) VALUES ($1, $2, 'original body', $3, 'none', 'member', $4, 0, $5, $6::jsonb)
		RETURNING id
	`, testWorkspaceID, token+" issue", status, testUserID, number, metadata).Scan(&id); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return id
}

func putIssue(t *testing.T, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+id, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.UpdateIssue(recorder, req)
	return recorder
}

func issueBody(t *testing.T, id string) (string, string) {
	t.Helper()
	var title, description string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, description FROM issue WHERE id = $1`, id).
		Scan(&title, &description); err != nil {
		t.Fatalf("read issue body: %v", err)
	}
	return title, description
}

// The point of the feature: a finished issue records what was true when it
// finished, and nothing marks a later edit — the description is one current
// value, not a history.
func TestUpdateIssue_FreezesTheBodyOfATerminalIssue(t *testing.T) {
	for _, status := range []string{"done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			id := seedFreezeIssue(t, status)

			recorder := putIssue(t, id, map[string]any{"description": "rewritten"})
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
			}

			_, description := issueBody(t, id)
			if description != "original body" {
				t.Fatalf("description = %q, want it untouched", description)
			}
		})
	}
}

func TestUpdateIssue_FreezesTheTitleToo(t *testing.T) {
	id := seedFreezeIssue(t, "done")

	if recorder := putIssue(t, id, map[string]any{"title": "renamed"}); recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

// The refusal has to say how to get out of it, or the caller — a person or an
// agent — is left with a 409 and no next move.
func TestUpdateIssue_FreezeErrorExplainsHowToUnlock(t *testing.T) {
	id := seedFreezeIssue(t, "done")

	recorder := putIssue(t, id, map[string]any{"description": "rewritten"})
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error == "" {
		t.Fatalf("no error message")
	}
	for _, want := range []string{"frozen", "terminal status", "new issue"} {
		if !containsFold(body.Error, want) {
			t.Fatalf("error %q does not mention %q", body.Error, want)
		}
	}
}

// This is the unlock. It must pass, or a terminal issue is locked forever and
// one mis-click on "done" costs the record permanently.
func TestUpdateIssue_AllowsMovingOutOfATerminalStatus(t *testing.T) {
	id := seedFreezeIssue(t, "done")

	if recorder := putIssue(t, id, map[string]any{"status": "in_progress"}); recorder.Code != http.StatusOK {
		t.Fatalf("reopen status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	// And the body is writable again from the next request on.
	if recorder := putIssue(t, id, map[string]any{"description": "corrected"}); recorder.Code != http.StatusOK {
		t.Fatalf("edit after reopen = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if _, description := issueBody(t, id); description != "corrected" {
		t.Fatalf("description = %q, want %q", description, "corrected")
	}
}

// Reopening and rewriting in one request is still refused: at the moment it
// arrives the issue is finished, and judging on the requested status instead
// would let any write bypass the freeze by attaching a status field.
func TestUpdateIssue_RefusesReopenAndRewriteInOneRequest(t *testing.T) {
	id := seedFreezeIssue(t, "done")

	recorder := putIssue(t, id, map[string]any{
		"status":      "in_progress",
		"description": "rewritten",
	})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	// Neither half landed.
	if _, description := issueBody(t, id); description != "original body" {
		t.Fatalf("description = %q, want it untouched", description)
	}
}

// The freeze is narrow on purpose: everything that legitimately happens to a
// finished issue must keep happening.
func TestUpdateIssue_TerminalIssueAcceptsNonBodyChanges(t *testing.T) {
	id := seedFreezeIssue(t, "done")

	for _, field := range []map[string]any{
		{"priority": "high"},
		{"status": "cancelled"},
	} {
		if recorder := putIssue(t, id, field); recorder.Code != http.StatusOK {
			t.Fatalf("update %v = %d, want 200: %s", field, recorder.Code, recorder.Body.String())
		}
	}
}

// Control: an issue still in flight is unaffected.
func TestUpdateIssue_LeavesAnUnfinishedIssueWritable(t *testing.T) {
	id := seedFreezeIssue(t, "in_progress")

	if recorder := putIssue(t, id, map[string]any{"description": "edited"}); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if _, description := issueBody(t, id); description != "edited" {
		t.Fatalf("description = %q, want %q", description, "edited")
	}
}

func containsFold(haystack, needle string) bool {
	return len(needle) == 0 ||
		(len(haystack) >= len(needle) &&
			indexFold(haystack, needle) >= 0)
}

func indexFold(haystack, needle string) int {
	lower := func(r byte) byte {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if lower(haystack[i+j]) != lower(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
