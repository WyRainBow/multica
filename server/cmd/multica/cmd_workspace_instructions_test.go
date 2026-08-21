package main

import (
	"strings"
	"testing"
)

// Pulling rewrites somebody's own agent config. The whole contract is that it
// owns exactly one region of that file and leaves the rest alone, so these
// tests are about what survives rather than what gets written.

const pulledBlockSample = "# 团队通用指令\n\n- 声称「已合入」必须带完整 40 位 commit SHA。"

func TestPullingIntoAFileKeepsWhatWasAlreadyThere(t *testing.T) {
	t.Parallel()
	existing := "# My own rules\n\nAlways run the linter.\n"
	got := applyInstructionsBlock(existing, renderInstructionsBlock(pulledBlockSample))

	if !strings.Contains(got, "Always run the linter.") {
		t.Error("the file's own content was destroyed")
	}
	if !strings.Contains(got, pulledBlockSample) {
		t.Error("the pulled instructions did not land")
	}
	// Personal rules first, pulled block after: the owner's file reads as
	// theirs, with a managed region appended rather than prepended over it.
	if strings.Index(got, "Always run the linter.") > strings.Index(got, instructionsMarkerBegin) {
		t.Error("the managed block must come after the file's own content")
	}
}

func TestPullingTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	// A command that rewrites a config file on every invocation trains people
	// to ignore its diffs.
	block := renderInstructionsBlock(pulledBlockSample)
	once := applyInstructionsBlock("# Mine\n\nrule one\n", block)
	twice := applyInstructionsBlock(once, block)

	if once != twice {
		t.Errorf("a second pull changed the file:\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
}

func TestAChangedUpstreamReplacesOnlyTheBlock(t *testing.T) {
	t.Parallel()
	before := applyInstructionsBlock(
		"# Mine\n\nkeep me\n",
		renderInstructionsBlock("old rules"))
	after := applyInstructionsBlock(before, renderInstructionsBlock("new rules"))

	if strings.Contains(after, "old rules") {
		t.Error("the previous block survived; it must be replaced in full")
	}
	if !strings.Contains(after, "new rules") {
		t.Error("the new instructions did not land")
	}
	if !strings.Contains(after, "keep me") {
		t.Error("content outside the block was lost on update")
	}
	// Exactly one block, always. A second begin marker means the next pull
	// updates one copy and leaves the other to rot.
	if got := strings.Count(after, instructionsMarkerBegin); got != 1 {
		t.Errorf("found %d blocks, want exactly 1", got)
	}
}

func TestContentBelowTheBlockIsPreserved(t *testing.T) {
	t.Parallel()
	// Someone will write their own notes under the block. Replacing to
	// end-of-file would silently eat them.
	seeded := applyInstructionsBlock("# Mine\n", renderInstructionsBlock("old rules")) +
		"\n## My notes below\n\nremember this\n"
	got := applyInstructionsBlock(seeded, renderInstructionsBlock("new rules"))

	if !strings.Contains(got, "remember this") {
		t.Errorf("content after the block was eaten:\n%s", got)
	}
	if !strings.Contains(got, "new rules") {
		t.Error("the new instructions did not land")
	}
}

func TestAnEmptyFileGetsTheBlockAlone(t *testing.T) {
	t.Parallel()
	got := applyInstructionsBlock("", renderInstructionsBlock(pulledBlockSample))
	if strings.HasPrefix(got, "\n") {
		t.Errorf("a new file must not start with blank lines:\n%q", got)
	}
	if !strings.HasPrefix(got, instructionsMarkerBegin) {
		t.Error("a new file should lead with the block")
	}
}

func TestATruncatedBlockIsReplacedFromItsBeginMarker(t *testing.T) {
	t.Parallel()
	// A begin marker with no end means the file was cut off or hand-edited.
	// Everything from the marker on is no longer readable as the owner's
	// content, so replacing it is the only recoverable move — but what came
	// BEFORE it is still theirs.
	broken := "# Mine\n\nkeep me\n\n" + instructionsMarkerBegin + "\nhalf a block, no end"
	got := applyInstructionsBlock(broken, renderInstructionsBlock("new rules"))

	if !strings.Contains(got, "keep me") {
		t.Error("content before the broken marker was lost")
	}
	if strings.Contains(got, "half a block, no end") {
		t.Error("the truncated remains survived")
	}
	if got := strings.Count(got, instructionsMarkerEnd); got != 1 {
		t.Errorf("found %d end markers, want exactly 1", got)
	}
}

func TestTheBlockSaysWhereToEdit(t *testing.T) {
	t.Parallel()
	// Someone finding these rules inside their own config would otherwise
	// reasonably edit them there, and lose the edit on the next pull.
	block := renderInstructionsBlock(pulledBlockSample)
	if !strings.Contains(block, "Edits inside this block are lost on the next pull") {
		t.Error("the block must say that edits here do not survive")
	}
}

func TestExpandUserPathOnlyTouchesALeadingTilde(t *testing.T) {
	t.Parallel()
	// A tilde is a legal filename character anywhere else, and rewriting it
	// would send the write somewhere the caller never named.
	for _, in := range []string{"/etc/x~y", "relative/~odd", "./~"} {
		got, err := expandUserPath(in)
		if err != nil {
			t.Fatalf("expandUserPath(%q): %v", in, err)
		}
		if got != in {
			t.Errorf("expandUserPath(%q) = %q, want it unchanged", in, got)
		}
	}
	got, err := expandUserPath("~/x")
	if err != nil {
		t.Fatalf("expandUserPath(~/x): %v", err)
	}
	if strings.HasPrefix(got, "~") {
		t.Errorf("a leading ~/ was not expanded: %q", got)
	}
}
