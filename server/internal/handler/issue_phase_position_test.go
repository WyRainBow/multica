package handler

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// 评审 2 appended to 开始 / 评审 / 冻结 reads as a review that happens after
// the issue was frozen. A round belongs beside the station it repeats.

func route(names []string, positions []int32) []db.IssuePhase {
	phases := make([]db.IssuePhase, len(names))
	for i := range names {
		phases[i] = db.IssuePhase{Name: names[i], Position: positions[i]}
	}
	return phases
}

func defaultRoute() []db.IssuePhase {
	return route([]string{"开始", "评审", "冻结"}, []int32{0, 1000, 2000})
}

func TestNextPhasePosition_RoundLandsBesideItsBase(t *testing.T) {
	got := nextPhasePosition(defaultRoute(), "评审 2")
	if got != 1500 {
		t.Fatalf("评审 2 position = %d, want 1500 (between 评审 and 冻结)", got)
	}
}

// Round three sits after round two, not after the original — the sequence has
// to stay readable left to right.
func TestNextPhasePosition_LaterRoundsStack(t *testing.T) {
	phases := route([]string{"开始", "评审", "评审 2", "冻结"}, []int32{0, 1000, 1500, 2000})
	got := nextPhasePosition(phases, "评审 3")
	if got != 1750 {
		t.Fatalf("评审 3 position = %d, want 1750 (between 评审 2 and 冻结)", got)
	}
}

// A genuinely new station is not a round of anything and still appends.
func TestNextPhasePosition_NewStationAppends(t *testing.T) {
	got := nextPhasePosition(defaultRoute(), "实施")
	if got != 3000 {
		t.Fatalf("实施 position = %d, want 3000 (end of the route)", got)
	}
}

// The first station on an empty issue starts the sequence.
func TestNextPhasePosition_FirstStationStartsAtZero(t *testing.T) {
	if got := nextPhasePosition(nil, "开始"); got != 0 {
		t.Fatalf("first phase position = %d, want 0", got)
	}
}

// Nothing to sit beside: a round of a station this issue does not have is just
// a new station.
func TestNextPhasePosition_RoundWithNoBaseAppends(t *testing.T) {
	got := nextPhasePosition(defaultRoute(), "测试 2")
	if got != 3000 {
		t.Fatalf("测试 2 position = %d, want 3000 (append; no 测试 to sit beside)", got)
	}
}

// When the base already ends the route there is nothing to insert before, and
// appending is the same answer.
func TestNextPhasePosition_RoundOfTheLastStationAppends(t *testing.T) {
	got := nextPhasePosition(defaultRoute(), "冻结 2")
	if got != 3000 {
		t.Fatalf("冻结 2 position = %d, want 3000", got)
	}
}

// Two neighbours with no gap left. Appending is wrong-looking but honest; a
// midpoint would collide on a duplicate position, which the unique ordering
// cannot express at all.
func TestNextPhasePosition_NoRoomFallsBackToAppending(t *testing.T) {
	phases := route([]string{"开始", "评审", "冻结"}, []int32{0, 1000, 1001})
	got := nextPhasePosition(phases, "评审 2")
	if got != 2001 {
		t.Fatalf("position = %d, want 2001 (append; no gap between 1000 and 1001)", got)
	}
}

// A number that is part of the name, not a round of anything: "V2 结论" must
// not be read as round 2 of "V".
func TestPhaseRoundRoot_OnlyATrailingNumberCounts(t *testing.T) {
	cases := map[string]string{
		"评审 2":    "评审",
		"评审 10":   "评审",
		"评审":      "",
		"V2 结论":   "",
		"2":       "",
		"  评审 3 ": "评审",
	}
	for name, want := range cases {
		if got := phaseRoundRoot(name); got != want {
			t.Fatalf("phaseRoundRoot(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIsRoundOf(t *testing.T) {
	for _, tc := range []struct {
		phase, root string
		want        bool
	}{
		{"评审", "评审", true},
		{"评审 2", "评审", true},
		{"评审 12", "评审", true},
		{"冻结", "评审", false},
		{"评审前", "评审", false},
		{"V2 结论", "V", false},
	} {
		if got := isRoundOf(tc.phase, tc.root); got != tc.want {
			t.Fatalf("isRoundOf(%q, %q) = %v, want %v", tc.phase, tc.root, got, tc.want)
		}
	}
}
