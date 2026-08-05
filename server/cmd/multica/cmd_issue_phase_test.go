package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Tests for `multica issue phase`. The behavior worth pinning is the
// name→UUID resolution — the API only speaks UUIDs while every caller holds a
// name — and the two guards that stand between a typo and deleted comments.

const phaseIDReview = "22222222-2222-2222-2222-222222222222"
const phaseIDReview2 = "33333333-3333-3333-3333-333333333333"
const phaseIDStart = "44444444-4444-4444-4444-444444444444"

// phaseTestServer answers resolveIssueRef and the phase list, and forwards
// everything else under /phases to the supplied handler. Captured paths let a
// test assert which phase UUID the CLI actually addressed.
func phaseTestServer(t *testing.T, phases []map[string]any, handler http.HandlerFunc) *[]string {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         testIssueUUID,
				"identifier": "MUL-1",
				"title":      "test issue",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID+"/phases":
			_ = json.NewEncoder(w).Encode(map[string]any{"phases": phases})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"+testIssueUUID+"/phases"):
			if handler != nil {
				handler(w, r)
				return
			}
			// Mirror the real server: DELETE is 204, every other phase write
			// answers 200 with the updated phase.
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": phaseIDReview, "name": "评审"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return &paths
}

func phase(id, name string, commentCount int64) map[string]any {
	return map[string]any{
		"id":            id,
		"name":          name,
		"entered_at":    nil,
		"completed_at":  nil,
		"comment_count": commentCount,
	}
}

func newPhaseTestCmd(use string) *cobra.Command {
	c := &cobra.Command{Use: use}
	c.Flags().String("output", "json", "")
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("full-id", false, "")
	c.Flags().Int32("position", 0, "")
	return c
}

// A route grows rounds: adding 评审 2 must not make the original 评审
// unreachable. Exact match has to win over prefix match for that to hold.
func TestResolveIssuePhasePrefersExactNameOverPrefix(t *testing.T) {
	paths := phaseTestServer(t, []map[string]any{
		phase(phaseIDReview, "评审", 0),
		phase(phaseIDReview2, "评审 2", 0),
	}, nil)

	cmd := newPhaseTestCmd("enter")
	if _, err := captureStdout(t, func() error {
		return runIssuePhaseEnter(cmd, []string{testIssueUUID, "评审"})
	}); err != nil {
		t.Fatalf("enter phase: %v", err)
	}

	want := "POST /api/issues/" + testIssueUUID + "/phases/" + phaseIDReview + "/enter"
	if !containsPath(*paths, want) {
		t.Fatalf("addressed the wrong phase; paths = %v, want %q", *paths, want)
	}
}

func TestResolveIssuePhaseAcceptsUniquePrefix(t *testing.T) {
	paths := phaseTestServer(t, []map[string]any{
		phase(phaseIDStart, "开始", 0),
		phase(phaseIDReview, "评审", 0),
	}, nil)

	cmd := newPhaseTestCmd("complete")
	if _, err := captureStdout(t, func() error {
		return runIssuePhaseComplete(cmd, []string{testIssueUUID, "开"})
	}); err != nil {
		t.Fatalf("complete phase: %v", err)
	}

	want := "POST /api/issues/" + testIssueUUID + "/phases/" + phaseIDStart + "/complete"
	if !containsPath(*paths, want) {
		t.Fatalf("addressed the wrong phase; paths = %v, want %q", *paths, want)
	}
}

// An ambiguous prefix must not silently pick one. Guessing here would file a
// comment under the wrong round, which reads as correct until someone audits.
func TestResolveIssuePhaseRejectsAmbiguousPrefix(t *testing.T) {
	phaseTestServer(t, []map[string]any{
		phase(phaseIDReview, "评审", 0),
		phase(phaseIDReview2, "评审 2", 0),
	}, nil)

	cmd := newPhaseTestCmd("enter")
	_, err := captureStdout(t, func() error {
		return runIssuePhaseEnter(cmd, []string{testIssueUUID, "评"})
	})
	if err == nil {
		t.Fatal("ambiguous prefix was accepted, want an error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v, want it to name the ambiguity", err)
	}
}

