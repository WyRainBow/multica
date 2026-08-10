package main

import (
	"strings"
	"testing"
)

// The resolution is the one comment in a thread a reader has to be able to
// find: it is what a folded read keeps, and what the app marks in green. These
// pin the two places the CLI has to say so, because both were silent before —
// the JSON carried resolved_at all along while a table reader saw a settled
// thread as an undifferentiated list.

func TestCommentListTable_MarksTheResolution(t *testing.T) {
	resolved := map[string]any{"type": "comment", "resolved_at": "2026-08-09T19:42:08-07:00"}
	if got := commentTableKind(resolved); got != "resolution" {
		t.Fatalf("kind = %q, want %q", got, "resolution")
	}
}

func TestCommentListTable_LeavesAnOrdinaryCommentAlone(t *testing.T) {
	if got := commentTableKind(map[string]any{"type": "comment"}); got != "comment" {
		t.Fatalf("kind = %q, want the row's own type", got)
	}
	// A null resolved_at is the shape the API actually returns for an
	// unresolved comment, and strVal must not read it as a timestamp.
	if got := commentTableKind(map[string]any{"type": "comment", "resolved_at": nil}); got != "comment" {
		t.Fatalf("kind = %q for a null resolved_at, want %q", got, "comment")
	}
}

// A system comment that somehow carries a resolution still reads as one — the
// column answers "is this the conclusion", and nothing else in the row does.
func TestCommentListTable_ResolutionWinsOverTheRowType(t *testing.T) {
	row := map[string]any{"type": "system", "resolved_at": "2026-08-09T19:42:08-07:00"}
	if got := commentTableKind(row); got != "resolution" {
		t.Fatalf("kind = %q, want %q", got, "resolution")
	}
}

// Which comment to pass is the part that is easy to get backwards, and
// backwards actively hides the conclusion. If that stops being in the help,
// an agent reading only --help has no way to learn it.
func TestResolveHelp_SaysWhichCommentToPass(t *testing.T) {
	long := issueCommentResolveCmd.Long
	for _, want := range []string{
		"not the thread root",
		"the root ALONE",
		"at most one conclusion",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("resolve --help lost %q", want)
		}
	}
}

// The reopen gap is a real defect, not a preference: replying does not clear a
// conclusion that lives on a reply. Until it is fixed, the help is the only
// place a caller learns why their reply vanished.
func TestUnresolveHelp_ExplainsTheReopenGap(t *testing.T) {
	long := issueCommentUnresolveCmd.Long
	for _, want := range []string{
		"THREAD ROOT",
		"hidden from the default reads",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("unresolve --help lost %q", want)
		}
	}
}

// The default route lives in three places that cannot import each other:
// issuephase.DefaultRoute on the server, PHASE_TEMPLATES in the web app, and
// this help text. An agent that only runs --help learns the route here, and a
// stale list sends it to file into stations that do not exist.
//
// Pinned on `list` rather than the parent `phase` command because the help
// template renders Long only on leaf commands — the paragraph on the parent is
// never shown to anyone.
func TestPhaseListHelp_NamesTheDefaultRoute(t *testing.T) {
	long := issuePhaseListCmd.Long
	for _, station := range []string{"需求梳理", "方案评审", "代码评审", "测试验收", "需求冻结"} {
		if !strings.Contains(long, station) {
			t.Errorf("phase list --help does not name %q", station)
		}
	}
	// Why the two reviews are separate is the part that stops someone
	// "simplifying" them back into one station.
	if !strings.Contains(long, "different questions of different") {
		t.Error("phase list --help lost why the two reviews are separate")
	}
}
