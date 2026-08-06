package handler

import "strings"

// The banner stored at the top of a finished issue's description.
//
// Stored, not rendered: it has to survive being read as raw Markdown — through
// `multica issue get`, through the API, pasted into somewhere else — because
// that is where a reader picks up a three-month-old design and treats it as
// current. A banner the UI draws is invisible to every one of those paths.
//
// The cost is that this rewrites text the author wrote, so the write is made
// REVERSIBLE rather than one-way: inserted on the way into a terminal status,
// stripped on the way out. That is what keeps reopen→reclose from stacking two
// banners, and what keeps an in_progress issue from carrying a line that says
// it is finished.
//
// One language rather than the request's locale: this lands in the issue body,
// which is authored content with no locale of its own, and a body whose banner
// changes language depending on who happened to close it would be worse than
// one that always reads the same.
const archivedNoticeBody = "**本 issue 已完成** —— 以下正文仅作封存记录、对过去准确、对现在不一定。据此行动前请先核对查证当前状态。"

// archivedNoticeMarker is what strip matches on, and it is deliberately
// narrower than the full sentence: the wording above may be tuned later, and an
// issue closed under the old wording still has to be strippable when it is
// reopened. Anything that changes must stay to the right of this prefix.
const archivedNoticeMarker = "> **本 issue 已完成**"

// archivedNoticeBlock is the exact text inserted, as a Markdown blockquote so
// it reads as an annotation rather than as the author's own first paragraph.
const archivedNoticeBlock = "> " + archivedNoticeBody

// insertArchivedNotice puts the banner above the body, or returns the
// description unchanged when it is already there — the caller cannot always
// know whether a previous close already inserted one.
func insertArchivedNotice(description string) string {
	if hasArchivedNotice(description) {
		return description
	}
	trimmed := strings.TrimLeft(description, "\n")
	if trimmed == "" {
		return archivedNoticeBlock
	}
	return archivedNoticeBlock + "\n\n" + trimmed
}

// stripArchivedNotice removes the banner and the blank line under it, leaving
// the author's text exactly as it was before the close.
func stripArchivedNotice(description string) string {
	if !hasArchivedNotice(description) {
		return description
	}
	rest := strings.TrimLeft(description, "\n")
	// The banner is one line; everything after the first newline is the body.
	// A banner with no newline after it means the description was empty.
	idx := strings.IndexByte(rest, '\n')
	if idx < 0 {
		return ""
	}
	return strings.TrimLeft(rest[idx+1:], "\n")
}

func hasArchivedNotice(description string) bool {
	return strings.HasPrefix(strings.TrimLeft(description, "\n"), archivedNoticeMarker)
}
