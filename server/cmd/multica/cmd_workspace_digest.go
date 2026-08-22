package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/assetmap"
	"github.com/multica-ai/multica/server/internal/cli"
)

// multica workspace digest — one scan a day, delivered instead of offered.
//
// See digest.go for why this exists. This file is the scanning: five questions
// the workspace could already answer if someone asked, asked in one place.
//
// It computes and prints by default, and posts only with --post. A command
// that wrote to the workspace merely by being run would be unrunnable for a
// person who just wanted to look.

// digestCardTitle is the standing card every day's comment lands on. Matched
// by exact title, which is why it is a constant and not a template: a title
// that varied by date would create a card a day, and 365 of them a year is
// the outcome that made a standing card the choice in the first place.
const digestCardTitle = "每日巡逻"

var workspaceDigestCmd = &cobra.Command{
	Use:   "digest [workspace-id|slug|prefix]",
	Short: "One scan of everything that is waiting to be noticed",
	Long: `One scan of everything that is waiting to be noticed.

Five mechanisms in this workspace can each report a problem, and every one of
them has to be run by hand to say anything: branches that landed while their
cards stayed open, checkouts with work that can be lost, local copies of the
workspace rules that went stale, machine-written retros nobody has read, an
asset index that grew past scanning size.

Prints by default. With --post it writes one comment onto the standing 每日巡逻
card — and stays silent when the scan found nothing, or when today reads
exactly like the last thing it wrote.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceDigest,
}

func init() {
	workspaceCmd.AddCommand(workspaceDigestCmd)
	workspaceDigestCmd.Flags().Bool("post", false,
		"Write the scan onto the standing 每日巡逻 card as a comment")
	workspaceDigestCmd.Flags().String("output", "markdown", "Output format: markdown or json")
}

func runWorkspaceDigest(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}

	scan := digest{
		Date: time.Now().Format("2006-01-02"),
		Sections: []digestSection{
			scanMergedOpenCards(ctx, client),
			scanWorktreeTrouble(ctx, client),
			scanStaleLocalCopies(ctx, client, wsID),
			scanUnreviewedDrafts(ctx, client),
			scanAssetEntropy(ctx, client),
		},
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"date": scan.Date, "fingerprint": scan.Fingerprint(), "sections": scan.nonEmpty(),
		})
	}

	post, _ := cmd.Flags().GetBool("post")
	if !post {
		if scan.Empty() {
			fmt.Fprintln(os.Stderr, "巡逻没发现要处理的东西。")
			return nil
		}
		fmt.Print(scan.Render())
		return nil
	}
	return postDigest(ctx, client, scan)
}

// scanMergedOpenCards is the loop the worktree ledger exists to close: a
// branch that landed while the cards it was for stayed open. Neither side can
// see it alone — the tree does not know a card's status and the card does not
// know the branch merged.
func scanMergedOpenCards(ctx context.Context, client *cli.APIClient) digestSection {
	section := digestSection{
		Label: "已合入但卡还开着",
		Do:    "代码进去了，卡没收——确认一下是该收卡还是合错了。",
	}
	items, err := computeReadyItems(ctx, client)
	if err != nil {
		return section
	}
	for _, item := range items {
		if !hasReason(item.Reasons, "merged_open_cards") {
			continue
		}
		section.Items = append(section.Items, digestItem{
			Ref: item.Tree, Text: strings.Join(item.Issues, " ") + " 还开着",
		})
	}
	return section
}

// scanWorktreeTrouble reports the ledger's other states. Uncommitted work
// leads because it is the only one here that can actually be lost.
func scanWorktreeTrouble(ctx context.Context, client *cli.APIClient) digestSection {
	section := digestSection{
		Label: "工作树异常",
		Do:    "有未提交改动的先处理，那是唯一会丢的；其余按 `multica worktree ready` 看细节。",
	}
	items, err := computeReadyItems(ctx, client)
	if err != nil {
		return section
	}
	for _, item := range items {
		var labels []string
		for _, reason := range item.Reasons {
			if reason == "merged_open_cards" {
				continue // reported on its own above
			}
			labels = append(labels, readyLabels[reason])
		}
		if len(labels) == 0 {
			continue
		}
		section.Items = append(section.Items, digestItem{
			Ref: item.Tree, Text: strings.Join(labels, " / "),
		})
	}
	return section
}

// scanStaleLocalCopies asks the question `instructions status` and `skills
// status` were built to answer and that nothing ever asks them.
//
// Both were given exit codes for a shell prompt or a session-start hook. No
// such hook was ever installed, so a local copy drifting from the workspace is
// silent — which is the same failure the copies were meant to fix.
func scanStaleLocalCopies(ctx context.Context, client *cli.APIClient, wsID string) digestSection {
	section := digestSection{
		Label: "本机副本过期",
		Do:    "手动开的会话读的是这些文件，不是 workspace。重新 pull 一遍。",
	}
	targets := loadPullTargets()
	if len(targets) == 0 {
		return section
	}

	var ws struct {
		Slug    string `json:"slug"`
		Context string `json:"context"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return section
	}
	live := instructionsFingerprint(ws.Context)

	var skills []workspaceSkill
	var skillsLoaded bool
	for _, target := range targets {
		switch target.Kind {
		case pullTargetKindInstructions:
			if problem := instructionsFreshness(target.Path, ws.Slug, live); problem != "" {
				section.Items = append(section.Items, digestItem{Ref: target.Path, Text: problem})
			}
		case pullTargetKindSkills:
			if !skillsLoaded {
				skills, _ = fetchWorkspaceSkills(ctx, client)
				if builtins, err := fetchBuiltinSkills(ctx, client); err == nil {
					skills = append(skills, builtins...)
				}
				skillsLoaded = true
			}
			if len(skills) == 0 {
				continue
			}
			state := classifyLocalSkills(target.Path, skills, false)
			var parts []string
			if len(state.stale) > 0 {
				parts = append(parts, fmt.Sprintf("%d 个过期（%s）", len(state.stale), strings.Join(state.stale, ", ")))
			}
			if len(state.missing) > 0 {
				parts = append(parts, fmt.Sprintf("%d 个没拉过（%s）", len(state.missing), strings.Join(state.missing, ", ")))
			}
			if len(parts) == 0 {
				continue
			}
			section.Items = append(section.Items, digestItem{
				Ref: target.Path, Text: strings.Join(parts, "；"),
			})
		}
	}
	return section
}

// scanUnreviewedDrafts surfaces retros a machine wrote and nobody has read.
//
// They already appear in the asset map, but the map has to be asked for. This
// is the second showing that keeps a draft from quietly rotting in a folder
// that only gets read by someone already looking for it.
func scanUnreviewedDrafts(ctx context.Context, client *cli.APIClient) digestSection {
	section := digestSection{
		Label: "复盘草稿待人审",
		Do:    "读一遍，够格的把 kind 改成 AgentWiki/cases_案例，不够格的删掉。",
	}
	var resp struct {
		Cards []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"cards"`
		Total int `json:"total"`
	}
	path := "/api/cards?kind=" + urlQueryEscape(assetmap.CaseDraftKind) + "&limit=25"
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return section
	}
	for _, card := range resp.Cards {
		section.Items = append(section.Items, digestItem{Ref: card.ID, Text: card.Title})
	}
	return section
}

