package handler

import (
	"strings"
	"testing"
)

// Exactly one document on an issue states where it stands. Getting this wrong
// in either direction is costly: miss the spec and the brief has no state of
// record, mistake a round for it and the brief claims two.

func TestOnlyTheSpecCountsAsTheStateOfRecord(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"COC-305/spec", "MUL-1/spec", "COC-305/spec/"} {
		if !isCurrentIssueDoc(kind) {
			t.Errorf("%q should be the state of record", kind)
		}
	}
	for _, kind := range []string{
		"COC-305/rounds/R1-方案评审",
		"COC-305/decisions/D1",
		"COC-305/reviews/R1-方案评审",
		// Suffix-anchored on purpose: a round about the spec is still a round.
		"COC-305/rounds/R1-spec-review",
		"COC-305/specification",
		"",
	} {
		if isCurrentIssueDoc(kind) {
			t.Errorf("%q must not be treated as the state of record", kind)
		}
	}
}

func TestOnlyTheManagedSectionOfASpecTravels(t *testing.T) {
	t.Parallel()
	// The rest of a spec is hand-written — goals, scope, acceptance — and
	// belongs to whoever wrote it. Shipping all of it would turn "read the
	// conclusions" into "read the whole document" on every turn.
	spec := "# 目标\n\n手写的目标与验收口径。\n\n" +
		specRoundsOpen + "\n\n## 轮次结论\n\n| R1 | approve |\n\n" + specRoundsClose +
		"\n\n# 附录\n\n更多手写内容。\n"

	got := extractSpecConclusions(spec)
	if !strings.Contains(got, "R1 | approve") {
		t.Errorf("the conclusions did not travel:\n%s", got)
	}
	for _, mustNotTravel := range []string{"手写的目标与验收口径", "更多手写内容", specRoundsOpen, specRoundsClose} {
		if strings.Contains(got, mustNotTravel) {
			t.Errorf("%q escaped the managed section", mustNotTravel)
		}
	}
}

func TestASpecWithoutTheSectionYieldsNothing(t *testing.T) {
	t.Parallel()
	// The honest answer for a spec nobody has closed a round on. Guessing here
	// would ship hand-written prose as if it were recorded conclusions.
	for _, spec := range []string{
		"",
		"# 目标\n\n没有轮次段。\n",
		// An opening marker with no close means the spec was hand-edited.
		// Guessing where the section ends could ship the rest of the document.
		"# 目标\n\n" + specRoundsOpen + "\n\n未闭合，后面全是手写。\n",
		specRoundsOpen + "\n\n   \n\n" + specRoundsClose,
	} {
		if got := extractSpecConclusions(spec); got != "" {
			t.Errorf("expected nothing from %q, got %q", spec, got)
		}
	}
}

func TestAnOversizedConclusionsSectionSaysItWasCut(t *testing.T) {
	t.Parallel()
	// Truncation that does not admit itself reads as the whole set, and the
	// agent stops looking exactly where the older rounds are.
	long := strings.Repeat("轮", specConclusionsBriefLimit+50)
	got := extractSpecConclusions(specRoundsOpen + "\n" + long + "\n" + specRoundsClose)
	if !strings.Contains(got, "truncated") {
		t.Error("an oversized section was cut without saying so")
	}
	if len([]rune(got)) > specConclusionsBriefLimit+80 {
		t.Errorf("truncation did not bound the payload: %d runes", len([]rune(got)))
	}
}
