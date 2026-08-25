package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// What `issue create` sends, so the session recorded on a new card can be
// asserted from the request rather than from a comment posted afterwards.
//
// The index comment this replaces was retired in COC-352: it was a second copy
// of facts the card and its progress ledger already hold, and a hook had to
// keep it current. The session was the one thing it carried that nothing else
// did, so that became a field.
type createCapture struct {
	body map[string]any
}

const sessionTestIssueID = "issue-1"

func newCreateCaptureServer(t *testing.T, capture *createCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/me":
			// `issue create` defaults the assignee to the caller.
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "member-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			if err := json.NewDecoder(r.Body).Decode(&capture.body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         sessionTestIssueID,
				"identifier": "TST-1",
				"title":      "a card",
				"status":     "todo",
				"priority":   "none",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// createAndCapture runs `issue create` against the capture server.
//
// sessionEnv is set AFTER the blanket clear, so a case testing the environment
// fallback actually exercises it. An earlier version set it before the clear
// and silently tested the flag instead, which made the env cases pass without
// covering anything.
func createAndCapture(t *testing.T, capture *createCapture, env [2]string, flags map[string]string) error {
	t.Helper()
	srv := newCreateCaptureServer(t, capture)
	defer srv.Close()

	// A real machine has zcode rollout logs under the real home, and the
	// detector would find one and call it this session. Point HOME at an empty
	// directory so "no session anywhere" actually means that.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	// The suite itself may be running inside an agent session, which would
	// otherwise leak that session id into every case here.
	for _, candidate := range sessionEnv {
		t.Setenv(candidate.env, "")
	}
	if env[0] != "" {
		t.Setenv(env[0], env[1])
	}

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "a card")
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	return runIssueCreate(cmd, nil)
}

// TestIssueCreateRecordsTheSession covers where the value comes from when
// nobody types it, which is the whole point: a session id that has to be
// remembered is a session id that goes unrecorded.
func TestIssueCreateRecordsTheSession(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		value string
		flags map[string]string
		want  any
	}{
		{"claude env", "CLAUDE_CODE_SESSION_ID", "sess-claude", nil, "sess-claude"},
		{"codex env", "CODEX_SESSION_ID", "sess-codex", nil, "sess-codex"},
		{
			"explicit flag beats the environment",
			"CLAUDE_CODE_SESSION_ID", "sess-env",
			map[string]string{"session": "sess-flag"},
			"sess-flag",
		},
		// Filed from outside any agent session: the field is left off the
		// request entirely rather than sent as a placeholder, so the card
		// records "no session" instead of a value nobody can resume.
		{"nothing to record", "", "", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			capture := &createCapture{}
			if err := createAndCapture(t, capture, [2]string{c.env, c.value}, c.flags); err != nil {
				t.Fatalf("issue create: %v", err)
			}
			got, present := capture.body["created_by_session"]
			if c.want == nil {
				if present {
					t.Errorf("created_by_session = %#v; an unrecorded session must not be sent at all", got)
				}
				return
			}
			if got != c.want {
				t.Errorf("created_by_session = %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestIssueCreateNoLongerPostsAnIndexComment guards the retirement. The comment
// was posted from here for every card; a stray re-introduction would put the
// duplicate back without anyone noticing, because posting it never failed
// loudly — it was best effort by design.
func TestIssueCreateNoLongerPostsAnIndexComment(t *testing.T) {
	capture := &createCapture{}
	// The capture server errors the test on any request other than /api/me and
	// the create itself, so a comment POST fails this outright.
	if err := createAndCapture(t, capture, [2]string{}, nil); err != nil {
		t.Fatalf("issue create: %v", err)
	}
}

func TestIssueCreateSessionFlagIsRegistered(t *testing.T) {
	cmd := newIssueCreateTestCmd()
	if cmd.Flags().Lookup("session") == nil {
		t.Fatal("--session is not registered on issue create")
	}
}

// TestZcodeSessionFromRollout covers the one runtime that does not say which
// session it is. The id has to come from the log it is writing, and picking the
// wrong file would put another session's id on the card.
func TestZcodeSessionFromRollout(t *testing.T) {
	root := t.TempDir()
	write := func(name string, ageSeconds int) {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-time.Duration(ageSeconds) * time.Second)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}

	if got := zcodeSessionFromRollout(root); got != "" {
		t.Errorf("empty directory returned %q; there is no session to report", got)
	}

	write("model-io-sess_11111111-1111-1111-1111-111111111111.jsonl", 300)
	write("model-io-sess_22222222-2222-2222-2222-222222222222.jsonl", 10)
	// Neither of these is a rollout log, and treating one as a session id would
	// put a value on the card that resumes nothing.
	write("notes.txt", 1)
	write("model-io-sess_short.jsonl", 1)

	want := "sess_22222222-2222-2222-2222-222222222222"
	if got := zcodeSessionFromRollout(root); got != want {
		t.Errorf("zcodeSessionFromRollout = %q, want %q (the most recently written log)", got, want)
	}

	if got := zcodeSessionFromRollout(filepath.Join(root, "missing")); got != "" {
		t.Errorf("missing directory returned %q; zcode is simply not installed here", got)
	}
}