// scanAssetEntropy reports a folder that outgrew scanning.
//
// The warning already exists inside `workspace context`, which is a guard
// against not noticing that itself requires noticing. A list does not fail on
// a particular day; it gets gradually longer until somebody realises it has
// been noise for a while.
func scanAssetEntropy(ctx context.Context, client *cli.APIClient) digestSection {
	section := digestSection{
		Label: "资产超过好读的规模",
		Do:    fmt.Sprintf("超过 %d 条就得靠人做相关性匹配了。拆子目录，或者把过时的归档。", assetmap.ComfortableIndexSize),
	}
	for _, folder := range contextAssetFolders {
		var resp struct {
			Total int `json:"total"`
		}
		path := "/api/cards?kind=" + urlQueryEscape(folder.kind) + "&limit=1"
		if err := client.GetJSON(ctx, path, &resp); err != nil {
			continue
		}
		if resp.Total <= assetmap.ComfortableIndexSize {
			continue
		}
		section.Items = append(section.Items, digestItem{
			Text: fmt.Sprintf("%s %d 条，超过 %d", folder.label, resp.Total, assetmap.ComfortableIndexSize),
		})
	}
	return section
}

func hasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

// postDigest writes today's scan onto the standing card, or explains which of
// the two silences this was.
func postDigest(ctx context.Context, client *cli.APIClient, scan digest) error {
	issueID, key, err := findOrCreateDigestCard(ctx, client)
	if err != nil {
		return err
	}
	last, err := lastDigestComment(ctx, client, issueID)
	if err != nil {
		return err
	}
	post, reason := postDecision(scan, last)
	if !post {
		fmt.Fprintf(os.Stderr, "%s %s\n", key, reason)
		return nil
	}
	body := scan.Render()
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments",
		map[string]any{"content": body}, nil); err != nil {
		return fmt.Errorf("post the digest comment: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s 写了一条（%s，%d 类）。\n", key, scan.Fingerprint(), len(scan.nonEmpty()))
	return nil
}

