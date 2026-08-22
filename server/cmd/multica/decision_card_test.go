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

func TestAffectsLinksADecisionToWhatItChanged(t *testing.T) {
	t.Parallel()
	// A decision is not a fourth document type — it is the event that moves one
	// of the three. The link belongs on the decision, where it is written once,
	// rather than in the document, where it would be maintained by hand.
	card := RenderDecisionCard(DecisionMeta{
		ID: "D8", Question: "需求怎么改", Summary: "拆成两条",
		Affects: []string{"requirements", "spec"},
	}, "")
	if !strings.Contains(card, "affects: requirements") || !strings.Contains(card, "affects: spec") {
		t.Errorf("both affected documents must be in the header:\n%s", card)
	}
	if !strings.Contains(card, "**改动了**") {
		t.Error("a person reading the card should see it too, not only the parser")
	}
}

func TestOnlyLiveDocumentsGetSnapshotted(t *testing.T) {
	t.Parallel()
	// Rounds and decisions are already write-once; snapshotting them would
	// duplicate immutable records for nothing.
	for _, kind := range []string{"K/requirements", "K/design", "K/spec"} {
		if _, ok := liveDocSuffix(kind); !ok {
			t.Errorf("%q should be snapshotted", kind)
		}
	}
	for _, kind := range []string{
		"K/rounds/R1-方案评审", "K/decisions/D1",
		"K/snapshots/spec/R1-方案评审", "K/notes",
	} {
		if _, ok := liveDocSuffix(kind); ok {
			t.Errorf("%q must not be snapshotted", kind)
		}
	}
}
