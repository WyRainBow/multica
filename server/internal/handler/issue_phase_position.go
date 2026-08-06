package handler

import (
	"regexp"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// roundSuffixRe matches the number a round carries: "评审 2" → root "评审".
// Anchored to the end and requiring whitespace, so "V2 结论" is not mistaken
// for a round of "V2".
var roundSuffixRe = regexp.MustCompile(`\s+\d+$`)

// phaseRoundRoot returns the station a name is a round OF, or "" when the name
// is not a numbered round. Mirrors nextRoundName in phase-round.ts, which
// produces these names on the client.
func phaseRoundRoot(name string) string {
	trimmed := strings.TrimSpace(name)
	root := strings.TrimSpace(roundSuffixRe.ReplaceAllString(trimmed, ""))
	if root == "" || root == trimmed {
		return ""
	}
	return root
}

// isRoundOf reports whether phaseName is root itself or a numbered round of it.
func isRoundOf(phaseName, root string) bool {
	trimmed := strings.TrimSpace(phaseName)
	if strings.EqualFold(trimmed, root) {
		return true
	}
	stripped := strings.TrimSpace(roundSuffixRe.ReplaceAllString(trimmed, ""))
	return stripped != trimmed && strings.EqualFold(stripped, root)
}

// nextPhasePosition decides where a new station lands when the caller did not
// name a position.
//
// Appending is right for a genuinely new station, and wrong for another ROUND
// of one that already exists: 评审 2 appended to 开始 / 评审 / 冻结 reads as a
// review that happens after the issue was frozen. A round belongs beside the
// station it repeats, so it goes immediately after the last existing round of
// the same root.
//
// phases must be in track order (ListIssuePhases sorts by position), which is
// what makes "the phase after the last sibling" meaningful.
//
// The midpoint is what phasePositionStep exists for — inserting between two
// neighbours without renumbering them. When two neighbours are already
// adjacent there is no room left, and the round appends rather than colliding
// on a duplicate position: a route that deep is past the point where one more
// gap would have helped.
func nextPhasePosition(phases []db.IssuePhase, name string) int32 {
	// One step past the furthest station. Zero on an empty route, which lines
	// the first station up with the 0 / 1000 / 2000 a seeded route uses.
	appendAt := int32(0)
	for _, phase := range phases {
		if next := phase.Position + phasePositionStep; next > appendAt {
			appendAt = next
		}
	}

	root := phaseRoundRoot(name)
	if root == "" {
		return appendAt
	}

	lastSibling := -1
	for i, phase := range phases {
		if isRoundOf(phase.Name, root) {
			lastSibling = i
		}
	}
	if lastSibling < 0 || lastSibling == len(phases)-1 {
		// No station to sit beside, or the siblings already end the route.
		return appendAt
	}

	prev := phases[lastSibling].Position
	next := phases[lastSibling+1].Position
	if next-prev < 2 {
		return appendAt
	}
	return prev + (next-prev)/2
}
