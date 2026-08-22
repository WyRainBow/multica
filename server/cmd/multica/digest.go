package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// The daily patrol, and the one thing it must not become.
//
// Five mechanisms in this workspace wait to be pulled: a branch that landed
// while its cards stayed open, a checkout with uncommitted work, a local copy
// of the workspace rules that went stale, a machine-written retro nobody has
// read, an asset index that grew past the size a person can scan. Each was
// built with a way to ask, and nothing asks. `worktree ready` has to be run.
// `instructions status` returns an exit code for a shell hook nothing
// installs. The entropy warning lives inside the output of a command you have
// to remember to run — a guard against not noticing that itself needs
// noticing.
//
// So this is one scan a day that says what changed, delivered instead of
// offered. The design constraint that matters more than the content: it stays
// silent when there is nothing to say, and silent again when today reads
// exactly like yesterday. A year of "nothing to report" trains a reader to
// skip it, and then a digest that finally has something to say arrives in a
// slot that has been ignorable for months.

// digestItem is one thing worth a line. Ref is what the reader acts on — a
// tree name, a card key, a path — and is kept separate from the prose so the
// same item can be compared across days without the wording deciding it.
type digestItem struct {
	Ref  string
	Text string
}

// digestSection is one category of finding, with the line saying what to do.
type digestSection struct {
	Label string
	Do    string
	Items []digestItem
}

// digest is a day's scan.
type digest struct {
	Date     string
	Sections []digestSection
}

// nonEmpty returns the sections that found something. A section with no items
// is dropped rather than rendered empty: "0 stale copies" is a claim about a
// check having run, and this digest is read for what needs doing, not for
// reassurance.
func (d digest) nonEmpty() []digestSection {
	var out []digestSection
	for _, s := range d.Sections {
		if len(s.Items) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// Empty reports whether the whole scan found nothing.
func (d digest) Empty() bool { return len(d.nonEmpty()) == 0 }

// Fingerprint identifies what the scan found, deliberately excluding the date.
//
// This is the whole of "silent when unchanged". Folding the date in would make
// every day differ from the day before, and the suppression rule would be code
// that never once suppressed anything — the kind of feature that looks present
// in a diff and is absent in behaviour.
func (d digest) Fingerprint() string {
	var b strings.Builder
	for _, s := range d.nonEmpty() {
		b.WriteString(s.Label)
		b.WriteString("\n")
		for _, item := range s.Items {
			b.WriteString(item.Ref)
			b.WriteString("\x1f")
			b.WriteString(item.Text)
			b.WriteString("\n")
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// fingerprintMarker is how a posted digest carries its own fingerprint, so the
// next day can compare against what was actually delivered rather than against
// a record kept somewhere else that can drift from it.
const fingerprintMarker = "<!-- digest:"

// Render writes the comment body. The fingerprint rides in an HTML comment:
// inert in every Markdown renderer, and read back by the next run.
func (d digest) Render() string {
	sections := d.nonEmpty()
	if len(sections) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s -->\n\n", fingerprintMarker, d.Fingerprint())
	fmt.Fprintf(&b, "## %s 巡逻\n\n", d.Date)
	b.WriteString("> 这些机制本来都要人主动去跑才会说话。这条是每天替你跑一遍的结果，没事的日子不写。\n\n")
	for _, s := range sections {
		fmt.Fprintf(&b, "**%s**（%d）\n\n", s.Label, len(s.Items))
		for _, item := range s.Items {
			if strings.TrimSpace(item.Ref) == "" {
				fmt.Fprintf(&b, "- %s\n", item.Text)
				continue
			}
			fmt.Fprintf(&b, "- `%s` — %s\n", item.Ref, item.Text)
		}
		if do := strings.TrimSpace(s.Do); do != "" {
			fmt.Fprintf(&b, "\n  %s\n", do)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// fingerprintOf reads the marker back out of a previously posted body.
// Anything it cannot parse reads as "no previous digest", which errs toward
// posting: a duplicate is noise, a silently dropped digest is the failure this
// whole thing exists to prevent.
func fingerprintOf(body string) string {
	start := strings.Index(body, fingerprintMarker)
	if start < 0 {
		return ""
	}
	rest := body[start+len(fingerprintMarker):]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// postDecision says whether today's scan is worth a comment, and why not when
// it is not. The reason is returned rather than logged so the caller can print
// it: a command that exits 0 having written nothing should say which of the
// two silences this was.
func postDecision(d digest, lastPosted string) (post bool, reason string) {
	switch {
	case d.Empty():
		return false, "巡逻没发现要处理的东西，本次不写。"
	case fingerprintOf(lastPosted) == d.Fingerprint():
		return false, "与上次写的完全相同，本次不写。"
	default:
		return true, ""
	}
}
