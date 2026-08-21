package handler

import (
	"context"
	"strings"
	"testing"
)

// The loader reads real folders through ListCardsByKind, whose "kind is a
// path" semantics are what let a folder select everything beneath it. A test
// that stubs the query would prove nothing about that.

func seedWorkspaceAsset(t *testing.T, ctx context.Context, kind, title string) {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO card (workspace_id, issue_id, author_type, author_id, title, content, kind)
		VALUES ($1, NULL, 'member', $2, $3, 'body', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, title, kind).Scan(&id); err != nil {
		t.Fatalf("seed %q: %v", kind, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM card WHERE id = $1`, id) })
}

func TestWorkspaceAssetsLoadFromTheirFolders(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	seedWorkspaceAsset(t, ctx, "AgentWiki/cases_案例", "一次真实的坑")
	seedWorkspaceAsset(t, ctx, "指南/Agent协作", "工作树协作")
	// A folder path selects everything beneath it, which is why the manuals
	// live under a sub-path and must still come back.
	seedWorkspaceAsset(t, ctx, "AgentWiki/playbooks_手册/skills_技能", "技能编写")
	// Something outside every named folder must not be swept in.
	seedWorkspaceAsset(t, ctx, "随手记/草稿", "不该出现")

	groups := testHandler.loadWorkspaceAssets(ctx, testWorkspaceID)
	if len(groups) == 0 {
		t.Fatal("no groups loaded")
	}
	seen := map[string]string{}
	for _, g := range groups {
		for _, d := range g.Docs {
			seen[d.Title] = g.Label
		}
	}
	for title, wantLabel := range map[string]string{
		"一次真实的坑": "经验案例",
		"工作树协作":  "指南",
		"技能编写":   "手册",
	} {
		if got := seen[title]; got != wantLabel {
			t.Errorf("%q landed in %q, want %q", title, got, wantLabel)
		}
	}
	if _, leaked := seen["不该出现"]; leaked {
		t.Error("a document outside the named folders was swept in")
	}
}

func TestWorkspaceAssetsCarryNoBodies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	seedWorkspaceAsset(t, ctx, "AgentWiki/cases_案例", "标题在，正文不该在")

	groups := testHandler.loadWorkspaceAssets(ctx, testWorkspaceID)
	for _, g := range groups {
		for _, d := range g.Docs {
			if strings.Contains(d.Title, "body") || d.ID == "" {
				t.Errorf("unexpected asset shape: %+v", d)
			}
		}
	}
	// The struct has no content field at all, which is the point: a body
	// cannot leak through a shape that cannot hold one.
	if len(groups) > 0 && len(groups[0].Docs) > 0 && groups[0].Docs[0].ID == "" {
		t.Error("an asset with no id cannot be fetched, which is why it is listed")
	}
}

func TestAnUnparseableWorkspaceIDYieldsNoSection(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	// A workspace id that will not parse is a reason to render nothing, not to
	// take the whole claim down with a panic.
	if got := testHandler.loadWorkspaceAssets(context.Background(), "not-a-uuid"); got != nil {
		t.Errorf("expected no groups, got %+v", got)
	}
}
