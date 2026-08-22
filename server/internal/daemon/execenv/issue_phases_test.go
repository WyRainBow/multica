package execenv

import (
	"strings"
	"testing"
)

// The brief told the agent to file comments at the station they belong to
// without ever saying which stations existed, so the instruction could not be
// obeyed. These tests pin that the route is shown, that "where are we" is
// answerable from it, and that an issue with no route is untouched.

func issueCtxWithPhases(phases ...IssuePhaseForEnv) TaskContextForEnv {
	return TaskContextForEnv{
		IssueID:     "55555555-6666-7777-8888-999999999999",
		IssuePhases: phases,
	}
}

func TestTheRouteAndTheCurrentStationAreBothVisible(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", issueCtxWithPhases(
		IssuePhaseForEnv{Name: "需求梳理", Entered: true, Completed: true},
		IssuePhaseForEnv{Name: "方案评审", Entered: true, Completed: true},
		IssuePhaseForEnv{Name: "代码评审", Entered: true},
		IssuePhaseForEnv{Name: "测试验收"},
	))

	for _, want := range []string{
		"## Issue Phases",
		"需求梳理 — done",
		"**代码评审 — CURRENT**",
		"测试验收 — not reached",
		// Naming the station is useless without the flag that files against it.
		"--phase",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief is missing %q", want)
		}
	}

	// Track order is the whole point: stations read out of sequence say
	// nothing about where the work is going next.
	planIdx := strings.Index(out, "方案评审")
	codeIdx := strings.Index(out, "代码评审")
	if planIdx < 0 || codeIdx < 0 || planIdx > codeIdx {
		t.Error("stations must render in route order")
	}
}

func TestARouteNobodyWalkedSaysWhatToDo(t *testing.T) {
	t.Parallel()
	// Every issue ships with its stations already listed and none entered.
	// Rendering four "not reached" lines and stopping reads as "no station
	// applies to me", which is how comments end up unfiled.
	out := buildMetaSkillContent("claude", issueCtxWithPhases(
		IssuePhaseForEnv{Name: "需求梳理"},
		IssuePhaseForEnv{Name: "方案评审"},
	))
	if !strings.Contains(out, "No station is open") {
		t.Errorf("an unwalked route must say what to do:\n%s", out)
	}
	if strings.Contains(out, "CURRENT") {
		t.Error("no station was entered, so none may be marked current")
	}
}

func TestMoreThanOneOpenStationIsReportedHonestly(t *testing.T) {
	t.Parallel()
	// Nothing stops two stations being open at once. Picking one to call
	// "the" current station would be inventing an answer.
	out := buildMetaSkillContent("claude", issueCtxWithPhases(
		IssuePhaseForEnv{Name: "代码评审", Entered: true},
		IssuePhaseForEnv{Name: "测试验收", Entered: true},
	))
	if got := strings.Count(out, "CURRENT"); got != 2 {
		t.Errorf("both open stations must be marked current, got %d", got)
	}
	if strings.Contains(out, "No station is open") {
		t.Error("stations are open; the fallback line must not appear")
	}
}

func TestAnIssueWithNoRouteRendersExactlyAsBefore(t *testing.T) {
	t.Parallel()
	bare := TaskContextForEnv{IssueID: "55555555-6666-7777-8888-999999999999"}
	before := buildMetaSkillContent("claude", bare)

	if strings.Contains(before, "## Issue Phases") {
		t.Error("an issue predating phases must not get the section")
	}

	// An empty (not nil) slice must be byte-identical to having none, or the
	// prompt-cache prefix churns for a claim that carried no information.
	empty := bare
	empty.IssuePhases = []IssuePhaseForEnv{}
	if got := buildMetaSkillContent("claude", empty); got != before {
		t.Error("an empty route changed the brief; it must be byte-identical to having none")
	}
}

func TestPhasesAreScopedToIssueTasks(t *testing.T) {
	t.Parallel()
	chat := TaskContextForEnv{
		ChatSessionID: "77777777-8888-9999-aaaa-bbbbbbbbbbbb",
		IssuePhases:   []IssuePhaseForEnv{{Name: "leaked", Entered: true}},
	}
	if strings.Contains(buildMetaSkillContent("claude", chat), "## Issue Phases") {
		t.Error("a chat run has no issue and must not render a route")
	}
}

func TestABlankStationNameIsSkippedNotRendered(t *testing.T) {
	t.Parallel()
	// A nameless station cannot be filed against, so a bullet for it is an
	// instruction the agent cannot follow.
	out := buildMetaSkillContent("claude", issueCtxWithPhases(
		IssuePhaseForEnv{Name: "  "},
		IssuePhaseForEnv{Name: "代码评审", Entered: true},
	))
	if strings.Contains(out, "-  — ") {
		t.Errorf("a blank station name rendered as a bullet:\n%s", out)
	}
	if !strings.Contains(out, "**代码评审 — CURRENT**") {
		t.Error("the named station must still render")
	}
}
