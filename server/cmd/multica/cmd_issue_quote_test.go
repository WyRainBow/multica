package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A description with the two things that break naive matching: a phrase that
// appears twice, and CJK text before the passage so byte offsets diverge from
// character offsets.
const quoteDoc = "# 概述\n\n背景说明。\n\n## 路由决策链路\n\n第一处正文，讲进域资格。\n\n## 附录\n\n再次提到 路由决策链路 这个词，但这里只是引用。\n"

func TestLocateQuote_ReturnsTheSpanBetweenItsEdges(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{
		Start: "## 路由决策链路",
		End:   "进域资格。",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(span.Text, "## 路由决策链路") || !strings.HasSuffix(span.Text, "进域资格。") {
		t.Fatalf("span does not run edge to edge: %q", span.Text)
	}
	if strings.Contains(span.Text, "附录") {
		t.Fatalf("span ran past its end into the next section: %q", span.Text)
	}
}

// The offsets are only meaningful if reading the description at them returns
// the span. A byte offset would land mid-character here, and be wrong by an
// amount that grows with how much CJK text precedes the passage.
func TestLocateQuote_OffsetsAreCharactersNotBytes(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{Start: "## 附录"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	runes := []rune(quoteDoc)
	if got := string(runes[span.Start:span.End]); got != span.Text {
		t.Fatalf("reading at the offsets gives %q, want %q", got, span.Text)
	}
	if span.Start == strings.Index(quoteDoc, "## 附录") {
		t.Fatalf("offset %d equals the byte index, so it is not a character offset", span.Start)
	}
}

// Without --quote-end the span is the start text alone, which is what a
// one-line selection produces.
func TestLocateQuote_WithoutAnEndReturnsTheStartTextAlone(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{Start: "第一处正文"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.Text != "第一处正文" {
		t.Fatalf("span = %q, want the start text alone", span.Text)
	}
}

// The failure that matters most: a phrase appearing twice must not resolve to
// whichever came first. An agent would review the wrong passage and report it
// with full confidence.
func TestLocateQuote_RefusesToGuessBetweenSeveralMatches(t *testing.T) {
	_, err := locateQuote(quoteDoc, quoteSpec{Start: "路由决策链路"})
	if err == nil {
		t.Fatalf("expected an error for a phrase that appears twice")
	}
	if !strings.Contains(err.Error(), "2 spans") {
		t.Fatalf("error should say how many spans matched: %v", err)
	}
	if !strings.Contains(err.Error(), "--quote-prefix") {
		t.Fatalf("error should say how to disambiguate: %v", err)
	}
}

// A short end like "。" has a candidate at the close of every sentence.
// Silently taking the nearest one hands back a truncated passage that reads
// exactly like a complete one — the same failure as guessing between starts,
// moved to the other edge.
func TestLocateQuote_RefusesToGuessBetweenSeveralEndings(t *testing.T) {
	// "。" closes three sentences after this start.
	_, err := locateQuote(quoteDoc, quoteSpec{Start: "# 概述", End: "。"})
	if err == nil {
		t.Fatalf("expected an error when the end has several candidates")
	}
	if !strings.Contains(err.Error(), "--quote-end") {
		t.Fatalf("error should name --quote-end, not the start: %v", err)
	}
	// The fix for a repeated end is a longer end, never surrounding context on
	// the start — saying otherwise sends the caller down a road that cannot work.
	if strings.Contains(err.Error(), "--quote-prefix with the text just before") {
		t.Fatalf("error offers a fix that cannot change where a span stops: %v", err)
	}
}

// Two ways out of an ambiguous ending, both of which the generated handles
// can produce: a longer end, or pinning the text that follows the passage.
func TestLocateQuote_ResolvesAnAmbiguousEnding(t *testing.T) {
	if _, err := locateQuote(quoteDoc, quoteSpec{Start: "# 概述", End: "。"}); err == nil {
		t.Fatalf("precondition: this end should be ambiguous")
	}

	longer, err := locateQuote(quoteDoc, quoteSpec{Start: "# 概述", End: "讲进域资格。"})
	if err != nil {
		t.Fatalf("a longer end should resolve it: %v", err)
	}
	if !strings.HasSuffix(longer.Text, "讲进域资格。") {
		t.Fatalf("span = %q", longer.Text)
	}

	pinned, err := locateQuote(quoteDoc, quoteSpec{
		Start:  "# 概述",
		End:    "。",
		Suffix: "## 路由决策链路",
	})
	if err != nil {
		t.Fatalf("a suffix should resolve it: %v", err)
	}
	if !strings.HasSuffix(pinned.Text, "背景说明。") {
		t.Fatalf("suffix picked the wrong ending: %q", pinned.Text)
	}
}

// A repeated start and a repeated end need different fixes, so they must not
// share one message.
func TestLocateQuote_NamesTheStartWhenItIsTheStartThatRepeats(t *testing.T) {
	_, err := locateQuote(quoteDoc, quoteSpec{Start: "路由决策链路"})
	if err == nil {
		t.Fatalf("expected an error for a repeated start")
	}
	if !strings.Contains(err.Error(), "--quote-prefix") {
		t.Fatalf("a repeated start should be fixed with context: %v", err)
	}
	if strings.Contains(err.Error(), "--quote-end matches") {
		t.Fatalf("error blames the end for a repeated start: %v", err)
	}
}

func TestLocateQuote_PrefixPicksBetweenSeveralMatches(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{
		Start:  "路由决策链路",
		Prefix: "再次提到",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The second occurrence sits after "## 附录"; the first does not.
	if span.Start < len([]rune(quoteDoc))/2 {
		t.Fatalf("prefix selected the first occurrence at %d, want the later one", span.Start)
	}
}

func TestLocateQuote_SuffixPicksBetweenSeveralMatches(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{
		Start:  "路由决策链路",
		Suffix: "这个词",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.Start < len([]rune(quoteDoc))/2 {
		t.Fatalf("suffix selected the first occurrence at %d, want the later one", span.Start)
	}
}

// Whitespace between the passage and its context is ignored, because a
// selection's edge rarely lands exactly on a space — a handle generated from
// this very document has to match it.
func TestLocateQuote_IgnoresWhitespaceBetweenThePassageAndItsContext(t *testing.T) {
	if _, err := locateQuote(quoteDoc, quoteSpec{
		Start:  "第一处正文",
		Prefix: "## 路由决策链路",
	}); err != nil {
		t.Fatalf("prefix separated by blank lines should still match: %v", err)
	}
}

// An edge travels through a one-line command, so its paragraph breaks arrive
// as single spaces. Matching whitespace strictly would fail every multi-line
// passage — which is most of them.
func TestLocateQuote_MatchesAFlattenedEdgeAcrossAParagraphBreak(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{Start: "## 路由决策链路 第一处正文"})
	if err != nil {
		t.Fatalf("a flattened edge should still match its source: %v", err)
	}
	// The span covers the real text, newlines and all — not the flattened form.
	if !strings.Contains(span.Text, "\n") {
		t.Fatalf("span lost the original line breaks: %q", span.Text)
	}
	runes := []rune(quoteDoc)
	if got := string(runes[span.Start:span.End]); got != span.Text {
		t.Fatalf("offsets do not bracket the span after a loose match: %q vs %q", got, span.Text)
	}
}

// Loose whitespace must not become loose matching in general: a missing
// character is still a miss.
func TestLocateQuote_LooseWhitespaceDoesNotMakeOtherTextOptional(t *testing.T) {
	if _, err := locateQuote(quoteDoc, quoteSpec{Start: "## 路由决策 第一处正文"}); err == nil {
		t.Fatalf("dropping characters inside an edge should not match")
	}
}

// Context matching is loose in the same way, for the same reason.
func TestLocateQuote_ContextMatchesAcrossAParagraphBreak(t *testing.T) {
	if _, err := locateQuote(quoteDoc, quoteSpec{
		Start:  "路由决策链路",
		Suffix: "第一处正文，讲进域资格。",
	}); err != nil {
		t.Fatalf("suffix separated by a blank line should match: %v", err)
	}
}

// A passage copied out of a document carries surrounding whitespace; matching
// it verbatim would fail against its own source.
func TestLocateQuote_TrimsTheEdges(t *testing.T) {
	span, err := locateQuote(quoteDoc, quoteSpec{Start: "  ## 附录\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.Text != "## 附录" {
		t.Fatalf("span = %q, want the trimmed edge", span.Text)
	}
}

func TestLocateQuote_RejectsABlankStart(t *testing.T) {
	if _, err := locateQuote(quoteDoc, quoteSpec{Start: "   "}); err == nil {
		t.Fatalf("expected an error for a blank --quote-start")
	}
}

func TestLocateQuote_SaysWhenTheStartIsNotThere(t *testing.T) {
	_, err := locateQuote(quoteDoc, quoteSpec{Start: "这段不存在"})
	if err == nil {
		t.Fatalf("expected an error for text that is not in the description")
	}
	if !strings.Contains(err.Error(), "verbatim") {
		t.Fatalf("error should tell the caller to copy it verbatim: %v", err)
	}
}

// A missing end is a different mistake from a missing start, and saying so is
// the difference between fixing one flag and re-copying both.
func TestLocateQuote_DistinguishesAMissingEndFromAMissingStart(t *testing.T) {
	_, err := locateQuote(quoteDoc, quoteSpec{
		Start: "# 概述",
		End:   "这段不存在",
	})
	if err == nil {
		t.Fatalf("expected an error for an end that never appears")
	}
	if !strings.Contains(err.Error(), "--quote-end") {
		t.Fatalf("error should name --quote-end as the wrong one: %v", err)
	}
}

// An end that exists but sits BEFORE the start is the same failure: a span
// cannot run backwards.
func TestLocateQuote_RejectsAnEndThatPrecedesTheStart(t *testing.T) {
	if _, err := locateQuote(quoteDoc, quoteSpec{
		Start: "## 附录",
		End:   "# 概述",
	}); err == nil {
		t.Fatalf("expected an error for an end that only appears before the start")
	}
}

func TestLocateQuote_SaysWhenTheContextDoesNotMatch(t *testing.T) {
	_, err := locateQuote(quoteDoc, quoteSpec{
		Start:  "路由决策链路",
		Prefix: "根本没写过这句",
	})
	if err == nil {
		t.Fatalf("expected an error when no match carries the given prefix")
	}
	if !strings.Contains(err.Error(), "--quote-prefix") {
		t.Fatalf("error should point at the context flags: %v", err)
	}
}

// --- flag wiring ---

func newQuoteFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("quote-start", "", "")
	cmd.Flags().String("quote-end", "", "")
	cmd.Flags().String("quote-prefix", "", "")
	cmd.Flags().String("quote-suffix", "", "")
	return cmd
}

func TestQuoteSpecFromFlags_ReportsNoQuoteWhenUnused(t *testing.T) {
	_, quoting, err := quoteSpecFromFlags(newQuoteFlagCmd())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if quoting {
		t.Fatalf("plain `issue get` should not be treated as a quote")
	}
}

// Ignoring a stray edge flag would print the whole description, which reads
// exactly like a quote that worked and happened to be long.
func TestQuoteSpecFromFlags_RejectsAnEdgeFlagWithoutAStart(t *testing.T) {
	for _, name := range []string{"quote-end", "quote-prefix", "quote-suffix"} {
		cmd := newQuoteFlagCmd()
		if err := cmd.Flags().Set(name, "x"); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
		_, _, err := quoteSpecFromFlags(cmd)
		if err == nil {
			t.Fatalf("--%s without --quote-start should be an error", name)
		}
		if !strings.Contains(err.Error(), "--quote-start") {
			t.Fatalf("error should name --quote-start: %v", err)
		}
	}
}

func TestQuoteSpecFromFlags_CarriesEveryEdge(t *testing.T) {
	cmd := newQuoteFlagCmd()
	for name, value := range map[string]string{
		"quote-start": "a", "quote-end": "b", "quote-prefix": "c", "quote-suffix": "d",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	spec, quoting, err := quoteSpecFromFlags(cmd)
	if err != nil || !quoting {
		t.Fatalf("quoting=%v err=%v", quoting, err)
	}
	if spec.Start != "a" || spec.End != "b" || spec.Prefix != "c" || spec.Suffix != "d" {
		t.Fatalf("spec = %+v", spec)
	}
}
