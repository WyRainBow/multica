package execenv

import (
	"strings"
	"testing"
)

// A decision that still holds belongs with the state of record; one that was
// replaced belongs with the history that produced it. Rendering both the same
// way makes the reader do the sorting, which is the work this section exists
// to have already done.

func decisionCtx(docs []IssueDocForEnv, decisions []IssueDecisionForEnv, open []IssueOpenQuestionForEnv) TaskContextForEnv {
	return TaskContextForEnv{
		IssueID:            "55555555-6666-7777-8888-999999999999",
		IssueDocs:          docs,
		IssueDecisions:     decisions,
		IssueOpenQuestions: open,
	}
}

func TestALiveDecisionRendersOnceAndUnderTheStateOfRecord(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", decisionCtx(
		[]IssueDocForEnv{
			{ID: "spec", Title: "spec", Kind: "K/spec", Current: true},
			{ID: "d2", Title: "D2", Kind: "K/decisions/D2"},
		},
		[]IssueDecisionForEnv{{ID: "D2", DocID: "d2", Question: "载体", Summary: "独立卡", DecidedBy: "用户"}},
		nil,
	))

	if !strings.Contains(out, "#### 现在算数的决策") {
		t.Fatalf("live decisions must render under the state of record:\n%s", out)
	}
	if !strings.Contains(out, "**D2** 载体 → 独立卡") {
		t.Error("the one-liner must carry the question and the answer")
	}
	// Naming the decider is half of what a decision record answers.
	if !strings.Contains(out, "（用户 定）") {
		t.Error("the decider must be named")
	}
	// Listing its card again under history would show one decision twice and
	// leave the reader deciding which entry to trust.
	if strings.Count(out, "K/decisions/D2") != 0 {
		t.Errorf("a live decision's card must not also appear in history:\n%s", out)
	}
}

func TestASupersededDecisionMovesToHistory(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", decisionCtx(
		[]IssueDocForEnv{
			{ID: "spec", Title: "spec", Kind: "K/spec", Current: true},
			{ID: "d1", Title: "D1", Kind: "K/decisions/D1"},
			{ID: "d2", Title: "D2", Kind: "K/decisions/D2"},
		},
		[]IssueDecisionForEnv{
			{ID: "D1", DocID: "d1", Question: "载体", Summary: "metadata", Superseded: true, SupersededBy: "D2"},
			{ID: "D2", DocID: "d2", Question: "载体", Summary: "独立卡"},
		},
		nil,
	))
	current := out[strings.Index(out, "#### 现在算数的决策"):strings.Index(out, "### History")]
	if strings.Contains(current, "**D1**") {
		t.Error("a superseded decision must not read as current")
	}
	if !strings.Contains(out, "K/decisions/D1") {
		t.Error("a superseded decision must still be findable in history")
	}
}

func TestOpenQuestionsAreShownAndNotForTheRunToAnswer(t *testing.T) {
	t.Parallel()
	// An unanswered question is where the work will stall and where a run is
	// most likely to invent an answer nobody agreed to.
	out := buildMetaSkillContent("claude", decisionCtx(
		[]IssueDocForEnv{{ID: "spec", Title: "spec", Kind: "K/spec", Current: true}},
		nil,
		[]IssueOpenQuestionForEnv{{Ref: "D2#1", Question: "命令叫什么", RaisedBy: "D2"}},
	))
	if !strings.Contains(out, "#### 未决") {
		t.Fatalf("open questions must be surfaced:\n%s", out)
	}
	if !strings.Contains(out, "`D2#1` 命令叫什么") {
		t.Error("an open question must carry its reference so a later decision can close it")
	}
	if !strings.Contains(out, "不要自行替它们作答") {
		t.Error("the brief must say these are not the run's to answer")
	}
}

func TestAnIssueWithNoDecisionsRendersNoDecisionSections(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", decisionCtx(
		[]IssueDocForEnv{{ID: "spec", Title: "spec", Kind: "K/spec", Current: true}}, nil, nil,
	))
	for _, unwanted := range []string{"#### 现在算数的决策", "#### 未决"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%q rendered for an issue that has no decisions", unwanted)
		}
	}
}

func TestEveryDecisionSupersededLeavesNoCurrentBlockClaim(t *testing.T) {
	t.Parallel()
	// All decisions replaced and nothing new: the section must say nothing
	// rather than print an empty heading that implies a decision exists.
	out := buildMetaSkillContent("claude", decisionCtx(
		[]IssueDocForEnv{
			{ID: "spec", Title: "spec", Kind: "K/spec", Current: true},
			{ID: "d1", Title: "D1", Kind: "K/decisions/D1"},
		},
		[]IssueDecisionForEnv{{ID: "D1", DocID: "d1", Summary: "旧的", Superseded: true, SupersededBy: "D2"}},
		nil,
	))
	if strings.Contains(out, "#### 现在算数的决策") {
		t.Error("an empty decisions heading rendered with nothing under it")
	}
}
