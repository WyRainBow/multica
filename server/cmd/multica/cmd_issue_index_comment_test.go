package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// indexCommentCapture records what `issue create` sent after the issue itself
// landed: the index comment body and the pin call.
type indexCommentCapture struct {
	commentBody map[string]any
	commentHits int
	pinPaths    []string
	// failComment / failPin make the corresponding call return 500.
	failComment bool
	failPin     bool
}

const indexTestIssueID = "issue-1"
const indexTestCommentID = "comment-1"

// newIndexCommentServer serves the three calls `issue create` makes: create the
// issue, post the index, pin it.
func newIndexCommentServer(t *testing.T, capture *indexCommentCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/me":
			// `issue create` defaults the assignee to the caller.
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "member-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         indexTestIssueID,
				"identifier": "TST-1",
				"title":      "indexed card",
				"status":     "todo",
				"priority":   "none",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+indexTestIssueID+"/comments":
			capture.commentHits++
			if err := json.NewDecoder(r.Body).Decode(&capture.commentBody); err != nil {
				t.Errorf("decode index comment body: %v", err)
			}
			if capture.failComment {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": indexTestCommentID})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pin"):
			capture.pinPaths = append(capture.pinPaths, r.URL.Path)
			if capture.failPin {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": indexTestCommentID})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// createWithIndexCapture runs `issue create` against the capture server. Any
// extra flag values are applied before the run.
func createWithIndexCapture(t *testing.T, capture *indexCommentCapture, flags map[string]string) error {
	t.Helper()
	srv := newIndexCommentServer(t, capture)
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	// The suite itself may be running inside an agent session, which would
	// otherwise leak that session id into every golden body here.
	for _, candidate := range sessionEnv {
		t.Setenv(candidate.env, "")
	}

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "indexed card")
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	return runIssueCreate(cmd, nil)
}

func indexContentFrom(t *testing.T, capture *indexCommentCapture) string {
	t.Helper()
	content, _ := capture.commentBody["content"].(string)
	if content == "" {
		t.Fatalf("index comment carried no content: %+v", capture.commentBody)
	}
	return content
}

// TestRunIssueCreatePostsPinnedIndexComment is the main assertion: a card is
// born with its ledger index already posted and pinned, so the rule stops
// depending on whoever created it remembering to do it.
func TestRunIssueCreatePostsPinnedIndexComment(t *testing.T) {
	var capture indexCommentCapture
	if err := createWithIndexCapture(t, &capture, map[string]string{"session": "sess-abc"}); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}

	if capture.commentHits != 1 {
		t.Fatalf("index comment posts = %d, want 1", capture.commentHits)
	}
	// progress_update, not comment. See
	// TestUpdateIssueDoneIgnoresProgressUpdateRootThread in internal/handler
	// for the other half: a `comment` root would give EVERY card in the system
	// a permanent unaddressed review thread and block `done` everywhere.
	if got := capture.commentBody["type"]; got != "progress_update" {
		t.Errorf("index comment type = %#v, want \"progress_update\": a \"comment\" root would block done on every card", got)
	}
	if capture.commentBody["parent_id"] != nil {
		t.Errorf("index must be a thread root, got parent_id = %#v", capture.commentBody["parent_id"])
	}

	want := `> 本卡索引，只列产物落点与当前状态，不含结论。

## 产物落点

- 待补
- 建卡会话（建卡当刻快照，不随会话变动）：` + "`sess-abc`" + `
- 调研：见调研记录阶段评论

## 当前状态

- 刚建卡，尚未开始`
	if got := indexContentFrom(t, &capture); got != want {
		t.Errorf("index content =\n%q\nwant\n%q", got, want)
	}

	if len(capture.pinPaths) != 1 || capture.pinPaths[0] != "/api/comments/"+indexTestCommentID+"/pin" {
		t.Errorf("pin calls = %v, want one POST to /api/comments/%s/pin", capture.pinPaths, indexTestCommentID)
	}
}

// TestRunIssueCreateIndexSessionFallback exercises every way the session line
// gets its value, in priority order. The environment variables are the same
// ones `worktree session --auto` reads — an agent runtime exports them to the
// commands it spawns, so a command run from a session reads its own id.
func TestRunIssueCreateIndexSessionFallback(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		flags map[string]string
		want  string
	}{
		{
			name: "claude env",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": "claude-session"},
			want: "`claude-session`",
		},
		{
			name: "codex env",
			env:  map[string]string{"CODEX_SESSION_ID": "codex-session"},
			want: "`codex-session`",
		},
		{
			name:  "explicit flag beats the environment",
			env:   map[string]string{"CLAUDE_CODE_SESSION_ID": "claude-session"},
			flags: map[string]string{"session": "typed-by-hand"},
			want:  "`typed-by-hand`",
		},
		{
			// Neither available: the card is still created and the line says
			// so rather than the create failing or the line going missing.
			name: "nothing to record",
			want: "未记录",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capture indexCommentCapture
			srv := newIndexCommentServer(t, &capture)
			defer srv.Close()

			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "mat_test-token")
			for _, candidate := range sessionEnv {
				t.Setenv(candidate.env, tc.env[candidate.env])
			}

			cmd := newIssueCreateTestCmd()
			_ = cmd.Flags().Set("title", "indexed card")
			for name, value := range tc.flags {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatalf("set --%s: %v", name, err)
				}
			}
			if err := runIssueCreate(cmd, nil); err != nil {
				t.Fatalf("runIssueCreate: %v", err)
			}

			content := indexContentFrom(t, &capture)
			wantLine := "- 建卡会话（建卡当刻快照，不随会话变动）：" + tc.want
			if !strings.Contains(content, wantLine) {
				t.Errorf("index content missing %q, got:\n%s", wantLine, content)
			}
		})
	}
}

// TestRunIssueCreateSurvivesIndexCommentFailure and its pin sibling pin the
// best-effort contract: the issue already exists by the time these calls run,
// so a non-zero exit would invite the caller to retry `issue create` and end up
// with a duplicate card. Warn and carry on.
func TestRunIssueCreateSurvivesIndexCommentFailure(t *testing.T) {
	capture := indexCommentCapture{failComment: true}
	if err := createWithIndexCapture(t, &capture, nil); err != nil {
		t.Fatalf("a failed index comment must not fail the create, got: %v", err)
	}
	if len(capture.pinPaths) != 0 {
		t.Errorf("nothing was created to pin, yet pin was called: %v", capture.pinPaths)
	}
}

func TestRunIssueCreateSurvivesIndexPinFailure(t *testing.T) {
	capture := indexCommentCapture{failPin: true}
	if err := createWithIndexCapture(t, &capture, nil); err != nil {
		t.Fatalf("a failed pin must not fail the create, got: %v", err)
	}
	if len(capture.pinPaths) != 1 {
		t.Errorf("pin calls = %v, want exactly one attempt", capture.pinPaths)
	}
}

// TestIssueCreateSessionFlagIsRegistered guards the real command's flag set,
// which the test helper above deliberately does not share.
func TestIssueCreateSessionFlagIsRegistered(t *testing.T) {
	var cmd *cobra.Command = issueCreateCmd
	if cmd.Flags().Lookup("session") == nil {
		t.Fatal("issue create is missing the --session flag")
	}
}
