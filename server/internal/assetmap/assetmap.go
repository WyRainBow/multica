// Package assetmap renders the map of what a workspace has written down.
//
// The same map reaches an agent two ways: the daemon writes it into a
// dispatched task's brief, and a person's terminal session asks for it with
// `multica workspace context`. Those are different deliveries of one answer,
// and the two rendering it separately is how the same asset ends up described
// two ways — the shape of failure this repository hit three times in a week
// (a skill's description with two sources, workspace rules that reached one
// surface and not the other, instructions that existed but were never named).
//
// Only rendering and its inputs live here. Fetching does not: the daemon has
// the data already, loaded server-side and delivered with the claim, while the
// CLI queries the API for it. Sharing the fetch would force one of them
// through the wrong door. Keeping the package this small is also what lets the
// CLI import it without pulling the daemon in behind it.
package assetmap

import (
	"fmt"
	"strings"
)

// Doc names one document. Titles and ids only — a title states when a case
// applies, so a name is enough to decide whether to open it, and shipping
// bodies is what once cost 40% of a brief.
type Doc struct {
	ID    string
	Title string
	Kind  string
}

// Group is one folder's worth of documents, with the line that says when to
// reach for it.
type Group struct {
	Label string
	When  string
	Docs  []Doc
	// Dropped is how many the cap left out. Said out loud, because a truncated
	// list that hides it reads as the whole set and the reader stops looking
	// exactly where the older entries are.
	Dropped int
}

// ReadCommand is the invocation that opens one of these documents. Passed in
// rather than fixed so each surface can name the form its reader will actually
// run.
const ReadCommand = "multica wiki get <id> --output json"

// ComfortableIndexSize is where a names-only list stops working.
//
// Below it a reader scans the titles and picks; above it they are doing
// relevance matching by hand, which is the judgment this index deliberately
// refuses to make for them — and the point where a selection layer starts
// earning its cost. The number comes from the same comparison that said a
// twenty-item list needs no selection at all: intent-matching was built for
// catalogues in the hundreds.
//
// The warning matters more than the threshold. A list does not fail on a
// particular day; it gets gradually longer until somebody notices it has
// become noise, and by then it has been noise for a while.
const ComfortableIndexSize = 30

// RenderGroups writes the map. Intro is the surface's own framing; the body
// below it is identical everywhere, which is the point.
func RenderGroups(b *strings.Builder, intro string, groups []Group) {
	if intro != "" {
		b.WriteString(intro)
		b.WriteString("\n\n")
	}
	for _, group := range groups {
		if len(group.Docs) == 0 {
			// A heading with nothing under it claims the folder is empty
			// rather than absent, which is a different thing.
			continue
		}
		fmt.Fprintf(b, "**%s**", group.Label)
		if when := strings.TrimSpace(group.When); when != "" {
			fmt.Fprintf(b, " — %s", when)
		}
		b.WriteString("\n\n")
		for _, doc := range group.Docs {
			title := strings.TrimSpace(doc.Title)
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(b, "- %s — `%s`\n", title, doc.ID)
		}
		if group.Dropped > 0 {
			fmt.Fprintf(b, "- …%d more, list them with `multica wiki list --kind <folder>`\n", group.Dropped)
		}
		if total := len(group.Docs) + group.Dropped; total > ComfortableIndexSize {
			fmt.Fprintf(b, "\n> **本组 %d 条，已超出 names-only 的舒适区（%d）。** "+
				"再往上，读的人得自己在标题里做相关性匹配——那正是这份索引刻意不替他做的事。"+
				"考虑归并重复主题，或引入按需挑选层。\n", total, ComfortableIndexSize)
		}
		b.WriteString("\n")
	}
}

// HasAny reports whether any group carries a document, so a caller can skip
// its heading entirely rather than print one over nothing.
func HasAny(groups []Group) bool {
	for _, g := range groups {
		if len(g.Docs) > 0 {
			return true
		}
	}
	return false
}

// SourceOfTruthNotice closes any surface that hands out this map.
//
// The map is titles and prose, and prose about code goes stale without anyone
// editing it. A reader who takes a case as a statement about today's behaviour
// will be wrong eventually, so every delivery says where the truth is.
const SourceOfTruthNotice = "以上是快照与叙述，不是当前行为的保证。涉及实现与行为，以源码为准。"
