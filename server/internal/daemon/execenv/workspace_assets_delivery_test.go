package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Workspace assets are only real if they land in the file the agent's runtime
// actually reads. Everything upstream of that is tested in pieces — the brief
// renders the sections, the writer writes a brief — and the join between them
// was not, which is precisely the seam that decides whether a rule written in
// the workspace reaches an agent at all.
//
// This matters beyond regression cover: team rules stay duplicated in each
// machine's own CLAUDE.md / AGENTS.md until this delivery is demonstrated, and
// "demonstrated" cannot mean the brief builder alone.

// injectionTargets are the two providers the workspace actually dispatches to,
// with the native file each reads. Codex is the one that would silently miss a
// CLAUDE.md-only assumption.
var injectionTargets = []struct {
	provider string
	file     string
}{
	{"claude", "CLAUDE.md"},
	{"codex", "AGENTS.md"},
}

func workspaceAssetContext() TaskContextForEnv {
	return TaskContextForEnv{
		IssueID: "55555555-6666-7777-8888-999999999999",
		WorkspaceContext: "# 团队通用指令\n\n" +
			"- 声称「已合入」必须带完整 40 位 commit SHA 与可复跑的 ancestry 命令。",
		IssuePhases: []IssuePhaseForEnv{
			{Name: "方案评审", Entered: true, Completed: true},
			{Name: "代码评审", Entered: true},
		},
		IssueDocs: []IssueDocForEnv{
			{ID: "b65528d4-0991-478a-ae2f-ecc8a1564aca", Title: "spec of record", Kind: "COC-305/spec"},
		},
	}
}

func TestWorkspaceAssetsReachTheFileEachRuntimeReads(t *testing.T) {
	t.Parallel()
	ctx := workspaceAssetContext()

	for _, target := range injectionTargets {
		target := target
		t.Run(target.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if _, err := InjectRuntimeConfig(dir, target.provider, ctx); err != nil {
				t.Fatalf("inject: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, target.file))
			if err != nil {
				t.Fatalf("the runtime's config file was not written: %v", err)
			}
			onDisk := string(raw)

			for _, want := range []string{
				// The workspace's own instructions, under the heading the
				// agent is told to obey.
				"## Workspace Context",
				"声称「已合入」必须带完整 40 位 commit SHA",
				// The issue's route and where the work stands.
				"## Issue Phases",
				"**代码评审 — CURRENT**",
				// The issue's own artefacts, named but not inlined.
				"## Issue Documents",
				"spec of record",
			} {
				if !strings.Contains(onDisk, want) {
					t.Errorf("%s is missing %q", target.file, want)
				}
			}
		})
	}
}

func TestBothRuntimesReceiveTheSameWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	// A rule that reaches one agent and not the other is worse than a rule
	// kept in two local files: it looks shared and behaves split.
	ctx := workspaceAssetContext()

	bodies := map[string]string{}
	for _, target := range injectionTargets {
		dir := t.TempDir()
		if _, err := InjectRuntimeConfig(dir, target.provider, ctx); err != nil {
			t.Fatalf("%s inject: %v", target.provider, err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, target.file))
		if err != nil {
			t.Fatalf("%s read: %v", target.provider, err)
		}
		bodies[target.provider] = extractSection(string(raw), "## Workspace Context")
	}

	if bodies["claude"] == "" {
		t.Fatal("no Workspace Context section was delivered to claude")
	}
	if bodies["claude"] != bodies["codex"] {
		t.Errorf("the two runtimes received different workspace instructions:\nclaude:\n%s\ncodex:\n%s",
			bodies["claude"], bodies["codex"])
	}
}

func TestAWorkspaceWithNoInstructionsDeliversNoHeading(t *testing.T) {
	t.Parallel()
	// An empty workspace context must not leave a bare heading behind: an
	// agent reading "## Workspace Context" followed by nothing reasonably
	// concludes the workspace has rules it failed to receive.
	ctx := workspaceAssetContext()
	ctx.WorkspaceContext = "   \n\n  "

	dir := t.TempDir()
	if _, err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("inject: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "## Workspace Context") {
		t.Error("a blank workspace context still emitted its heading")
	}
}

func TestInjectionKeepsWhatTheRepoAlreadyWrote(t *testing.T) {
	t.Parallel()
	// The managed block is appended to a repository's own CLAUDE.md rather
	// than replacing it. That ordering is what the layered-instruction rule
	// depends on, so losing the repo's bytes here would silently change which
	// rules an agent ends up following.
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const repoOwned = "# This repo's own rules\n\nBranch off main and delete the branch.\n"
	if err := os.WriteFile(path, []byte(repoOwned), 0o644); err != nil {
		t.Fatalf("seed repo file: %v", err)
	}

	if _, err := InjectRuntimeConfig(dir, "claude", workspaceAssetContext()); err != nil {
		t.Fatalf("inject: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	onDisk := string(raw)

	if !strings.Contains(onDisk, "Branch off main and delete the branch.") {
		t.Error("injection destroyed the repository's own instructions")
	}
	repoIdx := strings.Index(onDisk, "Branch off main")
	wsIdx := strings.Index(onDisk, "## Workspace Context")
	if repoIdx < 0 || wsIdx < 0 || repoIdx > wsIdx {
		t.Error("the managed block must come after the repository's own content")
	}
}

// extractSection returns the body under heading up to the next `## ` heading.
func extractSection(doc, heading string) string {
	start := strings.Index(doc, heading)
	if start < 0 {
		return ""
	}
	rest := doc[start+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return strings.TrimSpace(rest)
}
