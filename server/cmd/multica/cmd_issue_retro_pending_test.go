package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/assetmap"
)

// Both filters decide whether an agent run gets dispatched, so getting either
// wrong costs real money in one direction and a silently missing retro in the
// other.

func TestACardThatAlreadyCarriesARetroIsSkipped(t *testing.T) {
	t.Parallel()
	// The simple dedupe: whatever the existing artefact says, this card is
	// done. A machine that re-judged its quality would rewrite the same card
	// every night, and the drafts folder would fill with versions of one
	// retro nobody asked for twice.
	for _, kind := range []string{
		assetmap.CaseDraftKind + "/2026-08-22-一次真实的坑",
		assetmap.CaseDraftKind,
		"AgentWiki/cases_案例/2026-08-22-已经审过了",
	} {
		if !hasRetroArtefact([]docRow{{Kind: "COC-9/spec"}, {Kind: kind}}) {
			t.Errorf("%q was not recognised as a retro artefact", kind)
		}
	}
	// A promoted draft still counts. Promotion is a kind change, and a card
	// whose draft graduated must not become pending again the next night.
	if !hasRetroArtefact([]docRow{{Kind: "AgentWiki/cases_案例/promoted"}}) {
		t.Error("a promoted retro stopped counting as one")
	}
	if hasRetroArtefact([]docRow{{Kind: "COC-9/spec"}, {Kind: "COC-9/rounds/R1-代码评审"}}) {
		t.Error("ordinary issue documents were mistaken for a retro")
	}
}

func TestACardWithNothingToReadIsNotDispatched(t *testing.T) {
	t.Parallel()
	// The filter that keeps this from being expensive noise. This workspace
	// closed 58 cards in a week; almost none went through a review round, and
	// a retro against a card with no ledger and no rounds writes a page that
	// says nothing — which someone then has to read and delete.
	if got := retroEvidence(nil, 0); got != "" {
		t.Errorf("a card with no record qualified: %q", got)
	}
	// A spec alone is not a run. A card can be specced and never worked.
	if got := retroEvidence([]docRow{{Kind: "COC-9/spec"}}, 0); got != "" {
		t.Errorf("a spec with no run behind it qualified: %q", got)
	}
	if got := retroEvidence([]docRow{{Kind: "COC-9/decisions/D1"}}, 0); got != "" {
		t.Errorf("a decision card with no run behind it qualified: %q", got)
	}
}

func TestACardWithARunSaysWhatTheRetroWouldRead(t *testing.T) {
	t.Parallel()
	// The reason travels with the card. A list of keys with no reason beside
	// them is a list a reader has to re-derive before trusting.
	rounds := retroEvidence([]docRow{
		{Kind: "COC-9/rounds/R1-代码评审"},
		{Kind: "COC-9/rounds/R2-代码评审"},
		{Kind: "COC-9/spec"},
	}, 0)
	if !strings.Contains(rounds, "2 轮次文档") {
		t.Errorf("round documents were not counted: %q", rounds)
	}
	if !strings.Contains(rounds, "1 份其它文档") {
		t.Errorf("the spec was not mentioned alongside: %q", rounds)
	}

	// A ledger on its own is enough: plenty of work leaves entries and never
	// closes a formal round.
	ledger := retroEvidence(nil, 6)
	if !strings.Contains(ledger, "6 条工作树记录") {
		t.Errorf("ledger history alone did not qualify: %q", ledger)
	}
}

func TestARoundDocumentIsRecognisedByItsLastSegment(t *testing.T) {
	t.Parallel()
	// Kinds are paths. Matching the whole string against a round pattern
	// would classify every round document as "other", and a card with four
	// rounds would read as having no rounds at all.
	if lastKindSegment("COC-311/rounds/R4-代码评审") != "R4-代码评审" {
		t.Errorf("last segment = %q", lastKindSegment("COC-311/rounds/R4-代码评审"))
	}
	if lastKindSegment("spec") != "spec" {
		t.Error("a kind with no slash lost its only segment")
	}
}
