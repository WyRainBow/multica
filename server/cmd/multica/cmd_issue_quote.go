package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// A quote addresses a span of an issue description by quoting its EDGES rather
// than its whole text: the first few words, the last few words, and — when
// those words appear more than once — the text immediately around them.
//
// Edges, because the point of a handle is compression. Quoting a passage in
// order to locate it defeats itself: a 1000-character selection would need a
// 1000-character handle. Edges stay short however long the span is, which is
// what lets the editor generate a handle someone can paste into a chat.
//
// Prefix/suffix rather than an occurrence number: a reader can see the text
// around a passage, but cannot see how many times the same phrase appeared
// earlier in a document they have not read. `comment add` keeps
// --anchor-occurrence because a comment anchor is a single point; a span has
// two edges, and counting both would be worse than describing either.
//
// The shape is W3C's TextQuoteSelector (exact/prefix/suffix) widened to a span.
// The URL form of the same idea — Text Fragments, #:~:text=start,end — is
// deliberately NOT copied: packing four strings into one slot forces
// percent-encoding of "," and "-", and a half-width comma is ordinary in
// Chinese prose. A CLI has flags and needs no escaping.
type quoteSpec struct {
	Start  string
	End    string
	Prefix string
	Suffix string
}

// quoteSpan is a located span in CHARACTER offsets — the coordinate system
// anchorOffsetInText already uses, and for the same reason: a byte offset lands
// mid-character on any CJK description.
type quoteSpan struct {
	Start int
	End   int
	Text  string
}

// locateQuote resolves a spec against a description.
//
// It never guesses. Zero matches and several matches are both errors, because
// the caller is an agent about to review whatever comes back: returning the
// first of four candidates would produce a confident review of the wrong
// passage, and nothing downstream could tell.
func locateQuote(text string, spec quoteSpec) (quoteSpan, error) {
	return locateSpan(text, spec, quoteFlagNames)
}

// spanFlagNames carries the flag names — and the noun for the searched text —
// that locateSpan's errors speak in, so every command's errors point at flags
// it actually has: `issue get` says --quote-start, `comment edit` says
// --replace-start, and no remedy tells the caller to fix a flag their command
// does not carry.
type spanFlagNames struct {
	Start  string
	End    string
	Prefix string
	Suffix string
	// Noun is what the searched text is called in errors — "description" for
	// an issue, "comment" for a comment.
	Noun string
	// ContextFlags is true when the command exposes the --*-prefix/--*-suffix
	// flags. When it does not, a longer anchor is the only fix for ambiguity
	// left, and the error must not offer anything else.
	ContextFlags bool
}

var quoteFlagNames = spanFlagNames{
	Start:        "quote-start",
	End:          "quote-end",
	Prefix:       "quote-prefix",
	Suffix:       "quote-suffix",
	Noun:         "description",
	ContextFlags: true,
}

