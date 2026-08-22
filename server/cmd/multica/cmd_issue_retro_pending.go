package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/assetmap"
	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue retro-pending — which finished cards still have a retro owing.
//
// `issue status` already prints a nudge when a card reaches its end, and the
// nudge has the same weakness as everything else here: it speaks to whoever
// happened to run the command. This is the list a daily patrol can act on
// without anyone being present.
//
// Two filters do the real work, and the second one matters more than it looks.
//
// Dedupe is the simple version: a card that already carries a retro artefact
// is done, whatever that artefact says. Nothing tries to decide whether the
// existing one is good enough.
//
// The second filter is a precondition, not a quality bar. A retro reconstructs
// a run from the worktree ledger, the round documents and the card's own spec.
// A card with none of those has nothing to reconstruct, and running one
// against it produces a draft that says nothing, which someone then has to
// read and delete. This workspace closed 58 cards in a week and only a handful
// went through a review round; without this filter the first patrol would
// dispatch dozens of runs and fill the drafts folder with noise, which is a
// faster way to make the drafts folder worthless than never writing to it.

// retroPendingWindow is how far back a daily patrol looks. One day matches the
// cadence: a card that closed yesterday is the one still fresh enough that
// reconstructing it is cheap.
const retroPendingWindow = 24 * time.Hour

// retroPendingLimit caps one patrol's worth. A cap is what keeps a first run,
// or a run after the patrol was down for a week, from turning into a fleet of
// agent dispatches nobody asked for.
const retroPendingLimit = 3

var issueRetroPendingCmd = &cobra.Command{
	Use:   "retro-pending",
	Short: "Finished cards that have no retro yet and enough record to write one",
	Long: `Finished cards that have no retro yet and enough record to write one.

Read-only. An empty list is the good state, not an error.

A card qualifies when it reached done or cancelled inside the window, carries
no retro artefact already, and has something for a retro to read — a round
document or a worktree ledger entry. A card with neither has no run to
reconstruct, and a retro against it writes a page that says nothing.`,
	Args: cobra.NoArgs,
	RunE: runIssueRetroPending,
}

func init() {
	issueCmd.AddCommand(issueRetroPendingCmd)
	issueRetroPendingCmd.Flags().Duration("since", retroPendingWindow,
		"How far back to look at cards that finished")
	issueRetroPendingCmd.Flags().Int("limit", retroPendingLimit,
		"Most cards to report in one pass")
	issueRetroPendingCmd.Flags().String("output", "table", "Output format: table or json")
}

// retroCandidate is one card owing a retro, with the reason it qualified.
type retroCandidate struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	FinishedAt string `json:"finished_at"`
	Evidence   string `json:"evidence"` // what a retro would have to read
}

func runIssueRetroPending(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	since, _ := cmd.Flags().GetDuration("since")
	limit, _ := cmd.Flags().GetInt("limit")
	candidates, err := findRetroPending(ctx, client, since, limit)
	if err != nil {
		return err
	}

	if output, _ := cmd.Flags().GetString("output"); output != "table" {
		return cli.PrintJSON(os.Stdout, map[string]any{"pending": candidates})
	}
	if len(candidates) == 0 {
		fmt.Fprintln(os.Stderr, "没有欠着复盘的卡。")
		return nil
	}
	rows := make([][]string, 0, len(candidates))
	for _, c := range candidates {
		rows = append(rows, []string{c.Identifier, c.Status, c.Title, c.Evidence})
	}
	cli.PrintTable(os.Stdout, []string{"KEY", "STATUS", "TITLE", "有什么可读"}, rows)
	return nil
}

