package cli

import (
	"strings"
	"testing"
)

// The done gate compares last_activity_at character for character, and the
// value it wants is already in the 409 body. Printing only the one-line
// message costs every caller the same database query to find it.

func TestConflictDetailNamesTheThreadsAndTheExactTimestamp(t *testing.T) {
	t.Parallel()
	body := `{
	  "code": "comment_review_required",
	  "error": "请先处理评论：逐条选择解决或明确保留，然后附上聚合收尾说明",
	  "issues": [{
	    "identifier": "COC-318",
	    "threads": [
	      {"thread_root_id": "39546146-b80c-45eb-8491-589d429e4d49",
	       "content": "本卡索引，只列产物落点", "reply_count": 0,
	       "last_activity_at": "2026-08-22T11:27:16.001388Z", "pinned": true},
	      {"thread_root_id": "aaaa1111-0000-0000-0000-000000000000",
	       "content": "端到端闭合", "reply_count": 2,
	       "last_activity_at": "2026-08-22T11:46:52.5Z", "pinned": false}
	    ]
	  }]
	}`
	got := renderConflictDetail(body)
	if got == "" {
		t.Fatal("a recognized conflict must expand, not fall back to the one-liner")
	}
	for _, want := range []string{
		"2 条线程还欠处置",
		"39546146-b80c-45eb-8491-589d429e4d49",
		// The trailing zeros matter: reformatting this from the database is
		// exactly how a caller gets it wrong.
		"2026-08-22T11:27:16.001388Z",
		"2026-08-22T11:46:52.5Z",
		"COC-318",
		"[pinned]",
		"2 条回复",
		"keep_unresolved",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail is missing %q:\n%s", want, got)
		}
	}
}

func TestAnUnrelatedConflictStillFallsBackToTheOneLiner(t *testing.T) {
	t.Parallel()
	// A body this does not recognize must degrade to the plain message rather
	// than dump a raw response at the user.
	// The one that matters: a DIFFERENT conflict carrying threads of its own
	// must not be rendered as a comment review. Without this case the code
	// check is untested — every other body here is empty anyway.
	otherWithThreads := `{"code":"other_conflict","issues":[{"identifier":"COC-1",` +
		`"threads":[{"thread_root_id":"x","content":"y","last_activity_at":"z"}]}]}`
	for _, body := range []string{
		otherWithThreads,
		`{"error":"something else"}`,
		`{"code":"other_conflict","issues":[]}`,
		`{"code":"comment_review_required","issues":[]}`,
		`not json at all`,
		``,
	} {
		if got := renderConflictDetail(body); got != "" {
			t.Errorf("body %q must not expand, got:\n%s", body, got)
		}
	}
}
