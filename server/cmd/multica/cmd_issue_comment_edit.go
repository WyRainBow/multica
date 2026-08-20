package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/util"
)

// commentEditKind is which way `comment edit` was asked to build the next
// body. The server only ever takes a complete replacement, so the difference
// between the kinds is who assembles it: the caller (whole body) or the
// command (fetch the current body, splice, send the result).
type commentEditKind int

const (
	// commentEditReplaceBody: --content/--content-stdin/--content-file gave
	// the entire next body.
	commentEditReplaceBody commentEditKind = iota
	// commentEditReplaceSpan: --replace (or --replace-start/--replace-end)
	// named one passage and --with holds its replacement.
	commentEditReplaceSpan
	// commentEditAppend: --append/--append-stdin adds a paragraph at the end.
	commentEditAppend
)

// commentEditTarget is a parsed, conflict-checked edit. Everything except
// anchor resolution happens at parse time; the anchors can only be resolved
// against the current body, so apply() finishes the job once GET returns it.
type commentEditTarget struct {
	kind     commentEditKind
	body     string // commentEditReplaceBody: the whole next body
	addition string // commentEditAppend: the text to append
	spec     quoteSpec
	names    spanFlagNames // commentEditReplaceSpan: flag names for its errors
	with     string        // commentEditReplaceSpan: the replacement text
}

// commentEditTargetFromFlags turns the edit flags into one target.
//
// Every combination that leaves the intended edit ambiguous is an error
// here rather than a precedence rule: "the flag you forgot to clear
// silently won" is how an append turns into a whole-body replacement and
// erases a thread's context. Conflicts are also checked BEFORE anything
// reads stdin, so a conflicted run fails without first swallowing the pipe
// someone is holding open.
func commentEditTargetFromFlags(cmd *cobra.Command) (commentEditTarget, error) {
	contentInline, _ := cmd.Flags().GetString("content")
	contentStdin, _ := cmd.Flags().GetBool("content-stdin")
	contentFile, _ := cmd.Flags().GetString("content-file")
	replace, _ := cmd.Flags().GetString("replace")
	start, _ := cmd.Flags().GetString("replace-start")
	end, _ := cmd.Flags().GetString("replace-end")
	withInline, _ := cmd.Flags().GetString("with")
	withStdin, _ := cmd.Flags().GetBool("with-stdin")
	appendInline, _ := cmd.Flags().GetString("append")
	appendStdin, _ := cmd.Flags().GetBool("append-stdin")

	wantsBody := contentInline != "" || contentStdin || contentFile != ""
	wantsSpan := replace != "" || start != "" || end != "" || withInline != "" || withStdin
	wantsAppend := appendInline != "" || appendStdin

	if (wantsBody && wantsSpan) || (wantsBody && wantsAppend) || (wantsSpan && wantsAppend) {
		return commentEditTarget{}, fmt.Errorf(
			"--content/--content-stdin/--content-file, --replace*/--with, and " +
				"--append/--append-stdin are mutually exclusive; pick one way to build the next body")
	}

	switch {
	case wantsBody:
		body, ok, err := resolveTextFlag(cmd, "content")
		if err != nil {
			return commentEditTarget{}, err
		}
		if !ok {
			// Unreachable while wantsBody is defined above, but failing loud
			// beats PUTting an empty body if that ever drifts.
			return commentEditTarget{}, fmt.Errorf("--content, --content-stdin, or --content-file is required")
		}
		return commentEditTarget{kind: commentEditReplaceBody, body: body}, nil
	case wantsSpan:
		return commentEditSpanTarget(cmd, replace, start, end)
	case wantsAppend:
		addition, ok, err := resolveInlineOrStdinFlag(cmd, "append")
		if err != nil {
			return commentEditTarget{}, err
		}
		if !ok {
			return commentEditTarget{}, fmt.Errorf("--append or --append-stdin is required")
		}
		return commentEditTarget{kind: commentEditAppend, addition: addition}, nil
	default:
		return commentEditTarget{}, fmt.Errorf(
			"nothing to edit: give --content/--content-stdin/--content-file, or --replace " +
				"(or --replace-start/--replace-end) with --with, or --append/--append-stdin")
	}
}

