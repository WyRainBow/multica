package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue pr {list|add|remove} — review requests recorded against a card.
//
// This workspace talks to GitHub and GitLab and integrates with neither, so
// there is nothing to ask about a request's state and nothing is claimed about
// it. What gets recorded is the URL and who recorded it, which is the pair that
// makes the link actionable: one says where the review is, the other says who
// to ask about it.
//
// Distinct from `issue resource`, which is any external page. A card usually
// grows several resources and at most a couple of reviews, and the review is
// the one a reader looks for by name.

var issuePRCmd = &cobra.Command{
	Use:   "pr",
	Short: "Work with the review requests recorded against an issue",
	Long: `Work with the review requests recorded against an issue.

A link plus who recorded it. Nothing is fetched: no state, no title, no check
results. This workspace does not integrate with GitHub or GitLab, and a status
nobody can verify is worse on a card than no status at all.

The link is stored on the card's progress account, next to its branch and its
session, so everything about where this card's code lives reads from one place.`,
}

var issuePRListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List the review requests recorded against an issue",
	Args:  exactArgs(1),
	RunE:  runIssuePRList,
}

var issuePRAddCmd = &cobra.Command{
	Use:   "add <issue-id> <url>",
	Short: "Record a review request against an issue",
	Long: `Record a review request against an issue.

The same URL recorded twice is the same review, so a repeat is returned as it
stands rather than added again — which makes this safe to run from a wrap-up
script that cannot remember whether it already did.`,
	Args: exactArgs(2),
	RunE: runIssuePRAdd,
}

var issuePRRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id> <link-id>",
	Short: "Remove a recorded review request",
	Long: `Remove a recorded review request.

Removes the link, never the review it points at. A pasted URL is a pointer, and
a wrong pointer is worth deleting rather than annotating.`,
	Args: exactArgs(2),
	RunE: runIssuePRRemove,
}

func init() {
	issuePRCmd.AddCommand(issuePRListCmd)
	issuePRCmd.AddCommand(issuePRAddCmd)
	issuePRCmd.AddCommand(issuePRRemoveCmd)

	issuePRListCmd.Flags().String("output", "table", "Output format: table or json")
	issuePRListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issuePRAddCmd.Flags().String("title", "",
		"Label for the row. Optional — without one the list shows the host and path.")
	issuePRAddCmd.Flags().String("output", "json", "Output format: table or json")

	issueCmd.AddCommand(issuePRCmd)
}

type issuePRLinkRow struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	AddedBy     string `json:"added_by"`
	AddedByType string `json:"added_by_type"`
	AddedAt     string `json:"added_at"`
}

// prLinkLabel is what a row shows when nobody typed a title. Host plus path,
// clipped: the host alone would render three reviews on one forge as three
// identical rows.
func prLinkLabel(row issuePRLinkRow) string {
	if title := strings.TrimSpace(row.Title); title != "" {
		return title
	}
	parsed, err := url.Parse(row.URL)
	if err != nil || parsed.Host == "" {
		return row.URL
	}
	label := strings.TrimSuffix(parsed.Host+parsed.Path, "/")
	runes := []rune(label)
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return label
}

func issuePRPath(issueID string) string {
	return "/api/issues/" + url.PathEscape(issueID) + "/pr-links"
}

func runIssuePRList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Links []issuePRLinkRow `json:"pr_links"`
	}
	if err := client.GetJSON(ctx, issuePRPath(args[0]), &resp); err != nil {
		return fmt.Errorf("list review links: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output != "table" {
		return cli.PrintJSON(os.Stdout, resp.Links)
	}
	if len(resp.Links) == 0 {
		fmt.Fprintln(os.Stderr, "No review requests recorded on this card.")
		return nil
	}
	full, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "REVIEW", "RECORDED BY", "WHEN"}
	rows := make([][]string, 0, len(resp.Links))
	for _, link := range resp.Links {
		rows = append(rows, []string{
			displayID(link.ID, full),
			clip(prLinkLabel(link), 56),
			dashIfEmpty(link.AddedBy),
			shortTimestamp(link.AddedAt),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssuePRAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"url": strings.TrimSpace(args[1])}
	if title, _ := cmd.Flags().GetString("title"); strings.TrimSpace(title) != "" {
		body["title"] = strings.TrimSpace(title)
	}

	var link issuePRLinkRow
	if err := client.PostJSON(ctx, issuePRPath(args[0]), body, &link); err != nil {
		return fmt.Errorf("record review link: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Recorded on %s.\n", args[0])

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, link)
}

func runIssuePRRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, issuePRPath(args[0])+"/"+url.PathEscape(args[1])); err != nil {
		return fmt.Errorf("remove review link: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Removed from %s.\n", args[0])
	return nil
}
