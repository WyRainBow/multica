package handler

import "strings"

// An issue's documents are not peers. Exactly one of them — the spec — states
// where the issue stands now; the rounds and decisions beside it record how it
// got there. Listing them flat made an agent read all of them, or none, and
// gave it no reason to prefer the one that answers the question it has.
//
// The distinction is carried on the wire so the brief can render it, and it is
// decided here rather than in the brief writer because the same rule governs
// which document's body travels.

// specKindSuffix is where `multica issue round close` files the live spec:
// <ISSUE-KEY>/spec. Kept as a suffix match rather than a full path because the
// key varies per issue and per workspace prefix.
const specKindSuffix = "/spec"

// isCurrentIssueDoc reports whether a document kind names the issue's state of
// record. Suffix-anchored so a folder that merely contains the word — say
// "<KEY>/rounds/R1-spec-review" — is not mistaken for the spec itself.
func isCurrentIssueDoc(kind string) bool {
	trimmed := strings.TrimSuffix(strings.TrimSpace(kind), "/")
	return strings.HasSuffix(trimmed, specKindSuffix)
}

// Markers written by `multica issue round close` around the derived section of
// a spec. Duplicated here rather than shared with the CLI: the two live in
// different binaries, and a shared constant would tie a server release to a
// CLI one for a string neither side may change alone.
const (
	specRoundsOpen  = "<!-- rounds:begin -->"
	specRoundsClose = "<!-- rounds:end -->"
)

// specConclusionsBriefLimit bounds what travels. The conclusions table is one
// row per closed round and stays small in practice, but it is paid for on
// every turn of every run, so an issue that reviewed twenty times must not
// quietly double the brief.
const specConclusionsBriefLimit = 2000

// extractSpecConclusions returns the spec's derived conclusions section.
//
// Only the managed region travels. The rest of a spec is hand-written — goals,
// scope, acceptance — and belongs to whoever wrote it; shipping all of it would
// turn "read the conclusions" into "read the whole document" on every turn.
//
// Returns empty when the section is absent or malformed, which is the honest
// answer for a spec nobody has closed a round on: the brief then says the issue
// has no recorded conclusions rather than implying it has none at all.
func extractSpecConclusions(content string) string {
	start := strings.Index(content, specRoundsOpen)
	if start < 0 {
		return ""
	}
	rest := content[start+len(specRoundsOpen):]
	end := strings.Index(rest, specRoundsClose)
	if end < 0 {
		// An opening marker with no close means the spec was hand-edited.
		// Guessing where the section ends could ship the rest of the document.
		return ""
	}
	section := strings.TrimSpace(rest[:end])
	if section == "" {
		return ""
	}
	runes := []rune(section)
	if len(runes) <= specConclusionsBriefLimit {
		return section
	}
	// Truncation that does not admit itself reads as the whole set, and the
	// agent stops looking exactly where the older rounds are.
	return string(runes[:specConclusionsBriefLimit]) +
		"\n\n…truncated; read the full spec for the earlier rounds."
}
