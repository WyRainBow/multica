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
	start := strings.TrimSpace(spec.Start)
	if start == "" {
		return quoteSpan{}, fmt.Errorf("--quote-start must not be blank")
	}
	end := strings.TrimSpace(spec.End)
	prefix := strings.TrimSpace(spec.Prefix)
	suffix := strings.TrimSpace(spec.Suffix)

	haystack := []rune(text)
	startRunes := []rune(start)
	endRunes := []rune(end)
	prefixRunes := []rune(prefix)
	suffixRunes := []rune(suffix)

	var spans []quoteSpan
	startHits := 0
	endMissing := 0

	for i := 0; i < len(haystack); i++ {
		startLen := matchLenAt(haystack, i, startRunes)
		if startLen < 0 {
			continue
		}
		startHits++
		if prefix != "" && !precededBy(haystack[:i], prefixRunes) {
			continue
		}
		spanEnd := i + startLen
		if end != "" {
			offset, endLen := indexRunes(haystack[spanEnd:], endRunes)
			if offset < 0 {
				endMissing++
				continue
			}
			spanEnd += offset + endLen
		}
		if suffix != "" && !followedBy(haystack[spanEnd:], suffixRunes) {
			continue
		}
		spans = append(spans, quoteSpan{Start: i, End: spanEnd, Text: string(haystack[i:spanEnd])})
	}

	switch {
	case startHits == 0:
		return quoteSpan{}, fmt.Errorf(
			"--quote-start text does not appear in the description; copy it verbatim from the passage")
	case len(spans) == 0 && endMissing == startHits:
		return quoteSpan{}, fmt.Errorf(
			"--quote-start matched %d time(s), but --quote-end does not appear after any of them; "+
				"copy the end of the passage verbatim, and check it comes after the start", startHits)
	case len(spans) == 0:
		return quoteSpan{}, fmt.Errorf(
			"--quote-start matched %d time(s), but none of them is surrounded by the given "+
				"--quote-prefix/--quote-suffix; copy that text verbatim from around the passage", startHits)
	case len(spans) > 1:
		return quoteSpan{}, fmt.Errorf(
			"the quote matches %d spans in the description; add --quote-prefix with the text just "+
				"before the passage (or --quote-suffix with the text just after) to say which one", len(spans))
	}
	return spans[0], nil
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

// indexRunes returns the offset of the first loose match of needle within
// haystack and how much it covers, or (-1, 0).
func indexRunes(haystack, needle []rune) (int, int) {
	for i := 0; i < len(haystack); i++ {
		if l := matchLenAt(haystack, i, needle); l >= 0 {
			return i, l
		}
	}
	return -1, 0
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
