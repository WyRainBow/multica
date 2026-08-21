package execenv

import (
	"strings"
	"testing"
)

// Step 3 used to say one thing: read every comment, always. That was honest
// while the decided state lived nowhere else, and it meant every run rebuilt
// the same answer from the same chat history. These tests pin that it narrows
// when there are documents to narrow against, and that it does not narrow —
// or promise anything — when there are none.

func catchUpStep(out string) string {
	i := strings.Index(out, "\n3. ")
	j := strings.Index(out, "\n4. Complete the task")
	if i < 0 || j < 0 || j < i {
		return ""
	}
	return out[i:j]
}

func TestAnIssueWithDocumentsReadsThemBeforeTheComments(t *testing.T) {
	t.Parallel()
	step := catchUpStep(buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "55555555-6666-7777-8888-999999999999",
		IssueDocs: []IssueDocForEnv{{ID: "s", Title: "spec", Kind: "K/spec", Current: true}},
	}))
	if step == "" {
		t.Fatal("step 3 not found")
	}
	for _, want := range []string{
		"Read what was decided before reading what was said",
		"## Issue Documents",
		"结论计入截至",
		"--since",
	} {
		if !strings.Contains(step, want) {
			t.Errorf("step 3 is missing %q:\n%s", want, step)
		}
	}
	// Narrowing must not read as permission to skip. Comments still carry
	// what no document has yet.
	if !strings.Contains(step, "Do not skip the comment read either way") {
		t.Error("the narrowed step must still require reading comments")
	}
	// The full catch-up has to remain reachable, or an issue whose spec has no
	// conclusions loses its only path to context.
	if !strings.Contains(step, "--roots-only --summary --compact") {
		t.Error("the fallback to a full catch-up disappeared")
	}
}

func TestAnIssueWithNoDocumentsKeepsTheOriginalStep(t *testing.T) {
	t.Parallel()
	// Most issues are in this state. Pointing them at a section that is not in
	// their brief would be a dead instruction, and changing their wording at
	// all would churn a cached prompt prefix for no gain.
	step := catchUpStep(buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "55555555-6666-7777-8888-999999999999",
	}))
	if !strings.Contains(step, "this is mandatory, not optional") {
		t.Errorf("an issue with no documents must keep the unconditional step:\n%s", step)
	}
	if strings.Contains(step, "## Issue Documents") {
		t.Error("the step pointed at a section this brief does not carry")
	}
	if strings.Contains(step, "结论计入截至") {
		t.Error("the step promised a watermark that cannot exist here")
	}
}

func TestPhasesAloneAreEnoughToNarrow(t *testing.T) {
	t.Parallel()
	// An issue can have a route before anyone writes a document. Knowing which
	// station the work is at is already more than the comments alone say.
	step := catchUpStep(buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:     "55555555-6666-7777-8888-999999999999",
		IssuePhases: []IssuePhaseForEnv{{Name: "代码评审", Entered: true}},
	}))
	if !strings.Contains(step, "## Issue Phases") {
		t.Errorf("a route alone should still be read first:\n%s", step)
	}
}

func TestTheStepCountIsUnchanged(t *testing.T) {
	t.Parallel()
	// Other parts of the brief refer to steps by number ("Before step 4, run
	// ... in_progress"). Splitting step 3 in two would have silently
	// renumbered those references.
	for _, ctx := range []TaskContextForEnv{
		{IssueID: "55555555-6666-7777-8888-999999999999"},
		{IssueID: "55555555-6666-7777-8888-999999999999",
			IssueDocs: []IssueDocForEnv{{ID: "s", Kind: "K/spec", Current: true}}},
	} {
		out := buildMetaSkillContent("claude", ctx)
		if !strings.Contains(out, "Steps 1–6 — both modes") {
			t.Error("the step range changed")
		}
		if !strings.Contains(out, "Before step 4, run `multica issue status <issue-id> in_progress`.") {
			t.Error("a numbered cross-reference no longer matches the steps")
		}
	}
}
