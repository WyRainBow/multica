package handler

import (
	"context"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Done requires that every review round a card opened was also closed.
//
// A round that is argued and never closed leaves its verdict in the thread it
// was argued in: no round document, and a spec whose "current conclusions"
// stopped at the round before. The card then reads as finished while the last
// thing anyone decided about it is buried in comments — which is the failure
// the closure action exists to end, and a discipline nobody enforces is one
// that holds until the first hurried afternoon.
//
// Scoped to cards that opened a review station at all. Most cards never do,
// and a gate that fires on them is a gate people learn to work around.

// reviewPhasePrefixes name the stations whose rounds have to be closed. A
// station is a review when it says so — the vocabulary is the user's, not a
// database enum, so this matches rather than compares.
var reviewPhasePrefixes = []string{"方案评审", "代码评审", "测试验收"}

func isReviewPhase(name string) bool {
	trimmed := strings.TrimSpace(name)
	for _, prefix := range reviewPhasePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// roundDocKindFor is where `issue round close` files a round: <KEY>/rounds/R<N>-<station>.
func roundDocKindFor(identifier string) string {
	return identifier + "/rounds/"
}

// checkRoundClosureForDone reports why done is refused, if it is.
//
// It asks one question — did every review station this card opened produce a
// round document — because the round document is the only artefact whose
// existence proves the round was closed rather than merely discussed.
func (h *Handler) checkRoundClosureForDone(
	ctx context.Context,
	issue db.Issue,
	identifier string,
) (string, bool) {
	phases, err := h.Queries.ListIssuePhases(ctx, db.ListIssuePhasesParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		// A gate that cannot read its own inputs must not invent a verdict:
		// blocking here would make an unrelated database problem look like a
		// process violation.
		return "", false
	}

	var openedReviews []string
	for _, phase := range phases {
		if isReviewPhase(phase.Name) {
			openedReviews = append(openedReviews, strings.TrimSpace(phase.Name))
		}
	}
	if len(openedReviews) == 0 {
		return "", false
	}

	cards, err := h.Queries.ListCardsForIssue(ctx, db.ListCardsForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		return "", false
	}

	prefix := roundDocKindFor(identifier)
	closed := map[string]bool{}
	for _, card := range cards {
		kind := strings.TrimSpace(card.Kind)
		if !strings.HasPrefix(kind, prefix) {
			continue
		}
		// "R2-代码评审" → the station it closed. Everything after the first
		// dash is the name, which is how the closure action wrote it.
		segment := strings.TrimPrefix(kind, prefix)
		if _, station, ok := parseRoundSegment(segment); ok {
			closed[station] = true
		}
	}

	var missing []string
	for _, station := range openedReviews {
		if !closed[station] {
			missing = append(missing, station)
		}
	}
	if len(missing) == 0 {
		return "", false
	}

	return "issue opened review stations that were never closed (" +
		strings.Join(missing, ", ") + "). A round that is argued and not closed leaves " +
		"its verdict in the thread and the spec one round behind. Run " +
		"`multica issue round close <key> --phase <station> --verdict ... --summary ...` " +
		"for each, then retry done.", true
}

// parseRoundSegment reads "R2-代码评审" the way the CLI writes it. Kept beside
// the gate rather than shared with the CLI: the two live in different binaries
// and a shared constant would tie a server release to a CLI one.
func parseRoundSegment(segment string) (number string, station string, ok bool) {
	segment = strings.TrimSpace(strings.TrimSuffix(segment, "/"))
	if !strings.HasPrefix(segment, "R") {
		return "", "", false
	}
	rest := segment[1:]
	dash := strings.Index(rest, "-")
	if dash <= 0 || dash == len(rest)-1 {
		return "", "", false
	}
	number = rest[:dash]
	for _, r := range number {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}
	return number, rest[dash+1:], true
}
