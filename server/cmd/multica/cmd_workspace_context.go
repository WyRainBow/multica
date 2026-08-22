package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/assetmap"
	"github.com/multica-ai/multica/server/internal/cli"
)

// multica workspace context — the map a terminal session cannot otherwise see.
//
// A dispatched agent gets the workspace laid out for it: the daemon writes the
// rules, the skills and the asset index into its brief. A session someone
// starts by hand gets none of that. `instructions pull` and `skills pull`
// brought the CONTENT across, so the rules and the skill bodies are on disk —
// but nothing tells that session which cases exist, which manuals exist, or
// which other issues moved recently. An agent does not look up what it does
// not know is there.
//
// This is the index, not the content. Titles and read commands, computed fresh
// on every invocation. Nothing is written: running it and not running it leave
// the workspace identical, so an agent can call it without deciding whether it
// is allowed to.
//
// Deliberately not a skill and deliberately not cached. A skill's body is a
// file on disk, and an index on disk goes stale the moment somebody writes a
// case — the same failure a pulled copy has. The skill that belongs beside
// this holds the trigger and nothing else: it reminds an agent to run this,
// and this answers with what is true now.

const (
	// relatedIssueLimit bounds the "what else moved" hint. Past a handful the
	// agent has to filter the list, which is the relevance judgment this
	// command deliberately refuses to make on its behalf.
	relatedIssueLimit = 5
	// relatedIssueWindow bounds how far back "recently" reaches. Longer starts
	// surfacing work that has nothing to do with the task in hand.
	relatedIssueWindow = 7 * 24 * time.Hour
)

var workspaceContextCmd = &cobra.Command{
	Use:   "context [workspace-id|slug|prefix]",
	Short: "Print the workspace's asset map for this session",
	Long: `Prints what this workspace has, so a terminal session can find it.

A dispatched agent receives this in its brief. A session started by hand does
not, and 'instructions pull' / 'skills pull' bring the content across without
telling anyone what else exists.

Four sections: the team rules and whether the local copy is current, the skills
available, the workspace's own writing, and — with --issue — what that issue has
settled and what moved near it recently.

Leads, not conclusions. Nothing here says what to read; it says what exists and
how to open it. Nothing is written, so running it costs nothing and skipping it
breaks nothing.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceContext,
}

func init() {
	workspaceCmd.AddCommand(workspaceContextCmd)
	workspaceContextCmd.Flags().String("issue", "", "Also show what this issue has settled and what moved near it")
	workspaceContextCmd.Flags().String("instructions-file", "",
		"Local agent config to check the instructions copy against, e.g. ~/.claude/CLAUDE.md")
	workspaceContextCmd.Flags().String("skills-dir", "",
		"Local skills directory to check the skill mirror against, e.g. ~/.claude/skills")
}

func runWorkspaceContext(cmd *cobra.Command, args []string) error {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var ws struct {
		Slug    string `json:"slug"`
		Context string `json:"context"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Workspace %s\n\n", ws.Slug)

	writeContextRules(cmd, &b, ws.Slug, ws.Context)
	writeContextSkills(ctx, cmd, &b, client)
	writeContextAssets(ctx, &b, client)
	if issueRef := strings.TrimSpace(mustString(cmd, "issue")); issueRef != "" {
		writeContextIssue(ctx, &b, client, issueRef)
	}

	b.WriteString("---\n\n")
	b.WriteString(assetmap.SourceOfTruthNotice + "\n")
	b.WriteString("以上只是线索。要不要读、读哪一份，由你按手头任务判断；本命令不写入任何东西。\n")
	// The map covers one source. Saying so is the difference between an index
	// and a claim of completeness — and the layering rule the brief states for
	// instructions applies here too: each layer answers for its own scope.
	b.WriteString("**本命令只覆盖 Multica workspace 的资产。** 本机自有的 skills、文档与记忆不在此列，也不受 Multica 管理——它们由你的 runtime 各自发现，与这份地图是并列的两层，不是包含关系。\n")

	fmt.Print(b.String())
	return nil
}

// writeContextRules reports the rules without repeating them. The body is
// already on disk if it was pulled, and printing it again would turn an index
// into a second copy of the thing it indexes.
func writeContextRules(cmd *cobra.Command, b *strings.Builder, slug, instructions string) {
	b.WriteString("## 团队规则\n\n")
	body := strings.TrimSpace(instructions)
	if body == "" {
		b.WriteString("本 workspace 尚未设置 Instructions。\n\n")
		return
	}
	firstLine := strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
	fmt.Fprintf(b, "%s（%d 字符）。正文不在这里重复——本机副本用 `multica workspace instructions pull` 取。\n",
		firstLine, len([]rune(body)))

	if target := strings.TrimSpace(mustString(cmd, "instructions-file")); target != "" {
		// Whether the local copy is current is the one thing the file itself
		// cannot answer, and a stale copy reads exactly like a fresh one.
		if state := describeInstructionsCopy(target, instructions); state != "" {
			fmt.Fprintf(b, "本机副本 %s：%s\n", target, state)
		}
	}
	b.WriteString("\n")
}

