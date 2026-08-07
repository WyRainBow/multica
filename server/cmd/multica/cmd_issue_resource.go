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

// multica issue resource {list|add|rename|remove} — external pages attached to
// an issue.
//
// A design doc, a meeting note, a vendor page: anything whose home is outside
// Multica but which belongs next to this piece of work. Distinct from an
// attachment (a file we store) and from a PR link (written by the webhook and
// only ever a PR).

var issueResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Work with the external links attached to an issue",
	Long: `Work with the external links attached to an issue.

A resource is a URL plus a title you write yourself. The title is not fetched
from the page: the documents most worth attaching — Feishu docs, internal wikis
— return a login page to an anonymous fetch, so a fetched title would be wrong
exactly where it matters.`,
}

var issueResourceListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List the resources attached to an issue",
	Args:  exactArgs(1),
	RunE:  runIssueResourceList,
}

var issueResourceAddCmd = &cobra.Command{
	Use:   "add <issue-id> <url>",
	Short: "Attach an external link to an issue",
	Args:  exactArgs(2),
	RunE:  runIssueResourceAdd,
}

var issueResourceRenameCmd = &cobra.Command{
	Use:   "rename <issue-id> <resource-id> <title>",
	Short: "Change a resource's title",
	Args:  exactArgs(3),
	RunE:  runIssueResourceRename,
}

var issueResourceRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id> <resource-id>",
	Short: "Detach a resource from an issue",
	Long: `Detach a resource from an issue.

Removes the link, never the page it points at.`,
	Args: exactArgs(2),
	RunE: runIssueResourceRemove,
}

func init() {
	issueResourceCmd.AddCommand(issueResourceListCmd)
	issueResourceCmd.AddCommand(issueResourceAddCmd)
	issueResourceCmd.AddCommand(issueResourceRenameCmd)
	issueResourceCmd.AddCommand(issueResourceRemoveCmd)

	issueResourceListCmd.Flags().String("output", "table", "Output format: table or json")
	issueResourceListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issueResourceAddCmd.Flags().String("title", "",
		"Label for the row. Optional — without one the list shows the host and path.")
	issueResourceAddCmd.Flags().String("output", "json", "Output format: table or json")
	issueResourceRenameCmd.Flags().String("output", "json", "Output format: table or json")

	issueCmd.AddCommand(issueResourceCmd)
}

type issueResourceRow struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// resourceLabel is what a row shows when nobody typed a title.
//
// The host alone is not enough — three Feishu docs would render as three
// identical rows — so the path comes with it, clipped. Falls back to the raw
// string if it will not parse, which cannot happen through this CLI but can
// through an older server.
func resourceLabel(row issueResourceRow) string {
	if title := strings.TrimSpace(row.Title); title != "" {
		return title
	}
	parsed, err := url.Parse(row.URL)
	if err != nil || parsed.Host == "" {
		return row.URL
	}
	label := parsed.Host + parsed.Path
	runes := []rune(strings.TrimSuffix(label, "/"))
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return string(runes)
}

func runIssueResourceList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var resp struct {
		Resources []issueResourceRow `json:"resources"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/resources", &resp); err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if len(resp.Resources) == 0 {
		fmt.Fprintf(os.Stderr, "Issue %s has no resources.\n", issueRef.Display)
		return nil
	}

	full, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "URL", "ADDED"}
	rows := make([][]string, 0, len(resp.Resources))
	for _, resource := range resp.Resources {
		rows = append(rows, []string{
			displayID(resource.ID, full),
			resourceLabel(resource),
			resource.URL,
			shortTimestamp(resource.CreatedAt),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueResourceAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{"url": strings.TrimSpace(args[1])}
	if title, _ := cmd.Flags().GetString("title"); strings.TrimSpace(title) != "" {
		body["title"] = strings.TrimSpace(title)
	}

	var resource issueResourceRow
	path := "/api/issues/" + issueRef.ID + "/resources"
	if err := client.PostJSON(ctx, path, body, &resource); err != nil {
		return fmt.Errorf("add resource: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resource attached to issue %s.\n", issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, resource)
}

func runIssueResourceRename(cmd *cobra.Command, args []string) error {
	title := strings.TrimSpace(args[2])
	if title == "" {
		return fmt.Errorf("title must not be blank; use `issue resource remove` to detach it instead")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var resource issueResourceRow
	path := "/api/issues/" + issueRef.ID + "/resources/" + args[1]
	if err := client.PutJSON(ctx, path, map[string]any{"title": title}, &resource); err != nil {
		return fmt.Errorf("rename resource: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resource renamed to %q.\n", title)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, resource)
}

func runIssueResourceRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	path := "/api/issues/" + issueRef.ID + "/resources/" + args[1]
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove resource: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resource detached from issue %s.\n", issueRef.Display)
	return nil
}
