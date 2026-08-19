package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func issueStatusBody(cmd *cobra.Command, status string) (map[string]any, error) {
	body := map[string]any{"status": status}
	path, _ := cmd.Flags().GetString("comment-review-file")
	path = strings.TrimSpace(path)
	if path == "" {
		return body, nil
	}
	if status != "done" {
		return nil, fmt.Errorf("--comment-review-file is only valid when status is done")
	}
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); !allow {
		within, err := fileWithinWorkingDir(path)
		if err != nil {
			return nil, fmt.Errorf("resolve --comment-review-file path: %w", err)
		}
		if !within {
			return nil, fmt.Errorf("--comment-review-file %q resolves outside the current working directory; pass --allow-external-file to override", path)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --comment-review-file: %w", err)
	}
	var review map[string]any
	if err := json.Unmarshal(raw, &review); err != nil {
		return nil, fmt.Errorf("parse --comment-review-file as JSON: %w", err)
	}
	summary, _ := review["summary"].(string)
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("--comment-review-file requires a non-empty summary")
	}
	dispositions, ok := review["dispositions"].([]any)
	if !ok || len(dispositions) == 0 {
		return nil, fmt.Errorf("--comment-review-file requires at least one disposition")
	}
	body["comment_review"] = review
	return body, nil
}
