package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"add", "."},
		{"commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestCollectDeliverySnapshotCleanRepo(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	snap := collectDeliverySnapshot(dir)
	if snap == nil {
		t.Fatal("expected snapshot for a git worktree")
	}
	if snap.Dirty {
		t.Errorf("clean repo reported dirty: %+v", snap)
	}
	if snap.Branch != "master" && snap.Branch != "main" {
		t.Errorf("unexpected branch %q", snap.Branch)
	}
	if len(snap.HeadSHA) < 7 {
		t.Errorf("missing head sha: %q", snap.HeadSHA)
	}
	if len(snap.ChangedFiles) != 0 {
		t.Errorf("unexpected changed files: %v", snap.ChangedFiles)
	}
}

func TestCollectDeliverySnapshotDirtyRepo(t *testing.T) {
	dir := t.TempDir()
	// A tracked file modified after the commit (shows in both status and
	// diffstat) plus an untracked one (status only — diffstat never includes
	// untracked files, and the snapshot must not `git add -N` to fake it).
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, dir)
	if err := os.WriteFile(tracked, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := collectDeliverySnapshot(dir)
	if snap == nil {
		t.Fatal("expected snapshot for a git worktree")
	}
	if !snap.Dirty {
		t.Error("dirty repo not flagged")
	}
	joined := strings.Join(snap.ChangedFiles, "\n")
	if !strings.Contains(joined, "tracked.txt") || !strings.Contains(joined, "untracked.txt") {
		t.Errorf("changed files incomplete: %v", snap.ChangedFiles)
	}
	if !strings.Contains(snap.DiffStat, "tracked.txt") {
		t.Errorf("tracked.txt missing from diffstat: %q", snap.DiffStat)
	}
	if strings.Contains(snap.DiffStat, "untracked.txt") {
		t.Errorf("untracked file should not be in diffstat: %q", snap.DiffStat)
	}
}

func TestCollectDeliverySnapshotNonGit(t *testing.T) {
	dir := t.TempDir()
	if snap := collectDeliverySnapshot(dir); snap != nil {
		t.Errorf("non-git dir should yield nil, got %+v", snap)
	}
	if snap := collectDeliverySnapshot(""); snap != nil {
		t.Errorf("empty workdir should yield nil, got %+v", snap)
	}
}
