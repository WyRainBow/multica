package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// gitRepo builds a repository with two branches: main carries one commit, and
// side branches off it with a second. So side's HEAD is not contained in main,
// and main's is contained in side.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "first")
	run("checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "second")
	return dir
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// TestContainmentIsThreeAnswers pins the distinction the merge claim rests on.
// "Not contained" and "could not tell" both come back as a non-zero exit from
// git, and treating them alike is what lets a stale claim survive.
func TestContainmentIsThreeAnswers(t *testing.T) {
	t.Parallel()
	dir := gitRepo(t)
	mainSHA := gitOut(t, dir, "rev-parse", "main")
	sideSHA := gitOut(t, dir, "rev-parse", "side")

	if got := contains(dir, mainSHA, "side"); got != containsYes {
		t.Errorf("main in side = %v, want containsYes", got)
	}
	if got := contains(dir, sideSHA, "main"); got != containsNo {
		t.Errorf("side in main = %v, want containsNo", got)
	}
	if got := contains(dir, sideSHA, "no-such-branch"); got != containsUnknown {
		t.Errorf("side in a missing ref = %v, want containsUnknown", got)
	}
}

// syncServer stands in for the API during a sync: it serves one worktree row
// and records the payload the CLI posts back.
type syncServer struct {
	mu   sync.Mutex
	row  map[string]any
	sent map[string]any
}

func newSyncServer(t *testing.T, row map[string]any) (*syncServer, string) {
	t.Helper()
	s := &syncServer{row: row}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/sync") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.sent = body
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s.row)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/worktrees/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s.row)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *syncServer) payload() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}

func newSyncCmd(t *testing.T, dir, into string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("dir", dir, "")
	f.String("into", into, "")
	f.Bool("force", false, "")
	f.String("output", "json", "")
	f.String("profile", "", "")
	f.String("server-url", "", "")
	return cmd
}

// runSync drives the sync command against a stub API holding the given row.
func runSync(t *testing.T, row map[string]any, dir, into string) map[string]any {
	t.Helper()
	srv, url := newSyncServer(t, row)
	t.Setenv("HOME", t.TempDir())
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: url, Token: "t"}, ""); err != nil {
		t.Fatal(err)
	}
	cmd := newSyncCmd(t, dir, into)
	cmd.SetContext(context.Background())
	if err := runWorktreeSync(cmd, []string{"tree"}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	return srv.payload()
}

// TestSyncRetractsAStaleMergeClaim is the regression. A tree recorded as merged
// keeps that claim until sync says otherwise — and sync only ever said "merged",
// never "no longer". A rebase moves HEAD off the target, and the row went on
// reading merged at a commit the tree no longer carries.
func TestSyncRetractsAStaleMergeClaim(t *testing.T) {
	dir := gitRepo(t)
	old := gitOut(t, dir, "rev-parse", "main")
	row := map[string]any{
		"id": "w1", "name": "tree", "path": dir, "branch": "side",
		"base_ref": "main", "role": "feature", "status": "merged",
		"head_sha": old, "merged_sha": old, "merged_into": "main",
	}

	// HEAD is on side, which main does not contain.
	sent := runSync(t, row, dir, "main")

	if got, ok := sent["merged_sha"].(string); !ok || got != "" {
		t.Errorf("merged_sha = %#v, want an explicit empty string to clear it", sent["merged_sha"])
	}
	if got, ok := sent["merged_into"].(string); !ok || got != "" {
		t.Errorf("merged_into = %#v, want an explicit empty string to clear it", sent["merged_into"])
	}
	if got := sent["status"]; got != "active" {
		t.Errorf("status = %#v, want active — a tree that is not merged is not in the merged state", got)
	}
}

// A tree that really is contained still records the merge, on the commit that
// was measured.
func TestSyncRecordsAMergeItCanSee(t *testing.T) {
	dir := gitRepo(t)
	gitOut(t, dir, "checkout", "-q", "main")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	row := map[string]any{
		"id": "w1", "name": "tree", "path": dir, "branch": "main",
		"base_ref": "side", "role": "feature", "status": "active",
	}

	sent := runSync(t, row, dir, "side")

	if got := sent["merged_sha"]; got != head {
		t.Errorf("merged_sha = %#v, want %s", got, head)
	}
	if got := sent["merged_into"]; got != "side" {
		t.Errorf("merged_into = %#v, want side", got)
	}
	if got := sent["status"]; got != "merged" {
		t.Errorf("status = %#v, want merged", got)
	}
}