func describeInstructionsCopy(target, live string) string {
	expanded, err := expandUserPath(target)
	if err != nil {
		return ""
	}
	existing, err := readFileAllowingMissing(expanded)
	if err != nil {
		return "读不到"
	}
	if extractInstructionsBlock(existing) == "" {
		return "**未拉取**（`workspace instructions pull --file " + target + "`）"
	}
	_, localPrint := blockProvenance(existing)
	switch {
	case localPrint == "":
		return "**版本不可判定**，重拉一次即可（早于溯源记录）"
	case localPrint != instructionsFingerprint(live):
		return "**已过期**（本机 " + localPrint + "，workspace " + instructionsFingerprint(live) + "）"
	default:
		return "current"
	}
}

func writeContextSkills(ctx context.Context, cmd *cobra.Command, b *strings.Builder, client *cli.APIClient) {
	skills, err := fetchWorkspaceSkills(ctx, client)
	if err != nil || len(skills) == 0 {
		return
	}
	b.WriteString("## 可用 skill\n\n")
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })

	b.WriteString("**来自 workspace**（`multica workspace skills pull` 镜像到本机）\n\n")
	for _, s := range skills {
		desc := strings.Join(strings.Fields(effectiveSkillDescriptionForContext(s)), " ")
		if desc == "" {
			fmt.Fprintf(b, "- **%s**\n", s.Name)
			continue
		}
		fmt.Fprintf(b, "- **%s** — %s\n", s.Name, clipContextLine(desc, 160))
	}
	b.WriteString("\n")

	// Naming the machine's own skills matters more than listing them. A map
	// that shows only the workspace half reads as the whole world, and an
	// agent that believes it has seen everything stops looking — while its
	// runtime has been loading those local skills the entire time.
	if dir := strings.TrimSpace(mustString(cmd, "skills-dir")); dir != "" {
		mirrored, native := splitLocalSkills(dir)
		fmt.Fprintf(b, "本机 %s：镜像 %d 个（带溯源标记，`workspace skills status --dir %s` 查新鲜度）",
			dir, mirrored, dir)
		if total, shown := nativeSkillSummary(native); total > 0 {
			fmt.Fprintf(b, "，**另有 %d 个本机自有**：%s", total, strings.Join(shown, "、"))
		}
		b.WriteString("\n\n")
	}
	b.WriteString("本机自有的 skill 由你的 runtime 自行发现与加载，**不受 Multica 管理，本命令也不列全**。\n\n")
}

// splitLocalSkills separates what this tool mirrored from what the machine
// owns. The sidecar written by `workspace skills pull` is the only reliable
// tell: a directory without one was put there by a person, and treating it as
// ours is how a local skill gets overwritten or silently claimed.
func splitLocalSkills(dir string) (mirrored int, native []string) {
	root, err := expandUserPath(dir)
	if err != nil {
		return 0, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if _, ours := readPulledSkillMarker(filepath.Join(root, name)); ours {
			mirrored++
			continue
		}
		native = append(native, name)
	}
	sort.Strings(native)
	return mirrored, native
}

// nativeSkillSummary names a few and counts the rest. Past a handful the list
// stops being a boundary marker and becomes an index this command has no
// business maintaining — but the COUNT has to be the real one, or the boundary
// it is drawing is drawn in the wrong place.
func nativeSkillSummary(native []string) (total int, shown []string) {
	total = len(native)
	if total <= 8 {
		return total, native
	}
	return total, append(append([]string{}, native[:8]...), fmt.Sprintf("…另 %d 个", total-8))
}

// effectiveSkillDescriptionForContext prefers the body's own frontmatter, the
// way the on-disk SKILL.md does. Showing the stored field instead would name
// the same skill two different ways in one session — a divergence this
// repository has already paid for once.
func effectiveSkillDescriptionForContext(s workspaceSkill) string {
	if desc := frontmatterDescription(s.Content); desc != "" {
		return desc
	}
	return s.Description
}

