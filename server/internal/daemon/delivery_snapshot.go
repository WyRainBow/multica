package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// DeliverySnapshot is the per-run worktree state captured at task completion
// (COC-285): branch, head, upstream, dirty flag, changed files and a diffstat.
// It rides the completion request and persists inside the task result JSONB,
// so the run ledger answers "what did this run touch" even after the isolated
// worktree is GC'd. All fields are best-effort; a non-git workdir yields nil.
type DeliverySnapshot struct {
	Branch       string   `json:"branch,omitempty"`
	HeadSHA      string   `json:"head_sha,omitempty"`
	Upstream     string   `json:"upstream,omitempty"`
	Dirty        bool     `json:"dirty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	DiffStat     string   `json:"diff_stat,omitempty"`
}

const (
	snapshotCommandTimeout = 5 * time.Second
	snapshotMaxFiles       = 200
	snapshotMaxDiffStat    = 4 * 1024
)

func collectDeliverySnapshot(workDir string) *DeliverySnapshot {
	if workDir == "" {
		return nil
	}
	if out, err := gitIn(workDir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return nil
	}

	snap := &DeliverySnapshot{
		Branch:   strings.TrimSpace(gitOk(workDir, "rev-parse", "--abbrev-ref", "HEAD")),
		HeadSHA:  strings.TrimSpace(gitOk(workDir, "rev-parse", "HEAD")),
		Upstream: strings.TrimSpace(gitOk(workDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")),
	}

	if status := gitOk(workDir, "status", "--porcelain"); status != "" {
		snap.Dirty = true
		lines := strings.Split(strings.TrimSpace(status), "\n")
		if len(lines) > snapshotMaxFiles {
			lines = append(lines[:snapshotMaxFiles], "…")
		}
		snap.ChangedFiles = lines
	}

	if stat := gitOk(workDir, "diff", "--stat", "HEAD"); len(stat) > snapshotMaxDiffStat {
		snap.DiffStat = stat[:snapshotMaxDiffStat] + "\n…"
	} else {
		snap.DiffStat = stat
	}

	if !snap.Dirty && snap.Branch == "" && snap.HeadSHA == "" {
		return nil
	}
	return snap
}

func gitOk(workDir string, args ...string) string {
	out, err := gitIn(workDir, args...)
	if err != nil {
		return ""
	}
	return out
}

func gitIn(workDir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), snapshotCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workDir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}