// findRetroPending is the scan. Separated from printing so the digest and any
// future caller ask it the same way.
func findRetroPending(
	ctx context.Context, client *cli.APIClient, since time.Duration, limit int,
) ([]retroCandidate, error) {
	if limit <= 0 {
		limit = retroPendingLimit
	}
	// The list endpoint answers with an envelope. Decoding it as a bare array
	// yields no error and no rows, and the patrol would report "nothing owing"
	// forever without once saying anything was wrong.
	var resp struct {
		Issues []struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			UpdatedAt  string `json:"updated_at"`
		} `json:"issues"`
	}
	if err := client.GetJSON(ctx,
		"/api/issues?limit=60&sort=updated_at&direction=desc&include_closed=true", &resp); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	ledger := issuesWithLedgerHistory(ctx, client)
	cutoff := time.Now().Add(-since)

	var out []retroCandidate
	for _, issue := range resp.Issues {
		if len(out) >= limit {
			break
		}
		if issue.Status != "done" && issue.Status != "cancelled" {
			continue
		}
		finished, err := time.Parse(time.RFC3339, issue.UpdatedAt)
		if err != nil || finished.Before(cutoff) {
			continue
		}
		docs, err := fetchIssueDocs(ctx, client, issue.ID)
		if err != nil {
			// A card whose documents will not load cannot be checked for a
			// retro it may already have, and dispatching a duplicate run is
			// worse than waiting for the next pass.
			continue
		}
		if hasRetroArtefact(docs) {
			continue
		}
		evidence := retroEvidence(docs, ledger[issue.ID])
		if evidence == "" {
			continue
		}
		out = append(out, retroCandidate{
			ID: issue.ID, Identifier: issue.Identifier, Title: issue.Title,
			Status: issue.Status, FinishedAt: issue.UpdatedAt, Evidence: evidence,
		})
	}
	return out, nil
}

// hasRetroArtefact is the whole dedupe: a card that already carries one is
// done. Deliberately not asking whether the existing artefact is any good —
// that judgment belongs to the person reviewing the drafts, and a machine
// second-guessing it would rewrite the same card every night.
func hasRetroArtefact(docs []docRow) bool {
	for _, doc := range docs {
		if strings.HasPrefix(doc.Kind, assetmap.CaseDraftKind) ||
			strings.HasPrefix(doc.Kind, "AgentWiki/cases_案例") {
			return true
		}
	}
	return false
}

// retroEvidence names what a retro would actually read, or "" when there is
// nothing. The string is returned rather than a bool so the caller can print
// why a card qualified — a list of keys with no reason is a list nobody trusts.
func retroEvidence(docs []docRow, ledgerEntries int) string {
	var rounds, others int
	for _, doc := range docs {
		if _, _, ok := ParseRoundKind(lastKindSegment(doc.Kind)); ok {
			rounds++
			continue
		}
		others++
	}
	var parts []string
	if rounds > 0 {
		parts = append(parts, fmt.Sprintf("%d 轮次文档", rounds))
	}
	if ledgerEntries > 0 {
		parts = append(parts, fmt.Sprintf("%d 条工作树记录", ledgerEntries))
	}
	if len(parts) == 0 {
		return ""
	}
	// Specs and decisions are worth reading but cannot carry a retro alone: a
	// card can have a spec and no run behind it at all.
	if others > 0 {
		parts = append(parts, fmt.Sprintf("%d 份其它文档", others))
	}
	return strings.Join(parts, " + ")
}

func lastKindSegment(kind string) string {
	if idx := strings.LastIndex(kind, "/"); idx >= 0 {
		return kind[idx+1:]
	}
	return kind
}

// issuesWithLedgerHistory counts recent ledger entries per card.
//
// One call for every card rather than one per card: the ledger is small and
// read whole, while a per-card lookup would be a request per candidate for a
// signal most candidates fail on.
func issuesWithLedgerHistory(ctx context.Context, client *cli.APIClient) map[string]int {
	var resp struct {
		Entries []struct {
			IssueID *string `json:"issue_id"`
		} `json:"entries"`
	}
	if err := client.GetJSON(ctx, "/api/worktree-entries?limit=200", &resp); err != nil {
		return map[string]int{}
	}
	counts := map[string]int{}
	for _, entry := range resp.Entries {
		if entry.IssueID == nil {
			continue
		}
		counts[*entry.IssueID]++
	}
	return counts
}
