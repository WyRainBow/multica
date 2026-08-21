package execenv

import (
	"fmt"
	"strings"
	"testing"
)

// The issue's own artefacts used to be invisible to the agent working on it:
// the description and comments arrived, the spec and the closed review rounds
// did not. These tests pin the three things that made the omission costly —
// that the documents are named, that an issue without any renders exactly as
// before, and that a truncated list says so.

func issueCtxWithDocs(docs ...IssueDocForEnv) TaskContextForEnv {
	return TaskContextForEnv{
		IssueID:   "55555555-6666-7777-8888-999999999999",
		IssueDocs: docs,
	}
}

func TestIssueDocumentsAreNamedInTheBrief(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", issueCtxWithDocs(
		IssueDocForEnv{ID: "b65528d4-0991-478a-ae2f-ecc8a1564aca", Title: "COC-305 spec", Kind: "COC-305/spec"},
		IssueDocForEnv{ID: "576c4d8b-8c47-4df5-9edf-6c977e6bc08f", Title: "R1 方案评审", Kind: "COC-305/rounds/R1-方案评审"},
	))

	for _, want := range []string{
		"## Issue Documents",
		"COC-305 spec",
		"b65528d4-0991-478a-ae2f-ecc8a1564aca",
		"R1 方案评审",
		"576c4d8b-8c47-4df5-9edf-6c977e6bc08f",
		// The read entry matters as much as the list: a title with no way to
		// open it is a tease.
		"multica wiki get",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief is missing %q", want)
		}
	}

	// Bodies must not travel. The whole point of listing titles is that a
	// spec nobody reads costs one line rather than thousands of tokens.
	if strings.Contains(out, "轮次结论") {
		t.Error("document bodies leaked into the brief; only titles and ids should")
	}
}

func TestAnIssueWithNoDocumentsRendersExactlyAsBefore(t *testing.T) {
	t.Parallel()
	bare := TaskContextForEnv{IssueID: "55555555-6666-7777-8888-999999999999"}
	before := buildMetaSkillContent("claude", bare)

	if strings.Contains(before, "## Issue Documents") {
		t.Error("an issue with no documents must not get the section at all")
	}

	// Adding an empty (not nil) slice is the same as having none: a claim that
	// serialised `issue_docs: []` must not change a single byte of the brief,
	// or prompt-cache prefixes churn for no reason.
	empty := bare
	empty.IssueDocs = []IssueDocForEnv{}
	if got := buildMetaSkillContent("claude", empty); got != before {
		t.Error("an empty document list changed the brief; it must be byte-identical to having none")
	}
}

func TestTruncatedDocumentListSaysWhatItDropped(t *testing.T) {
	t.Parallel()
	docs := make([]IssueDocForEnv, issueDocsBriefLimit+3)
	for i := range docs {
		docs[i] = IssueDocForEnv{
			ID:    fmt.Sprintf("doc-%02d", i),
			Title: fmt.Sprintf("Round %d", i),
		}
	}
	out := buildMetaSkillContent("claude", issueCtxWithDocs(docs...))

	// A truncated list that does not admit it reads as the whole set, and the
	// agent stops looking exactly where the older rounds are.
	if !strings.Contains(out, "3 more not listed here") {
		t.Errorf("truncation must state how many were dropped:\n%s", out)
	}
	if strings.Contains(out, "doc-22") {
		t.Error("brief listed more documents than the cap allows")
	}
	if !strings.Contains(out, "doc-19") {
		t.Error("brief dropped a document that fits inside the cap")
	}
}

func TestAnUntitledDocumentStillGetsALine(t *testing.T) {
	t.Parallel()
	// A document with no title is still findable by id, and dropping the row
	// would silently shrink the index.
	out := buildMetaSkillContent("claude", issueCtxWithDocs(
		IssueDocForEnv{ID: "11111111-2222-3333-4444-555555555555", Title: "   "},
	))
	if !strings.Contains(out, "(untitled)") {
		t.Errorf("a titleless document must still be listed:\n%s", out)
	}
	if !strings.Contains(out, "11111111-2222-3333-4444-555555555555") {
		t.Error("a titleless document must still carry its id")
	}
}

func TestDocumentsAreScopedToIssueTasks(t *testing.T) {
	t.Parallel()
	// Chat and quick-create runs have no issue to carry documents for. If the
	// section ever appeared there it would be naming another issue's artefacts.
	chat := TaskContextForEnv{
		ChatSessionID: "77777777-8888-9999-aaaa-bbbbbbbbbbbb",
		IssueDocs:     []IssueDocForEnv{{ID: "x", Title: "leaked"}},
	}
	if strings.Contains(buildMetaSkillContent("claude", chat), "## Issue Documents") {
		t.Error("a chat run must not render Issue Documents")
	}
}
