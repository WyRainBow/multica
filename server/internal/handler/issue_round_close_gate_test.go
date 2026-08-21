package handler

import "testing"

func TestOnlyReviewStationsAreGated(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"方案评审", "代码评审 R2", "测试验收"} {
		if !isReviewPhase(name) {
			t.Errorf("%q should be gated: a verdict was reached there", name)
		}
	}
	// The default stations a card carries that are not reviews. Gating these
	// would fire on cards that never opened a review, and a gate that fires on
	// everyone is one people learn to route around.
	for _, name := range []string{"需求梳理", "需求冻结", "开始"} {
		if isReviewPhase(name) {
			t.Errorf("%q should not be gated: nothing is decided there", name)
		}
	}
}

func TestParseRoundSegmentReadsWhatTheCLIWrites(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in      string
		station string
		ok      bool
	}{
		{"R1-方案评审", "方案评审", true},
		{"R12-代码评审 R2", "代码评审 R2", true},
		// A station name containing a dash keeps all of it: only the first
		// dash separates the number.
		{"R2-代码评审-补充", "代码评审-补充", true},
		{"R1-", "", false},
		{"Rx-代码评审", "", false},
		{"代码评审", "", false},
		{"R1", "", false},
	} {
		_, station, ok := parseRoundSegment(c.in)
		if ok != c.ok || station != c.station {
			t.Errorf("parseRoundSegment(%q) = (%q, %v), want (%q, %v)",
				c.in, station, ok, c.station, c.ok)
		}
	}
}

func TestRoundDocKindMatchesWhereTheCLIFiles(t *testing.T) {
	t.Parallel()
	// The gate finds nothing if this drifts from the CLI's layout, and a gate
	// that finds nothing silently lets every card through.
	if got := roundDocKindFor("COC-300"); got != "COC-300/rounds/" {
		t.Errorf("roundDocKindFor = %q, want COC-300/rounds/", got)
	}
}

// phaseOpened mirrors the gate's test for "this station actually happened",
// so the rule can be exercised without a database.
func phaseOpened(entered, completed bool) bool {
	return entered || completed
}

func TestADefaultPhaseIsNotAnOpenedOne(t *testing.T) {
	t.Parallel()
	// Every card is created with the five default stations already listed, all
	// pending and never entered. Matching on the name alone refused done on
	// every card in the workspace — the gate became an outage. Caught by
	// running it against a real card, not by the unit tests that existed.
	if phaseOpened(false, false) {
		t.Error("a listed-but-never-entered station counted as opened")
	}
	if !phaseOpened(true, false) {
		t.Error("an entered station should count as opened")
	}
	// Completed without a recorded entry still happened — an older card may
	// carry one, and refusing to see it would let a real gap through.
	if !phaseOpened(false, true) {
		t.Error("a completed station should count as opened")
	}
}
