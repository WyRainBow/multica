package main

import (
	"strings"
	"testing"
)

func TestParseRoundKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in     string
		number int
		phase  string
		ok     bool
	}{
		{"R1-方案评审", 1, "方案评审", true},
		{"R12-代码评审", 12, "代码评审", true},
		{"R0-代码评审", 0, "", false},
		{"rounds", 0, "", false},
		{"R2", 0, "", false},
	} {
		n, p, ok := ParseRoundKind(c.in)
		if ok != c.ok || n != c.number || p != c.phase {
			t.Errorf("ParseRoundKind(%q) = (%d, %q, %v), want (%d, %q, %v)",
				c.in, n, p, ok, c.number, c.phase, c.ok)
		}
	}
}

func TestNextRoundNumberComesFromTheDocuments(t *testing.T) {
	t.Parallel()
	if got := NextRoundNumber(nil); got != 1 {
		t.Errorf("first round = %d, want 1", got)
	}
	// Counting from the documents rather than from a stored counter: a gap
	// stays visible, and a round cannot be closed twice under one number.
	rounds := []RoundDoc{{Number: 1}, {Number: 3}}
	if got := NextRoundNumber(rounds); got != 4 {
		t.Errorf("after R1 and R3 = %d, want 4", got)
	}
}

func TestRoundSectionLeadsWithTheLatestRound(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{
		{Number: 1, Phase: "方案评审", Verdict: "approve", Summary: "定了三本账", DocID: "d1"},
		{Number: 2, Phase: "代码评审", Verdict: "request_changes", Summary: "撤回假绿", DocID: "d2"},
	})
	r1 := strings.Index(section, "| R1 |")
	r2 := strings.Index(section, "| R2 |")
	if r2 < 0 || r1 < 0 || r2 > r1 {
		t.Errorf("rounds are not newest-first:\n%s", section)
	}
}

func TestASummaryCannotBreakItsRow(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{
		{Number: 1, Phase: "代码评审", Verdict: "approve", Summary: "a | b", DocID: "d1"},
	})
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| R1 |") {
			// Five columns means six pipes; an unescaped one in the summary
			// would silently add a column and shift every value right.
			if got := strings.Count(line, "|") - strings.Count(line, `\|`); got != 6 {
				t.Errorf("row has %d structural pipes, want 6: %s", got, line)
			}
		}
	}
}

func TestApplyRoundSectionLeavesTheRestOfTheSpecAlone(t *testing.T) {
	t.Parallel()
	spec := "# 目标\n\n把三本账分开。\n"
	once := ApplyRoundSection(spec, []RoundDoc{{Number: 1, Summary: "一"}})
	if !strings.HasPrefix(once, spec) {
		t.Errorf("prose was disturbed:\n%s", once)
	}

	// Closing a second round rewrites the section, not the document — and does
	// not leave the first one behind.
	twice := ApplyRoundSection(once, []RoundDoc{{Number: 1, Summary: "一"}, {Number: 2, Summary: "二"}})
	if !strings.HasPrefix(twice, spec) {
		t.Errorf("prose was disturbed on the second pass:\n%s", twice)
	}
	if n := strings.Count(twice, specSectionOpen); n != 1 {
		t.Errorf("%d sections after two closes, want 1", n)
	}
	if !strings.Contains(twice, "| R2 |") {
		t.Errorf("second round missing:\n%s", twice)
	}
}

func TestApplyRoundSectionRepairsHandEditedMarkers(t *testing.T) {
	t.Parallel()
	// Someone deleted the closing marker. Appending a fresh section would
	// leave the spec claiming two sets of current conclusions.
	damaged := "# 目标\n\n" + specSectionOpen + "\n\n旧内容\n"
	fixed := ApplyRoundSection(damaged, []RoundDoc{{Number: 1, Summary: "一"}})
	if n := strings.Count(fixed, specSectionOpen); n != 1 {
		t.Errorf("%d opening markers, want 1:\n%s", n, fixed)
	}
	if strings.Contains(fixed, "旧内容") {
		t.Errorf("stale content survived inside the managed section:\n%s", fixed)
	}
}

func TestAnEmptySpecStillGetsItsSection(t *testing.T) {
	t.Parallel()
	out := ApplyRoundSection("", nil)
	if !strings.Contains(out, specSectionOpen) || !strings.Contains(out, "尚无收口轮次") {
		t.Errorf("empty spec did not get a section:\n%s", out)
	}
}
