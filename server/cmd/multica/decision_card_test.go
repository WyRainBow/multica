package main

import (
	"strings"
	"testing"
)

// The card is write-once and its status is never stored, so everything a
// reader needs to work out whether a decision still holds has to be legible in
// the card itself. These tests pin what has to survive rendering.

func TestTheHeaderCarriesEverythingDerivationNeeds(t *testing.T) {
	t.Parallel()
	card := RenderDecisionCard(DecisionMeta{
		ID: "D2", Question: "载体", Summary: "独立卡",
		DecidedBy: "用户", RecordedBy: "Claude", DecidedAt: "2026-08-21T09:00:00Z",
		Open: []string{"命令叫什么"}, Closes: []string{"D1#2"}, Supersedes: []string{"D1"},
	}, "正文")

	for _, want := range []string{
		"id: D2", "question: 载体", "summary: 独立卡",
		"decided_by: 用户", "recorded_by: Claude",
		"open: 命令叫什么", "closes: D1#2", "supersedes: D1",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("header is missing %q", want)
		}
	}
	if !strings.Contains(card, "正文") {
		t.Error("the body did not survive")
	}
}

func TestTheDeciderAndRecorderAreBothNamedEvenWhenIdentical(t *testing.T) {
	t.Parallel()
	// A single name leaves the reader guessing whether it means "decided" or
	// "typed it up". Both are always written.
	card := RenderDecisionCard(DecisionMeta{
		ID: "D1", Question: "q", Summary: "s", DecidedBy: "Claude", RecordedBy: "Claude",
	}, "")
	if !strings.Contains(card, "**拍板者**：Claude") || !strings.Contains(card, "**记录者**：Claude") {
		t.Errorf("both roles must be named:\n%s", card)
	}
}

func TestANewlineInAFieldCannotBreakTheHeader(t *testing.T) {
	t.Parallel()
	// A newline inside a value would end the field early and turn the rest of
	// it into an unparseable line, silently losing whatever followed.
	card := RenderDecisionCard(DecisionMeta{
		ID: "D1", Question: "第一行\n第二行", Summary: "s",
	}, "")
	header := card[:strings.Index(card, "-->")]
	if strings.Contains(header, "第一行\n第二行") {
		t.Error("a multi-line value was written verbatim into the header")
	}
	if !strings.Contains(header, "question: 第一行 第二行") {
		t.Errorf("the value should be folded onto one line:\n%s", header)
	}
}

func TestAnAbsentFieldIsNotWrittenAsBlank(t *testing.T) {
	t.Parallel()
	// An absent field and a field set to nothing are different claims.
	card := RenderDecisionCard(DecisionMeta{ID: "D1", Question: "q", Summary: "s"}, "")
	if strings.Contains(card, "sha:") {
		t.Error("an unset baseline was written as an empty field")
	}
	if strings.Contains(card, "supersedes:") {
		t.Error("an empty supersede list was written as a field")
	}
}

func TestOpenQuestionsAreNumberedSoTheyCanBeClosed(t *testing.T) {
	t.Parallel()
	// A later card closes one by position. Without the numbering in the body a
	// person cannot see which index to pass.
	card := RenderDecisionCard(DecisionMeta{
		ID: "D1", Question: "q", Summary: "s",
		Open: []string{"第一个", "第二个"},
	}, "")
	if !strings.Contains(card, "1. 第一个") || !strings.Contains(card, "2. 第二个") {
		t.Errorf("open questions must be numbered:\n%s", card)
	}
	if !strings.Contains(card, "`--closes`") {
		t.Error("the card should say how an open question gets closed")
	}
}

func TestDecisionNumberingCountsOnlyDecisionCards(t *testing.T) {
	t.Parallel()
	docs := []docRow{
		{Kind: "COC-311/decisions/D1"},
		{Kind: "COC-311/decisions/D3"},
		{Kind: "COC-311/rounds/R9-方案评审"},
		{Kind: "COC-311/spec"},
		{Kind: "OTHER/decisions/D7"},
	}
	// Counted from the cards, so a gap stays visible and a decision cannot be
	// recorded twice under one number.
	if got := NextDecisionNumber("COC-311", docs); got != 4 {
		t.Errorf("next decision = %d, want 4", got)
	}
	if got := NextDecisionNumber("COC-999", docs); got != 1 {
		t.Errorf("an issue with no decisions should start at 1, got %d", got)
	}
}

func TestParseDecisionNumberRejectsWhatIsNotOne(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"D0", "D", "Dx", "1", "", "D-1", "R1"} {
		if _, ok := ParseDecisionNumber(in); ok {
			t.Errorf("%q parsed as a decision number", in)
		}
	}
	if n, ok := ParseDecisionNumber(" D12 "); !ok || n != 12 {
		t.Errorf("D12 = %d, %v", n, ok)
	}
}
