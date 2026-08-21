package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The main spec of an issue: what is currently true, as opposed to what was
// decided in any one round.
//
// It is derived, never typed. A hand-maintained "current conclusions" section
// is a copy of facts that live somewhere else, and a copy stops being updated
// the first time someone is in a hurry — after which it is worse than absent,
// because people still believe it. So closing a round rewrites this section
// from the round documents themselves, and the round documents are write-once.
//
// Only the marked section is rewritten. Everything a human put in the spec —
// the goal, the measurement, the constraints — is prose the closure action has
// no business touching.

const (
	specSectionOpen  = "<!-- rounds:begin -->"
	specSectionClose = "<!-- rounds:end -->"
)

// RoundDoc is one closed round, as the spec needs to see it.
type RoundDoc struct {
	// Sequence within the issue, taken from the document's kind.
	Number int
	// The review station it closed — "代码评审", "方案评审", …
	Phase string
	// approve / request_changes / block, or whatever the closer recorded.
	Verdict string
	// One line: what was decided.
	Summary string
	// The commit the round's verdict was actually checked against. Distinct
	// from the merge SHA: a round can approve a version that never lands, and
	// a version can land without anyone re-running the checks.
	VerifiedSHA string
	// What the checks said — "tests 4044/4044", "手工验收通过". A verdict with
	// no evidence beside it is an opinion.
	Evidence string
	// The document's own id, so the spec can point at the full text.
	DocID string
}

var roundKindRe = regexp.MustCompile(`^R(\d+)-(.+)$`)

// ParseRoundKind reads the round number and station out of a round document's
// last kind segment: "R2-代码评审" → 2, "代码评审".
func ParseRoundKind(segment string) (number int, phase string, ok bool) {
	m := roundKindRe.FindStringSubmatch(strings.TrimSpace(segment))
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, "", false
	}
	return n, m[2], true
}

// NextRoundNumber is one past the highest round already closed. Numbering from
// the documents rather than from a counter means a round cannot be closed twice
// under the same number, and a missing document is visible as a gap.
// NextRoundNumber returns the next round number AT ONE STATION.
//
// Rounds belong to the station they were argued at, not to the issue. Counting
// across the whole card made an issue that closed one round at 方案评审 print
// its first 代码评审 round as "R2", which reads as a second attempt at code
// review and sends the next reader looking for an R1 that was never held.
//
// Rounds recorded at other stations are ignored rather than skipped over, so
// each station's sequence starts at 1 and stays readable on its own.
func NextRoundNumber(rounds []RoundDoc, phase string) int {
	phase = strings.TrimSpace(phase)
	highest := 0
	for _, r := range rounds {
		if strings.TrimSpace(r.Phase) != phase {
			continue
		}
		if r.Number > highest {
			highest = r.Number
		}
	}
	return highest + 1
}

// RenderRoundSection builds the block the spec carries. Newest first: the
// question it answers is "where does this stand", and the answer is the last
// line written, not the first.
func RenderRoundSection(rounds []RoundDoc, watermark string) string {
	sorted := append([]RoundDoc(nil), rounds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Number > sorted[j].Number })

	var b strings.Builder
	b.WriteString(specSectionOpen)
	b.WriteString("\n\n## 轮次结论\n\n")
	if len(sorted) == 0 {
		b.WriteString("尚无收口轮次。\n")
	} else {
		b.WriteString("> 本节由 `multica issue round close` 重写，不要手改——手改会在下次收口时丢失。\n\n")
		// The watermark is what makes "is this still current?" answerable.
		// Without it a reader can see the conclusions but not whether anything
		// was argued after them, so the only safe move is re-reading every
		// comment — the cost this section exists to remove.
		if strings.TrimSpace(watermark) != "" {
			fmt.Fprintf(&b, "> **结论计入截至 %s。** 该时刻之后的评论尚未计入本表——只读那之后的，不必通读全部。\n\n", strings.TrimSpace(watermark))
		}
		b.WriteString("| 轮次 | 节点 | 结论 | 要点 | 验收版本 | 验证证据 | 正身 |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
		for _, r := range sorted {
			b.WriteString(fmt.Sprintf("| R%d | %s | %s | %s | %s | %s | %s |\n",
				r.Number,
				cellEscape(r.Phase),
				cellEscape(r.Verdict),
				cellEscape(r.Summary),
				cellEscape(shortSHA(r.VerifiedSHA)),
				cellEscape(r.Evidence),
				cellEscape(r.DocID),
			))
		}
	}
	b.WriteString("\n")
	b.WriteString(specSectionClose)
	return b.String()
}

// cellEscape keeps a pipe in a summary from splitting the row it is written in.
func cellEscape(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return "—"
	}
	return s
}

// ApplyRoundSection replaces the marked block in a spec, or appends one when
// the spec has never carried it.
//
// Anything outside the markers is returned untouched, including a spec that is
// empty: the closure action owns one section, not the document.
func ApplyRoundSection(spec string, rounds []RoundDoc, watermark string) string {
	section := RenderRoundSection(rounds, watermark)

	start := strings.Index(spec, specSectionOpen)
	end := strings.LastIndex(spec, specSectionClose)
	if start >= 0 && end > start {
		return spec[:start] + section + spec[end+len(specSectionClose):]
	}
	// One marker without the other: the pair was edited by hand. Appending
	// would leave the spec claiming two sets of current conclusions, so
	// everything from the surviving marker onward is replaced — that text is
	// inside the managed section by the only definition available.
	if start >= 0 {
		return spec[:start] + section + "\n"
	}
	if end >= 0 {
		return section + spec[end+len(specSectionClose):]
	}

	trimmed := strings.TrimRight(spec, "\n")
	if trimmed == "" {
		return section + "\n"
	}
	return trimmed + "\n\n" + section + "\n"
}
