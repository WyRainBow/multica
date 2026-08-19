package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func newIssueStatusReviewTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "status"}
	cmd.Flags().String("comment-review-file", "", "")
	cmd.Flags().Bool("allow-external-file", false, "")
	return cmd
}

func TestIssueStatusBodyLoadsStructuredCommentReview(t *testing.T) {
	t.Chdir(t.TempDir())
	review := map[string]any{
		"summary": "Reviewed every open thread.",
		"dispositions": []map[string]any{{
			"thread_root_id":   "11111111-1111-4111-8111-111111111111",
			"last_activity_at": "2026-08-19T12:00:00Z",
			"action":           "keep_unresolved",
		}},
	}
	raw, _ := json.Marshal(review)
	if err := os.WriteFile("review.json", raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newIssueStatusReviewTestCmd()
	_ = cmd.Flags().Set("comment-review-file", "review.json")

	body, err := issueStatusBody(cmd, "done")
	if err != nil {
		t.Fatalf("issueStatusBody: %v", err)
	}
	got, ok := body["comment_review"].(map[string]any)
	if !ok || got["summary"] != review["summary"] {
		t.Fatalf("comment_review = %#v", body["comment_review"])
	}
}

func TestIssueStatusBodyRejectsReviewWithoutSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("review.json", []byte(`{"dispositions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newIssueStatusReviewTestCmd()
	_ = cmd.Flags().Set("comment-review-file", "review.json")
	if _, err := issueStatusBody(cmd, "done"); err == nil {
		t.Fatal("missing summary was accepted")
	}
}
