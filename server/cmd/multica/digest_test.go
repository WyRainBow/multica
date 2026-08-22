package main

import (
	"strings"
	"testing"
)

func sampleDigest(date string, items ...digestItem) digest {
	return digest{
		Date: date,
		Sections: []digestSection{
			{Label: "已合入但卡还开着", Do: "合了就把卡收掉", Items: items},
			{Label: "工作树异常"},
		},
	}
}

func TestASilentDayWritesNothing(t *testing.T) {
	t.Parallel()
	// The rule that keeps the slot worth reading. A year of "nothing to
	// report" trains a reader to skip it, and then the day it matters lands
	// somewhere that has been ignorable for months.
	d := sampleDigest("2026-08-22")
	if !d.Empty() {
		t.Fatal("a scan with no items must report itself empty")
	}
	if d.Render() != "" {
		t.Errorf("an empty scan rendered a body:\n%s", d.Render())
	}
	post, reason := postDecision(d, "")
	if post {
		t.Error("an empty scan must not be posted")
	}
	if !strings.Contains(reason, "没发现") {
		t.Errorf("the reason does not say the scan was clean: %q", reason)
	}
}

func TestADayThatReadsLikeYesterdayWritesNothing(t *testing.T) {
	t.Parallel()
	// Two identical findings a day apart. Without this the digest reposts the
	// same three stale worktrees every morning until they are as good as
	// invisible.
	yesterday := sampleDigest("2026-08-21", digestItem{Ref: "coc-300", Text: "COC-301 还开着"})
	today := sampleDigest("2026-08-22", digestItem{Ref: "coc-300", Text: "COC-301 还开着"})

	post, reason := postDecision(today, yesterday.Render())
	if post {
		t.Errorf("an unchanged scan was posted again; reason was %q", reason)
	}
	if !strings.Contains(reason, "相同") {
		t.Errorf("the reason does not say it was unchanged: %q", reason)
	}
}

func TestTheDateAloneDoesNotCountAsAChange(t *testing.T) {
	t.Parallel()
	// The trap this is guarding: fold the date into the fingerprint and the
	// suppression rule still compiles, still has tests, and never once
	// suppresses anything — present in the diff, absent in behaviour.
	same := digestItem{Ref: "coc-299", Text: "没人认领"}
	if sampleDigest("2026-08-21", same).Fingerprint() != sampleDigest("2026-08-22", same).Fingerprint() {
		t.Error("the fingerprint moved because the date moved")
	}
}

func TestAChangedFindingIsPosted(t *testing.T) {
	t.Parallel()
	yesterday := sampleDigest("2026-08-21", digestItem{Ref: "coc-300", Text: "COC-301 还开着"})
	today := sampleDigest("2026-08-22",
		digestItem{Ref: "coc-300", Text: "COC-301 还开着"},
		digestItem{Ref: "coc-312", Text: "COC-312 还开着"},
	)
	if post, reason := postDecision(today, yesterday.Render()); !post {
		t.Errorf("a new finding was suppressed: %q", reason)
	}
}

func TestAnUnreadablePreviousBodyErrsTowardPosting(t *testing.T) {
	t.Parallel()
	// A digest wrongly posted twice is noise; a digest wrongly withheld is the
	// exact failure this mechanism exists to prevent. So an unparseable
	// previous body must not be read as "unchanged".
	today := sampleDigest("2026-08-22", digestItem{Ref: "coc-300", Text: "COC-301 还开着"})
	for _, previous := range []string{
		"",
		"someone replied here by hand",
		"<!-- digest: with no terminator",
	} {
		if post, reason := postDecision(today, previous); !post {
			t.Errorf("previous body %q suppressed today's digest: %q", previous, reason)
		}
	}
}

func TestTheRenderedDigestCarriesItsOwnFingerprint(t *testing.T) {
	t.Parallel()
	// Comparing against the delivered body rather than against a record kept
	// elsewhere is what keeps the two from drifting apart.
	d := sampleDigest("2026-08-22", digestItem{Ref: "coc-300", Text: "COC-301 还开着"})
	if got := fingerprintOf(d.Render()); got != d.Fingerprint() {
		t.Errorf("fingerprint did not survive rendering: %q vs %q", got, d.Fingerprint())
	}
}

func TestSectionsThatFoundNothingAreNotRendered(t *testing.T) {
	t.Parallel()
	// "0 stale copies" is a claim about a check having run. This digest is
	// read for what needs doing, and a reassurance line costs the same
	// attention as a real finding.
	body := sampleDigest("2026-08-22", digestItem{Ref: "coc-300", Text: "COC-301 还开着"}).Render()
	if strings.Contains(body, "工作树异常") {
		t.Errorf("an empty section was rendered:\n%s", body)
	}
	if !strings.Contains(body, "已合入但卡还开着") {
		t.Errorf("the section with findings is missing:\n%s", body)
	}
	// The count belongs beside the label; a reader deciding whether to open
	// this should not have to count bullets.
	if !strings.Contains(body, "（1）") {
		t.Errorf("the section does not carry its count:\n%s", body)
	}
}

func TestAnItemWithNoRefStillRenders(t *testing.T) {
	t.Parallel()
	// The entropy warning has no id to act on — it is a fact about a whole
	// folder. Rendering it as "`` — 资产 32 条" would look like a bug.
	body := digest{
		Date:     "2026-08-22",
		Sections: []digestSection{{Label: "资产超阈", Items: []digestItem{{Text: "经验案例 32 条，超过 30"}}}},
	}.Render()
	if strings.Contains(body, "`` —") {
		t.Errorf("an item with no ref rendered an empty code span:\n%s", body)
	}
	if !strings.Contains(body, "经验案例 32 条") {
		t.Errorf("the item did not render:\n%s", body)
	}
}
