package service

import (
	"strings"
	"testing"
)

func TestSummarizeDescriptionChange_CountsAddedAndRemovedLines(t *testing.T) {
	got := SummarizeDescriptionChange(
		"keep one\nremove me\nkeep two",
		"keep one\nkeep two\nadd me\nadd me too",
	)
	if got.AddedLines != 2 || got.RemovedLines != 1 {
		t.Fatalf("added/removed = %d/%d, want 2/1", got.AddedLines, got.RemovedLines)
	}
}

// A Markdown editor re-serializes spacing on every save. Counting that would
// make every edit look bigger than it was, and a pure-whitespace save would
// report a change nobody made.
func TestSummarizeDescriptionChange_IgnoresBlankLineChurn(t *testing.T) {
	got := SummarizeDescriptionChange("line one\nline two", "line one\n\n\nline two\n")
	if got.AddedLines != 0 || got.RemovedLines != 0 {
		t.Fatalf("blank-line churn counted as %d/%d, want 0/0", got.AddedLines, got.RemovedLines)
	}
}

func TestSummarizeDescriptionChange_IgnoresTrailingWhitespace(t *testing.T) {
	got := SummarizeDescriptionChange("line one", "line one   ")
	if got.AddedLines != 0 || got.RemovedLines != 0 {
		t.Fatalf("trailing whitespace counted as %d/%d, want 0/0", got.AddedLines, got.RemovedLines)
	}
}

// Editing one line is a replacement, not an unrelated add and delete — the
// counts should say +1/-1, which is what an LCS gives and a naive
// line-by-line comparison does not.
func TestSummarizeDescriptionChange_EditedLineCountsAsOneEach(t *testing.T) {
	got := SummarizeDescriptionChange("a\nb\nc", "a\nB CHANGED\nc")
	if got.AddedLines != 1 || got.RemovedLines != 1 {
		t.Fatalf("added/removed = %d/%d, want 1/1", got.AddedLines, got.RemovedLines)
	}
}

func TestSummarizeDescriptionChange_MarksFirstWrite(t *testing.T) {
	got := SummarizeDescriptionChange("", "# Plan\n\nfirst draft")
	if !got.Created {
		t.Fatalf("writing a description for the first time was not marked as created")
	}
	if got.Cleared {
		t.Fatalf("created change also reported as cleared")
	}
}

func TestSummarizeDescriptionChange_MarksClearing(t *testing.T) {
	got := SummarizeDescriptionChange("something", "")
	if !got.Cleared || got.Created {
		t.Fatalf("clearing reported created=%v cleared=%v", got.Created, got.Cleared)
	}
}

// Whitespace-only content is not a description. Saving "   " over real text is
// a clear, not an edit that happens to leave one line.
func TestSummarizeDescriptionChange_WhitespaceOnlyCountsAsEmpty(t *testing.T) {
	got := SummarizeDescriptionChange("real text", "   \n\n  ")
	if !got.Cleared {
		t.Fatalf("whitespace-only replacement was not treated as clearing")
	}
}

func TestSummarizeDescriptionChange_NamesTheSectionsThatChanged(t *testing.T) {
	before := "# Overview\n\nintro\n\n## Plan\n\nstep one\n\n## Risks\n\nnone"
	after := "# Overview\n\nintro\n\n## Plan\n\nstep one\nstep two\n\n## Risks\n\nnone"

	got := SummarizeDescriptionChange(before, after)
	if len(got.Sections) != 1 || got.Sections[0] != "Plan" {
		t.Fatalf("sections = %v, want [Plan]", got.Sections)
	}
}

func TestSummarizeDescriptionChange_ReportsAddedAndDeletedSections(t *testing.T) {
	before := "## Kept\n\nsame\n\n## Gone\n\nbye"
	after := "## Kept\n\nsame\n\n## Fresh\n\nhello"

	got := SummarizeDescriptionChange(before, after)
	if len(got.Sections) != 2 {
		t.Fatalf("sections = %v, want two entries", got.Sections)
	}
	// The new document's heading comes first; the deleted one follows, since
	// it has no position in what the reader is looking at.
	if got.Sections[0] != "Fresh" || got.Sections[1] != "Gone" {
		t.Fatalf("sections = %v, want [Fresh Gone]", got.Sections)
	}
}

func TestSummarizeDescriptionChange_SectionsFollowNewDocumentOrder(t *testing.T) {
	before := "## A\n\na\n\n## B\n\nb\n\n## C\n\nc"
	after := "## A\n\na CHANGED\n\n## B\n\nb\n\n## C\n\nc CHANGED"

	got := SummarizeDescriptionChange(before, after)
	if len(got.Sections) != 2 || got.Sections[0] != "A" || got.Sections[1] != "C" {
		t.Fatalf("sections = %v, want [A C] in document order", got.Sections)
	}
}

func TestSummarizeDescriptionChange_NoSectionsWhenOnlyIntroChanged(t *testing.T) {
	// Text before the first heading has no name to show, so it contributes to
	// the line counts and nothing else.
	got := SummarizeDescriptionChange("intro\n\n## Plan\n\nstep", "intro edited\n\n## Plan\n\nstep")
	if len(got.Sections) != 0 {
		t.Fatalf("sections = %v, want none", got.Sections)
	}
	if got.AddedLines != 1 || got.RemovedLines != 1 {
		t.Fatalf("added/removed = %d/%d, want 1/1", got.AddedLines, got.RemovedLines)
	}
}

func TestSummarizeDescriptionChange_IgnoresNonHeadings(t *testing.T) {
	// `#hashtag` has no space, and seven hashes is past the Markdown limit —
	// neither opens a section.
	got := SummarizeDescriptionChange("#hashtag\n####### deep\n\nbody", "#hashtag\n####### deep\n\nbody edited")
	if len(got.Sections) != 0 {
		t.Fatalf("sections = %v, want none", got.Sections)
	}
}

func TestSummarizeDescriptionChange_NoChangeReportsNothing(t *testing.T) {
	text := "## Plan\n\nstep one"
	got := SummarizeDescriptionChange(text, text)
	if got.AddedLines != 0 || got.RemovedLines != 0 || len(got.Sections) != 0 {
		t.Fatalf("identical text reported %+v", got)
	}
}

// The exact diff is quadratic, so it is abandoned past a size guard. The
// numbers must stay sane there rather than the summary blowing up or hanging.
func TestSummarizeDescriptionChange_HugeDocumentStillSummarizes(t *testing.T) {
	before := strings.Repeat("a line\n", 3000)
	after := before + strings.Repeat("new line\n", 5)

	got := SummarizeDescriptionChange(before, after)
	if got.AddedLines != 5 {
		t.Fatalf("added = %d, want 5", got.AddedLines)
	}
	if got.RemovedLines != 0 {
		t.Fatalf("removed = %d, want 0", got.RemovedLines)
	}
}

func TestSummarizeDescriptionChange_NormalizesWindowsLineEndings(t *testing.T) {
	got := SummarizeDescriptionChange("a\r\nb", "a\nb")
	if got.AddedLines != 0 || got.RemovedLines != 0 {
		t.Fatalf("line-ending normalization counted as %d/%d, want 0/0",
			got.AddedLines, got.RemovedLines)
	}
}
