package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pulling writes into somebody's own skills directory. The contract is that it
// can tell its own output apart from theirs, so these tests are mostly about
// what it refuses to touch.

func TestASkillWrittenByHandIsNotOurs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	own := filepath.Join(dir, "review-governance")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readPulledSkillMarker(own); ok {
		t.Error("a hand-written skill must not read as one we pulled; it would be overwritten")
	}
}

func TestAPulledSkillIsRecognisedAsOurs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "review-governance")
	skill := workspaceSkill{ID: "abc", Name: "review-governance", Content: "---\nname: x\n---\nbody"}
	body := renderPulledSkill(skill)
	print := pulledSkillFingerprint(body, skill.Files)

	if err := writePulledSkill(target, "cocoyu", skill, body, print); err != nil {
		t.Fatalf("write: %v", err)
	}
	marker, ok := readPulledSkillMarker(target)
	if !ok {
		t.Fatal("a skill we just wrote did not read back as ours")
	}
	if marker.Workspace != "cocoyu" || marker.SkillID != "abc" || marker.Fingerprint != print {
		t.Errorf("marker lost information: %+v", marker)
	}
	// The file a session actually loads has to be the skill, not our bookkeeping.
	raw, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(raw), "body") {
		t.Error("SKILL.md does not carry the skill")
	}
}

func TestOwnFrontmatterSurvivesUntouched(t *testing.T) {
	t.Parallel()
	// A skill carrying its own frontmatter must reach a manual session exactly
	// as the daemon delivers it, or the two surfaces describe it differently —
	// the failure that cost a whole review round earlier.
	content := "---\nname: orca-collab\ndescription: something specific\n---\n\nbody"
	got := renderPulledSkill(workspaceSkill{Name: "cross-terminal-collab", Description: "different", Content: content})
	if got != content {
		t.Errorf("existing frontmatter was rewritten:\n%s", got)
	}
	if strings.Contains(got, "different") {
		t.Error("the stored description overwrote the file's own")
	}
}

func TestFrontmatterIsSynthesizedOnlyWhenAbsent(t *testing.T) {
	t.Parallel()
	got := renderPulledSkill(workspaceSkill{Name: "Review Governance", Description: "when to use it", Content: "# Body\n"})
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("a skill with no frontmatter must get one, or runtimes drop it:\n%s", got)
	}
	if !strings.Contains(got, "name: review-governance") {
		t.Error("the synthesized name must be the sanitized directory name")
	}
	if !strings.Contains(got, "when to use it") {
		t.Error("the description is the routing signal and must travel")
	}
}

func TestTheFingerprintCoversSupportingFiles(t *testing.T) {
	t.Parallel()
	// A skill whose references changed while its body did not is still
	// changed; a fingerprint ignoring them reports it current forever.
	body := "same body"
	a := pulledSkillFingerprint(body, []skillFile{{Path: "references/x.md", Content: "one"}})
	b := pulledSkillFingerprint(body, []skillFile{{Path: "references/x.md", Content: "two"}})
	if a == b {
		t.Error("a changed supporting file did not change the fingerprint")
	}
	// Order is the server's business, not a change.
	f1 := []skillFile{{Path: "a", Content: "1"}, {Path: "b", Content: "2"}}
	f2 := []skillFile{{Path: "b", Content: "2"}, {Path: "a", Content: "1"}}
	if pulledSkillFingerprint(body, f1) != pulledSkillFingerprint(body, f2) {
		t.Error("reordering the same files read as a change")
	}
}

func TestASupportingFilePathCannotEscapeItsSkill(t *testing.T) {
	t.Parallel()
	// Supporting files are content from the server. A path climbing out of the
	// skill directory would write anywhere the user can.
	dir := t.TempDir()
	target := filepath.Join(dir, "evil")
	skill := workspaceSkill{
		ID: "x", Name: "evil", Content: "body",
		Files: []skillFile{
			{Path: "../../escaped.md", Content: "should not exist"},
			{Path: "references/ok.md", Content: "fine"},
		},
	}
	if err := writePulledSkill(target, "ws", skill, renderPulledSkill(skill), "print"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escaped.md")); err == nil {
		t.Error("a supporting file escaped the skill directory")
	}
	if _, err := os.Stat(filepath.Join(target, "references", "ok.md")); err != nil {
		t.Error("a legitimate supporting file was not written")
	}
}

func TestSkillNamesBecomeSafeDirectoryNames(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"review-governance": "review-governance",
		"Review Governance": "review-governance",
		"../../etc/passwd":  "etcpasswd",
		"  spaced  ":        "spaced",
		"...":               "",
		"a/b":               "ab",
	} {
		if got := sanitizePulledSkillName(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