// The name the user typed is not on the issue: the error has to list what IS
// there, or the next attempt is another guess.
func TestResolveIssuePhaseUnknownNameListsAvailable(t *testing.T) {
	phaseTestServer(t, []map[string]any{
		phase(phaseIDStart, "开始", 0),
		phase(phaseIDReview, "评审", 0),
	}, nil)

	cmd := newPhaseTestCmd("enter")
	_, err := captureStdout(t, func() error {
		return runIssuePhaseEnter(cmd, []string{testIssueUUID, "冻结"})
	})
	if err == nil {
		t.Fatal("unknown phase name was accepted, want an error")
	}
	if !strings.Contains(err.Error(), "开始") || !strings.Contains(err.Error(), "评审") {
		t.Fatalf("error = %v, want it to list the available phase names", err)
	}
}

// Deleting a phase deletes its comments server-side. The count is only
// visible before the call, so the guard has to fire without one request going
// out — a warning printed after the DELETE would be an epitaph.
func TestIssuePhaseDeleteRefusesToDropCommentsWithoutForce(t *testing.T) {
	paths := phaseTestServer(t, []map[string]any{
		phase(phaseIDReview, "评审", 3),
	}, nil)

	cmd := newPhaseTestCmd("delete")
	_, err := captureStdout(t, func() error {
		return runIssuePhaseDelete(cmd, []string{testIssueUUID, "评审"})
	})
	if err == nil {
		t.Fatal("delete dropped 3 comments without --force")
	}
	if !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("error = %v, want the comment count and the --force hint", err)
	}
	for _, p := range *paths {
		if strings.HasPrefix(p, "DELETE ") {
			t.Fatalf("a DELETE was sent despite the guard; paths = %v", *paths)
		}
	}
}

func TestIssuePhaseDeleteProceedsWithForce(t *testing.T) {
	paths := phaseTestServer(t, []map[string]any{
		phase(phaseIDReview, "评审", 3),
	}, nil)

	cmd := newPhaseTestCmd("delete")
	_ = cmd.Flags().Set("force", "true")
	if _, err := captureStdout(t, func() error {
		return runIssuePhaseDelete(cmd, []string{testIssueUUID, "评审"})
	}); err != nil {
		t.Fatalf("delete with --force: %v", err)
	}

	want := "DELETE /api/issues/" + testIssueUUID + "/phases/" + phaseIDReview
	if !containsPath(*paths, want) {
		t.Fatalf("paths = %v, want %q", *paths, want)
	}
}

// An empty phase carries nothing to lose, so demanding --force there would be
// friction with no reader behind it.
func TestIssuePhaseDeleteNeedsNoForceWhenEmpty(t *testing.T) {
	paths := phaseTestServer(t, []map[string]any{
		phase(phaseIDReview, "评审", 0),
	}, nil)

	cmd := newPhaseTestCmd("delete")
	if _, err := captureStdout(t, func() error {
		return runIssuePhaseDelete(cmd, []string{testIssueUUID, "评审"})
	}); err != nil {
		t.Fatalf("delete empty phase: %v", err)
	}

	want := "DELETE /api/issues/" + testIssueUUID + "/phases/" + phaseIDReview
	if !containsPath(*paths, want) {
		t.Fatalf("paths = %v, want %q", *paths, want)
	}
}

func TestPhaseStateDerivesFromTimestamps(t *testing.T) {
	cases := []struct {
		name  string
		phase issuePhase
		want  string
	}{
		{"never entered", issuePhase{}, "pending"},
		{"entered", issuePhase{EnteredAt: "2026-08-05T01:00:00Z"}, "current"},
		{
			"completed",
			issuePhase{EnteredAt: "2026-08-05T01:00:00Z", CompletedAt: "2026-08-05T02:00:00Z"},
			"done",
		},
		// Bad data still has to render something rather than break the row.
		{"completed without entering", issuePhase{CompletedAt: "2026-08-05T02:00:00Z"}, "done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := phaseState(tc.phase); got != tc.want {
				t.Fatalf("phaseState = %q, want %q", got, tc.want)
			}
		})
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
