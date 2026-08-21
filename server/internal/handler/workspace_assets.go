package handler

import (
	"context"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The workspace's own writing: what has been learned, and how things are done
// here. Both existed and neither reached an agent — six recorded cases and ten
// manuals that no run has ever been told about, which makes "archived
// experience feeds the next round" a claim rather than a mechanism.
//
// Titles only. A case title already says when it applies ("跳过的测试和通过的测试
// 在输出里长得一样"), so a name is enough to decide whether to open it, and
// shipping the bodies would repeat the mistake that cost 40% of a brief.
//
// Workspace-scoped, so this is the same for every task and sits in the most
// stable part of a cached prefix.

// workspaceAssetFolders are the kind prefixes worth naming in a brief, each
// with the line that says when to reach for it. Ordered: experience first,
// because "have we hit this before" is the question a run can answer cheapest
// and regrets skipping most.
var workspaceAssetFolders = []struct {
	kind  string
	label string
	when  string
}{
	{"AgentWiki/cases_案例", "经验案例", "撞到类似问题时先看这里，每条都是一次真实的坑与它的解法"},
	{"指南", "指南", "长期有效的做法与边界"},
	{"AgentWiki/playbooks_手册", "手册", "某一类工作的完整打法"},
}

// workspaceAssetsPerFolder bounds one folder's contribution. Generous relative
// to today's twenty documents, and present so a workspace that files a hundred
// cases does not quietly double every brief it ever renders.
const workspaceAssetsPerFolder = 25

// WorkspaceAssetData names one document the workspace wrote.
type WorkspaceAssetData struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind,omitempty"`
}

// WorkspaceAssetGroup is one folder's worth, with the line saying when to use it.
type WorkspaceAssetGroup struct {
	Label   string               `json:"label"`
	When    string               `json:"when,omitempty"`
	Docs    []WorkspaceAssetData `json:"docs"`
	Dropped int                  `json:"dropped,omitempty"` // said out loud; a truncated list that hides it reads as the whole set
}

// loadWorkspaceAssets reads the workspace's own writing for the brief.
//
// Best-effort by folder: one unreadable folder must not cost the others, and a
// workspace with none of them simply gets no section.
func (h *Handler) loadWorkspaceAssets(ctx context.Context, workspaceID string) []WorkspaceAssetGroup {
	wsUUID, err := util.ParseUUID(strings.TrimSpace(workspaceID))
	if err != nil {
		// A workspace id that will not parse is a reason to render no section,
		// not to take the whole claim down with it.
		return nil
	}
	var groups []WorkspaceAssetGroup
	for _, folder := range workspaceAssetFolders {
		total, err := h.Queries.CountCardsByKind(ctx, db.CountCardsByKindParams{
			WorkspaceID: wsUUID, Kind: folder.kind,
		})
		if err != nil || total == 0 {
			continue
		}
		rows, err := h.Queries.ListCardsByKind(ctx, db.ListCardsByKindParams{
			WorkspaceID: wsUUID, Kind: folder.kind,
			Limit: workspaceAssetsPerFolder, Offset: 0,
		})
		if err != nil || len(rows) == 0 {
			continue
		}
		docs := make([]WorkspaceAssetData, 0, len(rows))
		for _, row := range rows {
			docs = append(docs, WorkspaceAssetData{
				ID: uuidToString(row.ID), Title: row.Title, Kind: row.Kind,
			})
		}
		// Newest first is how they come back; by kind is how they read, since
		// the folder path groups related writing together.
		sort.SliceStable(docs, func(i, j int) bool { return docs[i].Kind < docs[j].Kind })
		groups = append(groups, WorkspaceAssetGroup{
			Label: folder.label, When: folder.when, Docs: docs,
			Dropped: int(total) - len(docs),
		})
	}
	return groups
}
