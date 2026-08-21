package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica worktree ready — what needs a decision, computed rather than read.
//
// The states worth acting on are the ones no column shows on its own: a
// checkout with uncommitted work, a tree whose facts were never measured, a
// branch that landed while its cards stayed open. That last one is the loop
// this ledger exists to close and neither side can see alone — the tree does
// not know a card's status and the card does not know the branch merged.
//
// This is what `ready.sh` did in the ledger it replaces, minus the shell.

var worktreeReadyCmd = &cobra.Command{
	Use:   "ready",
	Short: "What needs a decision across every tree",
	Long: `What needs a decision across every tree.

Read-only, and not a gate: an empty list is not an error, it means nothing is
waiting. Ordered by what it costs to leave alone — work that can be lost
outranks work that is merely unrecorded.`,
	Args: cobra.NoArgs,
	RunE: runWorktreeReady,
}

func init() {
	worktreeCmd.AddCommand(worktreeReadyCmd)
	worktreeReadyCmd.Flags().String("output", "table", "Output format: table or json")
}

type readyItem struct {
	Tree    string   `json:"tree"`
	Reasons []string `json:"reasons"`
	Issues  []string `json:"issues,omitempty"`
	Branch  string   `json:"branch"`
}

// Ordered by cost of neglect, not by severity in the abstract.
var readyOrder = []string{
	"blocked",
	"uncommitted",
	"merged_open_cards",
	"never_measured",
	"unclaimed",
}

var readyLabels = map[string]string{
	"blocked":           "阻塞",
	"uncommitted":       "有未提交改动",
	"merged_open_cards": "已合入但卡还开着",
	"never_measured":    "从未实测",
	"unclaimed":         "没人认领",
}

func runWorktreeReady(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var trees struct {
		Worktrees []worktreeRow `json:"worktrees"`
	}
	if err := client.GetJSON(ctx, "/api/worktrees?all=true", &trees); err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}

	items := make([]readyItem, 0, len(trees.Worktrees))
	for _, tree := range trees.Worktrees {
		if tree.Status == "archived" {
			continue
		}
		var reasons []string
		if tree.Status == "blocked" {
			reasons = append(reasons, "blocked")
		}
		// The only state here that can actually be lost.
		if tree.Dirty {
			reasons = append(reasons, "uncommitted")
		}
		var openCards []string
		if tree.Status == "merged" {
			openCards = openCardsFor(ctx, client, tree)
			if len(openCards) > 0 {
				reasons = append(reasons, "merged_open_cards")
			}
		}
		// Never measured means every fact on the row is somebody's claim.
		if tree.VerifiedAt == nil {
			reasons = append(reasons, "never_measured")
		}
		if tree.Session.Agent == "" && tree.Status == "active" {
			reasons = append(reasons, "unclaimed")
		}
		if len(reasons) == 0 {
			continue
		}
		sort.Slice(reasons, func(i, j int) bool {
			return indexOfReason(reasons[i]) < indexOfReason(reasons[j])
		})
		items = append(items, readyItem{
			Tree: tree.Name, Reasons: reasons, Issues: openCards, Branch: tree.Branch,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return indexOfReason(items[i].Reasons[0]) < indexOfReason(items[j].Reasons[0])
	})

	if output, _ := cmd.Flags().GetString("output"); output != "table" {
		return cli.PrintJSON(os.Stdout, map[string]any{"ready": items})
	}
	if len(items) == 0 {
		// Not an error: an empty list is the good state.
		fmt.Fprintln(os.Stderr, "Nothing is waiting.")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		labels := make([]string, 0, len(item.Reasons))
		for _, reason := range item.Reasons {
			labels = append(labels, readyLabels[reason])
		}
		rows = append(rows, []string{
			item.Tree,
			dashIfEmpty(item.Branch),
			strings.Join(labels, " / "),
			dashIfEmpty(strings.Join(item.Issues, " ")),
		})
	}
	cli.PrintTable(os.Stdout, []string{"NAME", "BRANCH", "NEEDS", "CARDS"}, rows)
	return nil
}

func indexOfReason(reason string) int {
	for i, candidate := range readyOrder {
		if candidate == reason {
			return i
		}
	}
	return len(readyOrder)
}

// openCardsFor returns the identifiers of cards bound to this tree that are
// still open. Advisory: a lookup that fails reports no open cards rather than
// inventing them, since the alternative is a ready list nobody can trust.
func openCardsFor(ctx context.Context, client *cli.APIClient, tree worktreeRow) []string {
	var resp struct {
		Entries []worktreeEntryRow `json:"entries"`
	}
	path := worktreePath(tree.Name) + "/entries?limit=200"
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var open []string
	for _, entry := range resp.Entries {
		if entry.IssueID == nil || seen[*entry.IssueID] {
			continue
		}
		seen[*entry.IssueID] = true
		var issue struct {
			Identifier string `json:"identifier"`
			Status     string `json:"status"`
		}
		if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(*entry.IssueID), &issue); err != nil {
			continue
		}
		if issue.Status != "done" && issue.Status != "cancelled" {
			open = append(open, issue.Identifier)
		}
	}
	sort.Strings(open)
	return open
}