// commentEditSpanTarget validates the anchor shape and pairs it with --with.
//
// --replace-start and --replace-end go together or not at all: allowing a
// lone start would mean "replace only the start text", which is --replace
// wearing a longer flag name, and a lone end names a stop with no start.
func commentEditSpanTarget(cmd *cobra.Command, replace, start, end string) (commentEditTarget, error) {
	if replace != "" && (start != "" || end != "") {
		return commentEditTarget{}, fmt.Errorf(
			"--replace and --replace-start/--replace-end are mutually exclusive; use " +
				"--replace for a short passage, or the start/end pair for a long one")
	}

	spec := quoteSpec{}
	names := spanFlagNames{Noun: "comment"}
	switch {
	case replace != "":
		spec.Start = replace
		names.Start = "replace"
	case start != "" && end != "":
		spec.Start, spec.End = start, end
		names.Start, names.End = "replace-start", "replace-end"
	case start != "":
		return commentEditTarget{}, fmt.Errorf(
			"--replace-start needs --replace-end to say where the replaced passage stops")
	case end != "":
		return commentEditTarget{}, fmt.Errorf("--replace-end needs --replace-start")
	default:
		return commentEditTarget{}, fmt.Errorf(
			"--with needs an anchor: give --replace, or --replace-start/--replace-end, " +
				"to say what it replaces")
	}

	with, ok, err := resolveInlineOrStdinFlag(cmd, "with")
	if err != nil {
		return commentEditTarget{}, err
	}
	if !ok {
		return commentEditTarget{}, fmt.Errorf(
			"--with or --with-stdin is required with the --replace flags")
	}
	return commentEditTarget{kind: commentEditReplaceSpan, spec: spec, names: names, with: with}, nil
}

// resolveInlineOrStdinFlag is resolveTextFlag's two-source sibling for flags
// that deliberately have no -file variant. Separate from resolveTextFlag so
// the mutual-exclusion error names only flags that exist on the command.
func resolveInlineOrStdinFlag(cmd *cobra.Command, name string) (string, bool, error) {
	inline, _ := cmd.Flags().GetString(name)
	useStdin, _ := cmd.Flags().GetBool(name + "-stdin")
	if inline != "" && useStdin {
		return "", false, fmt.Errorf("--%s and --%s-stdin are mutually exclusive", name, name)
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --%s-stdin: %w", name, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("stdin content for --%s-stdin is empty", name)
		}
		return body, true, nil
	}
	if inline == "" {
		return "", false, nil
	}
	return util.UnescapeBackslashEscapes(inline), true, nil
}

// apply builds the next body from the current one. For a whole-body edit the
// current body is neither read nor needed.
func (t commentEditTarget) apply(current string) (string, error) {
	switch t.kind {
	case commentEditAppend:
		return appendCommentBody(current, t.addition), nil
	case commentEditReplaceSpan:
		span, err := locateSpan(current, t.spec, t.names)
		if err != nil {
			return "", err
		}
		// Character offsets, per quoteSpan's contract: byte slicing would
		// land mid-character on any CJK comment.
		runes := []rune(current)
		return string(runes[:span.Start]) + t.with + string(runes[span.End:]), nil
	default:
		return t.body, nil
	}
}

// appendCommentBody joins the addition as its own paragraph. Trailing blank
// lines on the old body collapse into the one separator so appending twice
// in a row does not grow an ever-deeper gap.
func appendCommentBody(current, addition string) string {
	base := strings.TrimRight(current, "\n")
	if base == "" {
		return addition
	}
	return base + "\n\n" + addition
}