// When git cannot answer, the row keeps what it had: an unreachable target is
// not evidence that the tree was never merged.
func TestSyncLeavesTheClaimAloneWhenItCannotCheck(t *testing.T) {
	dir := gitRepo(t)
	old := gitOut(t, dir, "rev-parse", "main")
	row := map[string]any{
		"id": "w1", "name": "tree", "path": dir, "branch": "side",
		"base_ref": "main", "role": "feature", "status": "merged",
		"head_sha": old, "merged_sha": old, "merged_into": "main",
	}

	sent := runSync(t, row, dir, "no-such-branch")

	for _, field := range []string{"merged_sha", "merged_into", "status"} {
		if _, present := sent[field]; present {
			t.Errorf("%s was sent as %#v; an unanswerable check must not write the merge fields", field, sent[field])
		}
	}
}

// TestAutoSessionReadsTheAgentItRunsIn covers both agents and, more to the
// point, the case where there is nothing to read: a pointer invented from the
// newest transcript on disk would name the wrong session exactly when two are
// open, so no answer has to beat a plausible one.
func TestAutoSessionReadsTheAgentItRunsIn(t *testing.T) {
	for _, c := range []struct{ env, agent, want string }{
		{"CLAUDE_CODE_SESSION_ID", "claude", "claude --resume s-1"},
		{"CODEX_SESSION_ID", "codex", "codex resume s-1"},
	} {
		t.Run(c.agent, func(t *testing.T) {
			for _, e := range sessionEnv {
				t.Setenv(e.env, "")
			}
			t.Setenv(c.env, "s-1")

			agent, resume, sessionID, err := currentSession("")
			if err != nil {
				t.Fatalf("currentSession: %v", err)
			}
			if agent != c.agent {
				t.Errorf("agent = %q, want %q", agent, c.agent)
			}
			if resume != c.want {
				t.Errorf("resume = %q, want %q", resume, c.want)
			}
			// The raw id goes on the account too: the sidebar groups by it, and
			// digging it back out of the resume string would be guesswork.
			if sessionID != "s-1" {
				t.Errorf("sessionID = %q, want %q", sessionID, "s-1")
			}

			// With a checkout on the row the pointer takes you there first.
			_, resume, _, err = currentSession("/tmp/tree")
			if err != nil {
				t.Fatalf("currentSession with path: %v", err)
			}
			if want := "cd /tmp/tree && " + c.want; resume != want {
				t.Errorf("resume = %q, want %q", resume, want)
			}
		})
	}

	t.Run("no session", func(t *testing.T) {
		for _, e := range sessionEnv {
			t.Setenv(e.env, "")
		}
		// And no zcode rollout log either — on a real machine the detector
		// would find one under the true home and report it as this session.
		t.Setenv("HOME", t.TempDir())
		if _, _, _, err := currentSession(""); err == nil {
			t.Error("currentSession outside an agent session returned no error; a guessed pointer is worse than none")
		}
	})
}

// TestSyncRefusesToMeasureTheWrongCheckout guards the facts account against the
// easy slip: `sync <name>` names a row, not a directory, so running it from the
// wrong repository would write this checkout's HEAD onto another tree.
func TestSyncRefusesToMeasureTheWrongCheckout(t *testing.T) {
	measured := gitRepo(t)
	recorded := gitRepo(t)
	row := map[string]any{
		"id": "w1", "name": "tree", "path": recorded, "branch": "side",
		"base_ref": "main", "role": "feature", "status": "active",
	}

	srv, url := newSyncServer(t, row)
	t.Setenv("HOME", t.TempDir())
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: url, Token: "t"}, ""); err != nil {
		t.Fatal(err)
	}
	cmd := newSyncCmd(t, measured, "")
	cmd.SetContext(context.Background())

	if err := runWorktreeSync(cmd, []string{"tree"}); err == nil {
		t.Fatal("sync measured a different checkout than the row records and wrote it anyway")
	}
	if srv.payload() != nil {
		t.Errorf("posted %#v; nothing should reach the ledger when the checkout is not the one it names", srv.payload())
	}

	// --force is the deliberate override, and it does write.
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatal(err)
	}
	if err := runWorktreeSync(cmd, []string{"tree"}); err != nil {
		t.Fatalf("sync --force: %v", err)
	}
	if srv.payload() == nil {
		t.Error("--force posted nothing")
	}
}

// A checkout reached through a symlinked path is the same checkout. macOS
// serves /var as a link to /private/var, and refusing there would make --force
// routine, which is how a guard stops being one.
func TestSyncTreatsASymlinkedPathAsTheSamePlace(t *testing.T) {
	real := gitRepo(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	row := map[string]any{
		"id": "w1", "name": "tree", "path": link, "branch": "side",
		"base_ref": "main", "role": "feature", "status": "active",
	}

	if sent := runSync(t, row, real, ""); sent == nil {
		t.Error("sync refused a checkout reached by another name for the same directory")
	}
}