func frontmatterDescription(content string) string {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	rest := trimmed[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(key) == "description" {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

func writeContextAssets(ctx context.Context, b *strings.Builder, client *cli.APIClient) {
	groups := fetchWorkspaceAssetGroups(ctx, client)
	if !assetmap.HasAny(groups) {
		return
	}
	b.WriteString("## Workspace 资产\n\n")
	assetmap.RenderGroups(b, "本 workspace 写下来的东西。用 `"+assetmap.ReadCommand+"` 打开需要的那份：", groups)
}

// contextAssetFolders mirrors the server's workspaceAssetFolders. Duplicated
// rather than shared: the two live in different binaries, and a shared list
// would tie a CLI release to a server one for a set either may extend alone.
var contextAssetFolders = []struct{ kind, label, when string }{
	{"AgentWiki/cases_案例", "经验案例", "撞到类似问题时先看这里，每条都是一次真实的坑与它的解法"},
	{"指南", "指南", "长期有效的做法与边界"},
	{"AgentWiki/playbooks_手册", "手册", "某一类工作的完整打法"},
}

func fetchWorkspaceAssetGroups(ctx context.Context, client *cli.APIClient) []assetmap.Group {
	var groups []assetmap.Group
	for _, folder := range contextAssetFolders {
		// The list endpoint answers with an envelope, not a bare array. Decoding
		// it as an array yields no error and no rows — a section that silently
		// vanishes, which is how this shipped wrong the first time.
		var resp struct {
			Cards []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Kind  string `json:"kind"`
			} `json:"cards"`
			Total int `json:"total"`
		}
		path := "/api/cards?kind=" + urlQueryEscape(folder.kind) + "&limit=25"
		if err := client.GetJSON(ctx, path, &resp); err != nil || len(resp.Cards) == 0 {
			// One unreadable folder must not cost the others.
			continue
		}
		docs := make([]assetmap.Doc, 0, len(resp.Cards))
		for _, r := range resp.Cards {
			docs = append(docs, assetmap.Doc{ID: r.ID, Title: r.Title, Kind: r.Kind})
		}
		sort.SliceStable(docs, func(i, j int) bool { return docs[i].Kind < docs[j].Kind })
		groups = append(groups, assetmap.Group{
			Label: folder.label, When: folder.when, Docs: docs,
			Dropped: resp.Total - len(docs),
		})
	}
	return groups
}

func writeContextIssue(ctx context.Context, b *strings.Builder, client *cli.APIClient, ref string) {
	issue, err := resolveIssueRef(ctx, client, ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s could not be resolved (%v); skipping the issue section.\n", ref, err)
		return
	}
	key := strings.TrimSpace(issue.Display)
	if key == "" {
		key = issue.ID
	}
	fmt.Fprintf(b, "## %s 的线索\n\n", key)

	docs, err := fetchIssueDocs(ctx, client, issue.ID)
	if err == nil && len(docs) > 0 {
		var live, history []docRow
		for _, d := range docs {
			if _, ok := liveDocSuffix(d.Kind); ok {
				live = append(live, d)
				continue
			}
			history = append(history, d)
		}
		if len(live) > 0 {
			b.WriteString("**当前有效**\n\n")
			for _, d := range live {
				fmt.Fprintf(b, "- %s — `%s` — `%s`\n", d.Title, d.Kind, d.ID)
			}
			b.WriteString("\n")
		}
		if len(history) > 0 {
			fmt.Fprintf(b, "**历史** %d 份（轮次、决策、快照）——`multica wiki list --issue %s` 列全。\n\n",
				len(history), key)
		}
	}

	// What else moved lately. Mechanical only: recency, nothing semantic. A
	// plausible-but-wrong "related" is worse than none, because it gets
	// followed — so the command lists what is near in time and leaves the
	// judgment where it belongs.
	if related := fetchRecentIssues(ctx, client, key); len(related) > 0 {
		fmt.Fprintf(b, "**最近 %d 天动过的其他卡**（只按时间，未做相关性判断）\n\n", int(relatedIssueWindow.Hours()/24))
		for _, r := range related {
			fmt.Fprintf(b, "- %s %s\n", r.Identifier, clipContextLine(r.Title, 60))
		}
		b.WriteString("\n")
	}
}

type recentIssue struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	UpdatedAt  string `json:"updated_at"`
}

func fetchRecentIssues(ctx context.Context, client *cli.APIClient, excludeKey string) []recentIssue {
	var rows []recentIssue
	if err := client.GetJSON(ctx, "/api/issues?limit=40", &rows); err != nil {
		return nil
	}
	cutoff := time.Now().Add(-relatedIssueWindow)
	var out []recentIssue
	for _, r := range rows {
		if r.Identifier == excludeKey {
			continue
		}
		updated, err := time.Parse(time.RFC3339, r.UpdatedAt)
		if err != nil || updated.Before(cutoff) {
			continue
		}
		out = append(out, r)
		if len(out) == relatedIssueLimit {
			break
		}
	}
	return out
}

func clipContextLine(s string, limit int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~', r == '/':
			b.WriteRune(r)
		default:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}

// RenderDecisionCardNoop is a minimal body used by tests that only need a
// skill directory to exist on disk.
func RenderDecisionCardNoop() string { return "---\nname: x\n---\n\nbody" }
