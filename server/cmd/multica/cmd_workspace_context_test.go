package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The map's job is to say what exists without claiming to be everything, and
// without repeating the content it points at. These tests pin the parts that
// went wrong on the first real run and the boundaries that keep it honest.

func TestNativeSkillCountIsTheRealOne(t *testing.T) {
	t.Parallel()
	// The count was read after truncation, so 56 local skills reported as 9.
	// A boundary marker with the wrong number draws the boundary in the wrong
	// place, which is worse than not drawing it.
	native := make([]string, 56)
	for i := range native {
		native[i] = string(rune('a' + i%26))
	}
	total, shown := nativeSkillSummary(native)
	if total != 56 {
		t.Errorf("total = %d, want the real 56", total)
	}
	if len(shown) != 9 {
		t.Errorf("shown = %d entries, want 8 names plus the remainder line", len(shown))
	}
	if !strings.Contains(shown[len(shown)-1], "48") {
		t.Errorf("the remainder line must name what it left out: %q", shown[len(shown)-1])
	}
	// A short list is shown whole and says nothing about a remainder.
	total, shown = nativeSkillSummary([]string{"a", "b"})
	if total != 2 || len(shown) != 2 {
		t.Errorf("a short list must render whole: %d / %v", total, shown)
	}
}

func TestOurMirrorIsToldApartFromSomebodysOwnSkill(t *testing.T) {
	t.Parallel()
	// The sidecar is the only reliable tell. Without it a local skill would be
	// counted as ours, and the map would claim to manage something it does not.
	dir := t.TempDir()
	for _, name := range []string{"mine-one", "mine-two"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ours := filepath.Join(dir, "review-governance")
	skill := workspaceSkill{ID: "x", Name: "review-governance", Content: "body"}
	if err := writePulledSkill(ours, "cocoyu", skill, RenderDecisionCardNoop(), "print"); err != nil {
		t.Fatal(err)
	}
	// A dotfile is not a skill.
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mirrored, native := splitLocalSkills(dir)
	if mirrored != 1 {
		t.Errorf("mirrored = %d, want 1", mirrored)
	}
	if len(native) != 2 || native[0] != "mine-one" || native[1] != "mine-two" {
		t.Errorf("native = %v, want the two hand-written ones sorted", native)
	}
}

func TestAMissingSkillsDirIsNotAnError(t *testing.T) {
	t.Parallel()
	// The flag is optional and a machine may have no such directory. Reporting
	// nothing is right; failing the whole map is not.
	mirrored, native := splitLocalSkills(filepath.Join(t.TempDir(), "nope"))
	if mirrored != 0 || len(native) != 0 {
		t.Errorf("expected an empty answer, got %d / %v", mirrored, native)
	}
}

func TestFrontmatterDescriptionWinsOverTheStoredOne(t *testing.T) {
	t.Parallel()
	// The on-disk SKILL.md keeps its own frontmatter, so showing the stored
	// field would name the same skill two ways in one session.
	s := workspaceSkill{
		Name:        "cross-terminal-collab",
		Description: "stored",
		Content:     "---\nname: x\ndescription: from the file\n---\n\nbody",
	}
	if got := effectiveSkillDescriptionForContext(s); got != "from the file" {
		t.Errorf("got %q, want the file's own description", got)
	}
	// With no frontmatter the stored one is all there is.
	s.Content = "# body only"
	if got := effectiveSkillDescriptionForContext(s); got != "stored" {
		t.Errorf("got %q, want the stored description as fallback", got)
	}
}

func TestAKindWithNonASCIISurvivesTheQueryString(t *testing.T) {
	t.Parallel()
	// The asset folders are named in Chinese. A kind that does not survive
	// escaping returns nothing, and the section vanishes without an error —
	// exactly how the assets section shipped empty the first time.
	got := urlQueryEscape("AgentWiki/cases_案例")
	if strings.Contains(got, "案") {
		t.Errorf("non-ASCII was not escaped: %q", got)
	}
	if !strings.HasPrefix(got, "AgentWiki/cases_") {
		t.Errorf("the ASCII path was mangled: %q", got)
	}
}