// locateSpan is locateQuote with the error vocabulary supplied by the caller.
// The matching rules and the no-guessing contract are locateQuote's.
func locateSpan(text string, spec quoteSpec, names spanFlagNames) (quoteSpan, error) {
	start := strings.TrimSpace(spec.Start)
	if start == "" {
		return quoteSpan{}, fmt.Errorf("--%s must not be blank", names.Start)
	}
	end := strings.TrimSpace(spec.End)
	prefix := strings.TrimSpace(spec.Prefix)
	suffix := strings.TrimSpace(spec.Suffix)

	haystack := []rune(text)
	startRunes := []rune(start)
	endRunes := []rune(end)
	prefixRunes := []rune(prefix)
	suffixRunes := []rune(suffix)

	// Every place --quote-end could land, found once rather than per start.
	// Enumerated in full because taking the first one silently is the bug this
	// function exists to prevent, only moved from the start to the end: an end
	// of "。" has dozens of candidates, and the shortest span looks exactly
	// like the intended one to whoever reads the result.
	type endMatch struct{ at, length int }
	var endMatches []endMatch
	if end != "" {
		for j := 0; j < len(haystack); j++ {
			if l := matchLenAt(haystack, j, endRunes); l >= 0 {
				endMatches = append(endMatches, endMatch{at: j, length: l})
			}
		}
	}

	var spans []quoteSpan
	startHits := 0
	endMissing := 0
	capped := false

collect:
	for i := 0; i < len(haystack); i++ {
		startLen := matchLenAt(haystack, i, startRunes)
		if startLen < 0 {
			continue
		}
		startHits++
		if prefix != "" && !precededBy(haystack[:i], prefixRunes) {
			continue
		}
		from := i + startLen

		if end == "" {
			if suffix != "" && !followedBy(haystack[from:], suffixRunes) {
				continue
			}
			spans = append(spans, quoteSpan{Start: i, End: from, Text: string(haystack[i:from])})
			continue
		}

		reached := false
		for _, m := range endMatches {
			if m.at < from {
				continue
			}
			reached = true
			spanEnd := m.at + m.length
			if suffix != "" && !followedBy(haystack[spanEnd:], suffixRunes) {
				continue
			}
			spans = append(spans, quoteSpan{Start: i, End: spanEnd, Text: string(haystack[i:spanEnd])})
			// Past two, the exact number changes nothing — it is already an
			// error — and the bound keeps a one-character quote from scanning
			// the description once per match.
			if len(spans) >= maxQuoteSpanCandidates {
				capped = true
				break collect
			}
		}
		if !reached {
			endMissing++
		}
	}

	switch {
	case startHits == 0:
		return quoteSpan{}, fmt.Errorf(
			"--%s text does not appear in the %s; copy it verbatim from the passage",
			names.Start, names.Noun)
	case len(spans) == 0 && endMissing == startHits:
		return quoteSpan{}, fmt.Errorf(
			"--%s matched %d time(s), but --%s does not appear after any of them; "+
				"copy the end of the passage verbatim, and check it comes after the start",
			names.Start, startHits, names.End)
	case len(spans) == 0:
		return quoteSpan{}, fmt.Errorf(
			"--%s matched %d time(s), but none of them is surrounded by the given "+
				"--%s/--%s; copy that text verbatim from around the passage",
			names.Start, startHits, names.Prefix, names.Suffix)
	case len(spans) > 1:
		return quoteSpan{}, ambiguousQuoteError(spans, capped, names)
	}
	return spans[0], nil
}

// maxQuoteSpanCandidates bounds the search. Two is already an error, so the
// only thing a higher count buys is a more precise message.
const maxQuoteSpanCandidates = 32

// ambiguousQuoteError names the edge that is actually ambiguous, because the
// two have different fixes: a repeated start needs surrounding context, while a
// repeated end needs a longer end — no amount of --quote-prefix would change
// where a span STOPS.
func ambiguousQuoteError(spans []quoteSpan, capped bool, names spanFlagNames) error {
	count := fmt.Sprintf("%d", len(spans))
	if capped {
		count = fmt.Sprintf("at least %d", len(spans))
	}

	sameStart := true
	for _, s := range spans[1:] {
		if s.Start != spans[0].Start {
			sameStart = false
			break
		}
	}
	if sameStart {
		if !names.ContextFlags {
			return fmt.Errorf(
				"--%s matches %s places after the start, so the passage has no single ending; "+
					"copy more of the passage's last line into --%s", names.End, count, names.End)
		}
		return fmt.Errorf(
			"--%s matches %s places after the start, so the passage has no single ending; "+
				"copy more of the passage's last line into --%s, or add --%s with "+
				"the text just after it", names.End, count, names.End, names.Suffix)
	}
	if !names.ContextFlags {
		return fmt.Errorf(
			"the anchor matches %s spans in the %s; copy more of the passage into --%s "+
				"so it matches only once", count, names.Noun, names.Start)
	}
	return fmt.Errorf(
		"the quote matches %s spans in the %s; add --%s with the text just "+
			"before the passage (or --%s with the text just after) to say which one",
		count, names.Noun, names.Prefix, names.Suffix)
}

func isQuoteSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

// matchLenAt reports how many characters of haystack the needle covers starting
// at `at`, or -1 for no match.
//
// Whitespace is matched loosely: any run of whitespace in the needle matches
// any run in the haystack. Without this, an edge copied out of a rendered
// document could never match its own source — a paragraph break is "\n\n" in
// the stored Markdown but arrives as a single space once it has been through a
// one-line command, and every multi-line passage would fail to resolve.
//
// The consumed length is returned rather than assumed equal to the needle,
// because loose whitespace means the two can differ.
func matchLenAt(haystack []rune, at int, needle []rune) int {
	h, n := at, 0
	for n < len(needle) {
		if isQuoteSpace(needle[n]) {
			for n < len(needle) && isQuoteSpace(needle[n]) {
				n++
			}
			if h >= len(haystack) || !isQuoteSpace(haystack[h]) {
				return -1
			}
			for h < len(haystack) && isQuoteSpace(haystack[h]) {
				h++
			}
			continue
		}
		if h >= len(haystack) || haystack[h] != needle[n] {
			return -1
		}
		h++
		n++
	}
	return h - at
}

// The prefix must run right up to the passage, ignoring whitespace between
// them: a selection's edge rarely lands exactly on a space, so requiring one
// would make a generated handle fail against the text it came from.
func precededBy(before, prefix []rune) bool {
	end := len(before)
	for end > 0 && isQuoteSpace(before[end-1]) {
		end--
	}
	head := before[:end]
	for i := 0; i <= end; i++ {
		if l := matchLenAt(head, i, prefix); l >= 0 && i+l == end {
			return true
		}
	}
	return false
}

func followedBy(after, suffix []rune) bool {
	start := 0
	for start < len(after) && isQuoteSpace(after[start]) {
		start++
	}
	return matchLenAt(after, start, suffix) >= 0
}

// quoteEdgeFlags is ordered so the error below names the same flag every run;
// ranging a map would pick a different one each time.
var quoteEdgeFlags = []string{"quote-end", "quote-prefix", "quote-suffix"}

// quoteSpecFromFlags reports whether `issue get` was asked for a span.
//
// An edge flag without --quote-start is an error rather than an ignored flag:
// ignoring it would print the whole description, which looks exactly like a
// quote that worked and happened to be long.
func quoteSpecFromFlags(cmd *cobra.Command) (quoteSpec, bool, error) {
	start, _ := cmd.Flags().GetString("quote-start")
	if strings.TrimSpace(start) == "" {
		for _, name := range quoteEdgeFlags {
			if v, _ := cmd.Flags().GetString(name); strings.TrimSpace(v) != "" {
				return quoteSpec{}, false, fmt.Errorf("--%s needs --quote-start", name)
			}
		}
		return quoteSpec{}, false, nil
	}
	end, _ := cmd.Flags().GetString("quote-end")
	prefix, _ := cmd.Flags().GetString("quote-prefix")
	suffix, _ := cmd.Flags().GetString("quote-suffix")
	return quoteSpec{Start: start, End: end, Prefix: prefix, Suffix: suffix}, true, nil
}

// printIssueQuote emits the span alone. The identifier and title come with it
// so the reader knows which issue the passage was cut from; the rest of the
// description does not, which is the whole point.
func printIssueQuote(cmd *cobra.Command, issue map[string]any, spec quoteSpec) error {
	span, err := locateQuote(strVal(issue, "description"), spec)
	if err != nil {
		return err
	}

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		fmt.Fprintf(os.Stdout, "%s  %s\n\n%s\n",
			issueDisplayKey(issue), strVal(issue, "title"), span.Text)
		return nil
	}
	return cli.PrintJSON(os.Stdout, map[string]any{
		"identifier":   issueDisplayKey(issue),
		"title":        strVal(issue, "title"),
		"quote":        span.Text,
		"start_offset": span.Start,
		"end_offset":   span.End,
	})
}
