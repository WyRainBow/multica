package execenv

import (
	"strings"
	"testing"
)

// Six recorded cases and ten manuals existed in this workspace and no run had
// ever been told about any of them. An agent cannot look up a lesson it does
// not know was written, which makes "archived experience feeds the next round"
// a claim rather than a mechanism.

func assetCtx(groups ...WorkspaceAssetGroupForEnv) TaskContextForEnv {
	return TaskContextForEnv{IssueID: "5555", WorkspaceAssets: groups}
}

func TestTheWorkspacesOwnWritingIsNamed(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", assetCtx(
		WorkspaceAssetGroupForEnv{Label: "经验案例", When: "撞到类似问题时先看这里",
			Docs: []WorkspaceAssetForEnv{{ID: "c1", Title: "跳过的测试和通过的测试长得一样"}}},
	))
	for _, want := range []string{
		"## Workspace Assets",
		"**经验案例**",
		"撞到类似问题时先看这里",
		"跳过的测试和通过的测试长得一样",
		"c1",
		"multica wiki get",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief is missing %q", want)
		}
	}
	// Titles only. A case title states its own trigger; shipping bodies is what
	// once cost 40% of a brief.
	if strings.Contains(out, "## 场景") {
		t.Error("a document body leaked into the brief")
	}
}

func TestAssetsReachEveryTaskKindNotJustIssues(t *testing.T) {
	t.Parallel()
	// These belong to the workspace, not to an issue, and a chat is where
	// "have we hit this before" gets asked most often.
	chat := TaskContextForEnv{
		ChatSessionID: "77777777-8888-9999-aaaa-bbbbbbbbbbbb",
		WorkspaceAssets: []WorkspaceAssetGroupForEnv{{Label: "经验案例",
			Docs: []WorkspaceAssetForEnv{{ID: "c1", Title: "一条案例"}}}},
	}
	if !strings.Contains(buildMetaSkillContent("claude", chat), "## Workspace Assets") {
		t.Error("a chat run must still see the workspace's own writing")
	}
}

func TestATruncatedAssetListSaysWhatItDropped(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", assetCtx(
		WorkspaceAssetGroupForEnv{Label: "指南",
			Docs: []WorkspaceAssetForEnv{{ID: "g1", Title: "第一份"}}, Dropped: 9},
	))
	if !strings.Contains(out, "…9 more") {
		t.Errorf("truncation must say how many were left out:\n%s", out)
	}
	if !strings.Contains(out, "multica wiki list --kind") {
		t.Error("a truncated list must say how to see the rest")
	}
}

func TestAWorkspaceWithNoWritingGetsNoSection(t *testing.T) {
	t.Parallel()
	bare := TaskContextForEnv{IssueID: "5555"}
	before := buildMetaSkillContent("claude", bare)
	if strings.Contains(before, "## Workspace Assets") {
		t.Error("a workspace with nothing written must not get the section")
	}
	// An empty slice must be byte-identical to none, or a claim carrying
	// `workspace_assets: []` churns the cached prefix for no information.
	empty := bare
	empty.WorkspaceAssets = []WorkspaceAssetGroupForEnv{}
	if got := buildMetaSkillContent("claude", empty); got != before {
		t.Error("an empty asset list changed the brief")
	}
}

func TestAnEmptyGroupIsSkippedRatherThanHeaded(t *testing.T) {
	t.Parallel()
	// A heading with nothing under it reads as "this folder exists and is
	// empty", which is a different claim from "this folder was not returned".
	out := buildMetaSkillContent("claude", assetCtx(
		WorkspaceAssetGroupForEnv{Label: "空的", Docs: nil},
		WorkspaceAssetGroupForEnv{Label: "有的", Docs: []WorkspaceAssetForEnv{{ID: "x", Title: "一条"}}},
	))
	if strings.Contains(out, "**空的**") {
		t.Error("an empty group rendered its heading")
	}
	if !strings.Contains(out, "**有的**") {
		t.Error("a non-empty group was dropped")
	}
}