// findOrCreateDigestCard returns the standing card, creating it the first
// time. Matched on exact title among open cards: the alternative is storing an
// id somewhere, which is one more thing that can point at a deleted card.
func findOrCreateDigestCard(ctx context.Context, client *cli.APIClient) (id, key string, err error) {
	var found struct {
		Issues []struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
		} `json:"issues"`
	}
	params := url.Values{}
	params.Set("q", digestCardTitle)
	params.Set("limit", "20")
	if err := client.GetJSON(ctx, "/api/issues/search?"+params.Encode(), &found); err == nil {
		for _, issue := range found.Issues {
			if strings.TrimSpace(issue.Title) == digestCardTitle {
				return issue.ID, issue.Identifier, nil
			}
		}
	}

	var created struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	}
	body := map[string]any{
		"title": digestCardTitle,
		"description": "> 每天一条巡逻结果。这张卡本身不是任务，是投递口。\n\n" +
			"下面每条评论是一天的扫描：已合入但卡还开着、工作树异常、本机副本过期、复盘草稿待人审、资产超阈。\n\n" +
			"没发现问题的日子不写，与上一条完全相同的日子也不写——" +
			"每天一条「今日无事」会把人训练成忽略这张卡，那就等于没做。",
	}
	if err := client.PostJSON(ctx, "/api/issues", body, &created); err != nil {
		return "", "", fmt.Errorf("create the standing digest card: %w", err)
	}
	fmt.Fprintf(os.Stderr, "建了常驻汇总卡 %s。\n", created.Identifier)
	return created.ID, created.Identifier, nil
}

// lastDigestComment returns the most recent body this command wrote, ignoring
// anything a person added in between. Comparing against a human reply would
// make the next scan look changed and repost work that was already delivered.
// This endpoint answers with a BARE ARRAY, not an envelope. Decoding it into
// a struct with a `comments` field yields no error and no rows — the
// suppression rule then reads every day as "nothing posted before" and posts
// a duplicate, which is exactly what it shipped doing. The sibling mistake
// (an envelope decoded as an array) cost a whole asset section two cards ago.
func lastDigestComment(ctx context.Context, client *cli.APIClient, issueID string) (string, error) {
	var comments []struct {
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}
	path := "/api/issues/" + url.PathEscape(issueID) + "/comments?full=true"
	if err := client.GetJSON(ctx, path, &comments); err != nil {
		// A comment history that will not load is not a reason to stay
		// silent: withholding a digest is the failure this prevents.
		return "", nil
	}
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt > comments[j].CreatedAt
	})
	for _, comment := range comments {
		if fingerprintOf(comment.Content) != "" {
			return comment.Content, nil
		}
	}
	return "", nil
}
