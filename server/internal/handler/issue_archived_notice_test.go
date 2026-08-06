package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The banner is stored in the body rather than rendered, so the pair of
// operations has to be exactly reversible: whatever the author wrote must come
// back byte-for-byte when the issue is reopened. Everything below is about that
// invariant.

func TestInsertArchivedNotice_PutsTheBannerAboveTheBody(t *testing.T) {
	got := insertArchivedNotice("## 背景\n这一版的路由决策链路。")
	if !strings.HasPrefix(got, archivedNoticeMarker) {
		t.Fatalf("banner is not first:\n%s", got)
	}
	if !strings.Contains(got, "## 背景") {
		t.Fatalf("body was dropped:\n%s", got)
	}
}

// A second close must not stack a second banner. The caller cannot know
// whether an earlier close already inserted one, so insert has to be
// idempotent rather than trusting the transition to fire only once.
func TestInsertArchivedNotice_IsIdempotent(t *testing.T) {
	once := insertArchivedNotice("body")
	twice := insertArchivedNotice(once)
	if once != twice {
		t.Fatalf("second insert changed the description:\n%q\n%q", once, twice)
	}
	if n := strings.Count(twice, archivedNoticeMarker); n != 1 {
		t.Fatalf("banner appears %d times, want 1:\n%s", n, twice)
	}
}

// The whole justification for writing into the body instead of rendering is
// that it can be taken back out. If a round trip does not return the original
// text, the feature has silently eaten the author's writing.
func TestArchivedNotice_RoundTripsBackToTheOriginal(t *testing.T) {
	for _, original := range []string{
		"## 背景\n这一版的路由决策链路。",
		"single line",
		"",
		"trailing newline\n",
		"多段\n\n中间有空行\n\n结尾",
		// Text that merely mentions the banner's words must survive too — the
		// marker only counts at the very start.
		"讨论：**本 issue 已完成** 这句话该不该写死",
	} {
		closed := insertArchivedNotice(original)
		reopened := stripArchivedNotice(closed)
		want := strings.TrimLeft(original, "\n")
		if reopened != want {
			t.Fatalf("round trip lost content:\n original  %q\n closed    %q\n reopened  %q",
				original, closed, reopened)
		}
	}
}

// Reopening an issue that never carried a banner must not eat its first line.
func TestStripArchivedNotice_LeavesAnUnbanneredBodyAlone(t *testing.T) {
	body := "## 背景\n没有横幅的正文"
	if got := stripArchivedNotice(body); got != body {
		t.Fatalf("stripped a body that had no banner:\n%q", got)
	}
}

// The marker is narrower than the sentence on purpose: the wording may be
// tuned later, and an issue closed under the old wording still has to be
// strippable when someone reopens it.
func TestStripArchivedNotice_MatchesOnTheMarkerNotTheWholeSentence(t *testing.T) {
	oldWording := archivedNoticeMarker + " —— 一句以前的、现在已经不用的说法。\n\n真正的正文"
	if got := stripArchivedNotice(oldWording); got != "真正的正文" {
		t.Fatalf("old wording was not stripped, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Through the endpoint
// ---------------------------------------------------------------------------

func updateIssueStatus(t *testing.T, issueID, status string) *httptest.ResponseRecorder {
	t.Helper()
	return putIssue(t, issueID, map[string]any{"status": status})
}

func issueDescription(t *testing.T, issueID string) string {
	t.Helper()
	_, description := issueBody(t, issueID)
	return description
}

func TestUpdateIssue_InsertsTheBannerOnDone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "archived banner", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	putIssue(t, issueID, map[string]any{"description": "作者写的正文"})

	if rec := updateIssueStatus(t, issueID, "done"); rec.Code != http.StatusOK {
		t.Fatalf("status → done: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got := issueDescription(t, issueID)
	if !strings.HasPrefix(got, archivedNoticeMarker) {
		t.Fatalf("no banner after closing:\n%s", got)
	}
	if !strings.Contains(got, "作者写的正文") {
		t.Fatalf("author's text was lost:\n%s", got)
	}
}

// Reopening has to give the author's text back untouched — otherwise closing
// an issue is a one-way edit to something you wrote.
func TestUpdateIssue_StripsTheBannerOnReopen(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "archived banner reopen", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	putIssue(t, issueID, map[string]any{"description": "作者写的正文"})

	updateIssueStatus(t, issueID, "done")
	updateIssueStatus(t, issueID, "in_progress")

	if got := issueDescription(t, issueID); got != "作者写的正文" {
		t.Fatalf("reopen did not restore the original body, got %q", got)
	}
}

// done → cancelled is finished-to-finished. It never crosses the boundary, so
// it must not stack a second banner.
func TestUpdateIssue_DoesNotStackBannersBetweenTerminalStatuses(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "terminal to terminal", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	putIssue(t, issueID, map[string]any{"description": "作者写的正文"})

	updateIssueStatus(t, issueID, "done")
	updateIssueStatus(t, issueID, "cancelled")

	got := issueDescription(t, issueID)
	if n := strings.Count(got, archivedNoticeMarker); n != 1 {
		t.Fatalf("banner appears %d times, want 1:\n%s", n, got)
	}
}

// Close, reopen, close again — the shape that would double the banner if
// insert were not idempotent and strip did not run on the way out.
func TestUpdateIssue_SurvivesCloseReopenClose(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "close reopen close", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	putIssue(t, issueID, map[string]any{"description": "作者写的正文"})

	updateIssueStatus(t, issueID, "done")
	updateIssueStatus(t, issueID, "todo")
	updateIssueStatus(t, issueID, "done")

	got := issueDescription(t, issueID)
	if n := strings.Count(got, archivedNoticeMarker); n != 1 {
		t.Fatalf("banner appears %d times, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "作者写的正文") {
		t.Fatalf("author's text was lost:\n%s", got)
	}
}

// A move that does not cross the boundary leaves the body completely alone.
func TestUpdateIssue_LeavesTheBodyAloneWithinUnfinishedStatuses(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "unfinished move", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	putIssue(t, issueID, map[string]any{"description": "作者写的正文"})

	updateIssueStatus(t, issueID, "in_progress")

	if got := issueDescription(t, issueID); got != "作者写的正文" {
		t.Fatalf("body changed on an unfinished move, got %q", got)
	}
}

// The freeze still owns the caller's own edits: annotating the transition must
// not become a hole through which a finished body can be rewritten.
func TestUpdateIssue_FreezeStillRefusesABodyEditOnAFinishedIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "freeze still holds", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	updateIssueStatus(t, issueID, "done")

	rec := putIssue(t, issueID, map[string]any{"description": "偷偷改一下"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on a frozen body, got %d: %s", rec.Code, rec.Body.String())
	}
}
