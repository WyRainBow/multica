package main

import (
	"strings"
	"testing"
)

const anchorDoc = "# 概述\n\n背景。\n\n## V1 结论\n\n第一处。\n\n## 补充\n\n再次提到 V1 结论 这个词。"

func TestAnchorOffsetInText_FindsThePassage(t *testing.T) {
	got, err := anchorOffsetInText(anchorDoc, "V1 结论", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The offset is meaningful only if reading from it returns the passage.
	runes := []rune(anchorDoc)
	if string(runes[got:got+len([]rune("V1 结论"))]) != "V1 结论" {
		t.Fatalf("offset %d does not point at the anchor", got)
	}
}

// Offsets are character-based, matching how the editor re-locates an anchor.
// A byte offset would be wrong for every CJK description — and would be wrong
// by an amount that grows with how far into the document the passage sits.
func TestAnchorOffsetInText_CountsCharactersNotBytes(t *testing.T) {
	got, err := anchorOffsetInText("中文前缀 target", "target", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := len([]rune("中文前缀 ")); got != want {
		t.Fatalf("offset = %d, want %d (bytes would give %d)",
			got, want, strings.Index("中文前缀 target", "target"))
	}
}

func TestAnchorOffsetInText_SelectsTheRequestedOccurrence(t *testing.T) {
	first, err := anchorOffsetInText(anchorDoc, "V1 结论", 1)
	if err != nil {
		t.Fatalf("occurrence 1: %v", err)
	}
	second, err := anchorOffsetInText(anchorDoc, "V1 结论", 2)
	if err != nil {
		t.Fatalf("occurrence 2: %v", err)
	}
	if !(second > first) {
		t.Fatalf("occurrence 2 (%d) is not after occurrence 1 (%d)", second, first)
	}
}

// A mistyped passage has to fail at the CLI. Sending it anyway would create a
// comment that silently never highlights anything — the failure would surface
// as "the feature is broken" long after the command reported success.
func TestAnchorOffsetInText_RejectsAPassageThatIsNotThere(t *testing.T) {
	_, err := anchorOffsetInText(anchorDoc, "这段不存在", 1)
	if err == nil {
		t.Fatalf("expected an error for a passage that is not in the text")
	}
	if !strings.Contains(err.Error(), "verbatim") {
		t.Fatalf("error should tell the caller to copy the passage verbatim: %v", err)
	}
}

// Asking for an occurrence past the end names how many there actually are, so
// the caller can correct the number instead of guessing.
func TestAnchorOffsetInText_ReportsHowManyOccurrencesExist(t *testing.T) {
	_, err := anchorOffsetInText(anchorDoc, "V1 结论", 5)
	if err == nil {
		t.Fatalf("expected an error for occurrence 5")
	}
	if !strings.Contains(err.Error(), "2 time(s)") {
		t.Fatalf("error should report the real count: %v", err)
	}
}

func TestAnchorOffsetInText_TrimsTheAnchor(t *testing.T) {
	// A passage copied out of a document usually carries surrounding
	// whitespace; matching it verbatim would fail against its own source.
	got, err := anchorOffsetInText(anchorDoc, "  V1 结论\n", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want, _ := anchorOffsetInText(anchorDoc, "V1 结论", 1); got != want {
		t.Fatalf("trimmed anchor resolved to %d, want %d", got, want)
	}
}

func TestAnchorOffsetInText_RejectsABlankAnchor(t *testing.T) {
	if _, err := anchorOffsetInText(anchorDoc, "   ", 1); err == nil {
		t.Fatalf("expected an error for a blank anchor")
	}
}

func TestAnchorOffsetInText_MatchesAMultiLinePassage(t *testing.T) {
	// Selecting a whole paragraph is the common case for "explain this part".
	passage := "## V1 结论\n\n第一处。"
	got, err := anchorOffsetInText(anchorDoc, passage, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	runes := []rune(anchorDoc)
	if string(runes[got:got+len([]rune(passage))]) != passage {
		t.Fatalf("offset %d does not point at the multi-line passage", got)
	}
}
