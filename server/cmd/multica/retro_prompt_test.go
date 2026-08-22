package main

import (
	"strings"
	"testing"
)

// Every case in this workspace was written by hand and none through the skill
// that exists to write them. The gap was never capability — it was that
// nothing says so at the moment it applies.

func TestFinishingTriggersTheNudgeAndWorkingDoesNot(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"done", "cancelled", "DONE", " done "} {
		if !shouldPromptRetro(status, "") {
			t.Errorf("%q ends the work and should prompt", status)
		}
	}
	for _, status := range []string{"todo", "in_progress", "in_review", "backlog", ""} {
		if shouldPromptRetro(status, "") {
			t.Errorf("%q is work in flight and must not prompt", status)
		}
	}
	// Blocked has learned something but is not finished learning it; a retro
	// there would summarise a middle.
	if shouldPromptRetro("blocked", "") {
		t.Error("blocked must not prompt: the work is not over")
	}
}

func TestOnlyTheStationsThatEndARouteTrigger(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"需求冻结", "测试验收", "测试验收 2"} {
		if !shouldPromptRetro("", phase) {
			t.Errorf("%q ends a route and should prompt", phase)
		}
	}
	for _, phase := range []string{"需求梳理", "方案评审", "代码评审", ""} {
		if shouldPromptRetro("", phase) {
			t.Errorf("%q is mid-route and must not prompt", phase)
		}
	}
}

func TestTheNudgeNamesACommandAndDisclaimsItself(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	writeRetroPrompt(&b, "COC-314", "测试验收 已收口")
	out := b.String()

	if !strings.Contains(out, "COC-314") || !strings.Contains(out, "测试验收 已收口") {
		t.Errorf("the nudge must say which issue and why:\n%s", out)
	}
	// Naming the skill beats describing the idea: an agent reading "consider a
	// retro" has to work out what that means.
	if !strings.Contains(out, "interview-retro") {
		t.Error("the nudge must name the thing to run")
	}
	// The disclaimer is the design, not politeness. A step nobody can skip
	// gets worked around, and then the rule is worse than absent because
	// people believe it is running.
	if !strings.Contains(out, "只是一句提醒") {
		t.Error("the nudge must say nothing is waiting on it")
	}
}
