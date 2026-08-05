package service

import (
	"sort"
	"strings"
)

// DescriptionChangeSummary is the "what changed" recorded alongside a
// description_updated activity, so the timeline can say more than that
// something happened.
//
// Deliberately a summary, not a diff: the timeline is a scannable list, and
// storing full before/after text on every keystroke-debounced save would grow
// the activity log without bound.
type DescriptionChangeSummary struct {
	AddedLines   int `json:"added_lines"`
	RemovedLines int `json:"removed_lines"`
	// Headings whose content changed, in document order. Empty when the
	// description has no headings, or when only unsectioned text moved.
	Sections []string `json:"sections,omitempty"`
	// True when the whole description went from empty to written, or back.
	// The line counts alone read oddly in that case ("+40 −0" for the first
	// time anyone wrote anything).
	Created bool `json:"created,omitempty"`
	Cleared bool `json:"cleared,omitempty"`
	// The agent shell that ran the write, when one did — "claude-code" and so
	// on; empty when a person made the edit directly.
	//
	// Display only. The activity row's actor stays the member whose token was
	// used, because permissions, notifications and mentions all key off it;
	// this only changes the name the row shows. It is also self-reported by
	// the client and trivially spoofable, so it must never gate anything.
	Harness string `json:"harness,omitempty"`
}

// maxDiffCells bounds the LCS table. Beyond it the exact diff is abandoned for
// a multiset count — the numbers stay honest, they just stop accounting for
// moved lines. 4M cells is ~2000x2000 lines, far past any real description.
const maxDiffCells = 4_000_000

// SummarizeDescriptionChange reports what changed between two descriptions.
//
// Blank-only lines are ignored on both sides: a Markdown editor re-serializes
// spacing on every save, and counting that as a change would make every edit
// look bigger than it was.
func SummarizeDescriptionChange(before, after string) DescriptionChangeSummary {
	beforeLines := significantLines(before)
	afterLines := significantLines(after)

	summary := DescriptionChangeSummary{
		Created: len(beforeLines) == 0 && len(afterLines) > 0,
		Cleared: len(beforeLines) > 0 && len(afterLines) == 0,
	}
	summary.AddedLines, summary.RemovedLines = countLineChanges(beforeLines, afterLines)
	summary.Sections = changedSections(before, after)
	return summary
}

func significantLines(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func countLineChanges(before, after []string) (added, removed int) {
	if len(before)*len(after) > maxDiffCells {
		return countLineChangesByMultiset(before, after)
	}
	common := longestCommonSubsequenceLen(before, after)
	return len(after) - common, len(before) - common
}

// countLineChangesByMultiset is the fallback for documents too large to diff
// exactly. It undercounts a moved line (it appears on both sides, so it is
// reported as unchanged) — acceptable for a summary, and it never overstates.
func countLineChangesByMultiset(before, after []string) (added, removed int) {
	counts := make(map[string]int, len(before))
	for _, line := range before {
		counts[line]++
	}
	for _, line := range after {
		if counts[line] > 0 {
			counts[line]--
			continue
		}
		added++
	}
	for _, remaining := range counts {
		removed += remaining
	}
	return added, removed
}

func longestCommonSubsequenceLen(a, b []string) int {
	// Two rows instead of the full table: only the previous row is ever read.
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] >= curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// sectionBodies splits Markdown into heading -> body text. Content before the
// first heading lives under the empty key, which is never reported as a
// changed section because it has no name to show.
func sectionBodies(text string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if heading, ok := parseHeading(trimmed); ok {
			current = heading
			// Register the heading even with an empty body, so adding an empty
			// section still counts as a change.
			if _, exists := sections[current]; !exists {
				sections[current] = []string{}
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		sections[current] = append(sections[current], trimmed)
	}
	return sections
}

func parseHeading(trimmed string) (string, bool) {
	if !strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	// `#####...` past level 6 is not a heading, and `#foo` needs the space.
	if level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return "", false
	}
	name := strings.TrimSpace(trimmed[level:])
	if name == "" {
		return "", false
	}
	return name, true
}

// changedSections names the headings whose content differs, in the order they
// appear in the NEW text — that is the document the reader is looking at.
// Sections that only exist in the old text follow, so a deleted section is
// still reported.
func changedSections(before, after string) []string {
	beforeSections := sectionBodies(before)
	afterSections := sectionBodies(after)

	changed := map[string]bool{}
	for name, body := range afterSections {
		if name == "" {
			continue
		}
		old, existed := beforeSections[name]
		if !existed || !equalLines(old, body) {
			changed[name] = true
		}
	}
	for name := range beforeSections {
		if name == "" {
			continue
		}
		if _, stillThere := afterSections[name]; !stillThere {
			changed[name] = true
		}
	}
	if len(changed) == 0 {
		return nil
	}

	ordered := make([]string, 0, len(changed))
	for _, name := range headingOrder(after) {
		if changed[name] {
			ordered = append(ordered, name)
			delete(changed, name)
		}
	}
	remaining := make([]string, 0, len(changed))
	for name := range changed {
		remaining = append(remaining, name)
	}
	// Deleted sections have no position in the new document; sort them so the
	// output does not depend on map iteration order.
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

func headingOrder(text string) []string {
	var order []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if name, ok := parseHeading(strings.TrimSpace(line)); ok && !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	return order
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
