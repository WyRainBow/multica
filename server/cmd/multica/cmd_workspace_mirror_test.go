package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The mirror's job is to make a rule change reviewable. These tests pin the
// parts where getting it wrong would either let unreviewed text into the
// database or make the diff unreadable.

func mirrorRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestApplyRefusesWhatNobodyReviewed(t *testing.T) {
	t.Parallel()
	repo := mirrorRepo(t)

	// A clean main is the only state that means "a human approved this".
	if err := requireReviewedCheckout(repo); err != nil {
		t.Fatalf("a clean main must be accepted: %v", err)
	}

	// Uncommitted edits ride in with the approved ones if this passes.
	if err := os.WriteFile(filepath.Join(repo, "skills.md"), []byte("sneaky\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireReviewedCheckout(repo); err == nil {
		t.Error("a dirty checkout must be refused: those edits went through no review")
	}

	cmd := exec.Command("git", "-C", repo, "checkout", "-b", "agent/claude/COC-1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v\n%s", err, out)
	}
	if err := requireReviewedCheckout(repo); err == nil {
		t.Error("a feature branch must be refused: a branch is by definition not yet merged")
	}

	// Not a repository at all.
	if err := requireReviewedCheckout(t.TempDir()); err == nil {
		t.Error("a plain directory must be refused")
	}
}

func TestApplyRefusesAnotherWorkspacesMirror(t *testing.T) {
	repo := mirrorRepo(t)
	manifest, _ := json.Marshal(mirrorManifest{Workspace: "someone-else"})
	if err := os.WriteFile(filepath.Join(repo, mirrorManifestFile), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "manifest"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/workspaces/") {
			json.NewEncoder(w).Encode(map[string]any{"slug": "cocoyu", "context": "rules"})
			return
		}
		t.Errorf("apply must not reach %s before the workspace check", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{}
	cmd.Flags().String("dir", repo, "")
	cmd.Flags().Bool("check", false, "")
	cmd.Flags().Bool("allow-dirty", false, "")
	err := runWorkspaceApply(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "someone-else") {
		t.Fatalf("applying one workspace's rules onto another must be refused, got %v", err)
	}
}

func TestFrontmatterRoundTripsTheBodyExactly(t *testing.T) {
	t.Parallel()
	// apply reads the body back out of this; a lossy round trip writes a
	// mangled rule into the database.
	body := "# 团队通用指令\n\n- 一条: 带冒号\n- 另一条\n"
	doc := renderFrontmatter(map[string]string{
		"workspace": "cocoyu", "fingerprint": "abc123", "empty": "",
	}, body)
	if strings.Contains(doc, "empty:") {
		t.Error("an empty field must be dropped, not written blank")
	}
	front, got := splitFrontmatter(doc)
	if !strings.Contains(front, "workspace: cocoyu") {
		t.Errorf("frontmatter lost its fields: %q", front)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(body) {
		t.Errorf("body changed across the round trip:\nwant %q\ngot  %q", body, got)
	}
	// A hand-written file with no frontmatter is all body.
	if _, plain := splitFrontmatter("no frontmatter here\n"); plain != "no frontmatter here\n" {
		t.Errorf("a file without frontmatter must come back whole, got %q", plain)
	}
}

func TestATitleThatWouldBreakTheTreeIsMadeSafe(t *testing.T) {
	t.Parallel()
	used := map[string]string{}
	got := wikiRelativePath(mirrorCard{
		ID: "aaaabbbb-1111", Kind: "AgentWiki/cases_案例", Title: "改了写入面/不等于改了消费面",
	}, used)
	// wiki / AgentWiki / cases_案例 / <file> — the slash inside the TITLE must
	// not have added a fourth level.
	if strings.Count(got, string(filepath.Separator)) != 3 {
		t.Errorf("a slash in the title must not add a directory level: %q", got)
	}
	if !strings.HasPrefix(got, "wiki"+string(filepath.Separator)) {
		t.Errorf("documents belong under wiki/, not a folder that mislabels issue artefacts: %q", got)
	}
	if !strings.Contains(got, "案例") {
		t.Errorf("Chinese must survive: %q", got)
	}
	// Two documents with the same kind and title must not overwrite each other.
	second := wikiRelativePath(mirrorCard{
		ID: "ccccdddd-2222", Kind: "AgentWiki/cases_案例", Title: "改了写入面/不等于改了消费面",
	}, used)
	if second == got {
		t.Fatalf("a title collision silently dropped a document: both went to %q", got)
	}
}

func TestExportedSkillIsWhatApplyWouldWriteBack(t *testing.T) {
	t.Parallel()
	// A skill whose content carries no frontmatter must NOT gain a synthesized
	// one on the way out. `apply` writes SKILL.md back verbatim, so anything
	// export adds here gets written into the database on the next round trip —
	// the mirror would rewrite the rule it was only supposed to record.
	skill := workspaceSkill{
		ID: "516aec4c", Name: "review-governance", Description: "d",
		Content: "# 评审治理\n\n本 Skill 是唯一正文。\n",
	}
	body := mirrorSkillBody(skill)
	if body != skill.Content {
		t.Errorf("export changed the body, so a round trip would rewrite the database:\nwant %q\ngot  %q",
			skill.Content, body)
	}
	// One that ships its own frontmatter keeps it, unchanged.
	withFront := workspaceSkill{Content: "---\nname: x\n---\n\nbody\n"}
	if got := mirrorSkillBody(withFront); got != withFront.Content {
		t.Errorf("an existing frontmatter must survive untouched: %q", got)
	}
}

func TestSidecarFieldReadsTheID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, mirrorSkillSidecar),
		[]byte("id: 448e3f54-657c\nname: interview-retro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sidecarField(dir, "id"); got != "448e3f54-657c" {
		t.Errorf("id = %q", got)
	}
	// No sidecar means a hand-added skill, not an error.
	if got := sidecarField(t.TempDir(), "id"); got != "" {
		t.Errorf("a missing sidecar must read as empty, got %q", got)
	}
}
