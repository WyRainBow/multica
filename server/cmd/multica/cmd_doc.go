package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica doc {list|get|add|update|delete} — free-form notes that are not
// issues.
//
// A card has a title and Markdown content and nothing else: no status, no
// assignee, no route. That is the point — a retrospective or a lesson learned
// is not work to be tracked, and giving it a status only invites the question
// of when it will be "done". It belongs to the workspace; the issue it came
// out of is an optional link, not a parent.

var cardCmd = &cobra.Command{
	Use:   "doc",
	Short: "Work with documents — the long-lived writing that is not an issue",
	Long: `Work with cards.

A card is a title plus Markdown content, owned by the workspace. Use one for a
retrospective, a lesson learned, a decision worth keeping — anything that
outlives the issue it came out of and has no status to track.

Link one to an issue with --issue when it came from a specific piece of work;
the link is optional and can be removed later with --detach.`,
}

var cardListCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents",
	Args:  cobra.NoArgs,
	RunE:  runCardList,
}

var cardGetCmd = &cobra.Command{
	Use:   "get <doc-id>",
	Short: "Read one document",
	Args:  exactArgs(1),
	RunE:  runCardGet,
}

var cardAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Write a document",
	Long: `Write a document.

Pipe the body in rather than passing it inline whenever it is more than a
sentence — a retrospective is a document, and --content mangles newlines:

  multica doc add --title "COC-97 踩坑" --content-stdin < notes.md`,
	Args: cobra.NoArgs,
	RunE: runCardAdd,
}

var cardUpdateCmd = &cobra.Command{
	Use:   "update <doc-id>",
	Short: "Rewrite a document's title, content, or issue link",
	Args:  exactArgs(1),
	RunE:  runCardUpdate,
}

var cardKindsCmd = &cobra.Command{
	Use:   "kinds",
	Short: "List the kinds in use, with a count each",
	Long: `List the kinds in use, with a count each.

A kind is a folder PATH — slashes make levels, and every level is listed here,
so 工作流架构演进 and 工作流架构演进/04-验证与交付 both appear. It is free text
with nothing to create first, which is exactly why you should read this before
filing: reusing a name beats inventing 文档 / 档案 / doc for one thing.`,
	Args: cobra.NoArgs,
	RunE: runCardKinds,
}

var cardDeleteCmd = &cobra.Command{
	Use:   "delete <doc-id>",
	Short: "Delete a document",
	Args:  exactArgs(1),
	RunE:  runCardDelete,
}

func init() {
	cardCmd.AddCommand(cardListCmd)
	cardCmd.AddCommand(cardGetCmd)
	cardCmd.AddCommand(cardAddCmd)
	cardCmd.AddCommand(cardUpdateCmd)
	cardCmd.AddCommand(cardKindsCmd)
	cardCmd.AddCommand(cardDeleteCmd)

	cardListCmd.Flags().String("output", "table", "Output format: table or json")
	cardListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	cardListCmd.Flags().Int("limit", 50, "Maximum number of documents to return")
	cardListCmd.Flags().Int("offset", 0, "Number of documents to skip (for pagination)")
	cardListCmd.Flags().String("issue", "", "Only documents linked to this issue (key or UUID)")
	cardListCmd.Flags().String("kind", "",
		"Only documents in this folder AND everything below it — `kind` is a PATH, so `--kind 工作流架构演进` also returns `工作流架构演进/04-验证与交付`. Pass an empty value (--kind=) for the unfiled ones; omit it for all. `doc kinds` lists what exists.")
	cardListCmd.Flags().String("search", "", "Only documents whose title or content contains this text (case-insensitive). Searches the whole workspace, not just the current page. Ignored with --issue, which returns that issue's documents in full.")

	cardGetCmd.Flags().String("output", "table", "Output format: table or json")
	cardKindsCmd.Flags().String("output", "table", "Output format: table or json")

	cardAddCmd.Flags().String("title", "", "Document title (required)")
	cardAddCmd.Flags().String("content", "", "Document content (decodes \\n, \\r, \\t, \\\\; pipe via --content-stdin for multi-line bodies)")
	cardAddCmd.Flags().Bool("content-stdin", false, "Read document content from stdin (preserves multi-line content verbatim)")
	cardAddCmd.Flags().String("content-file", "", "Read document content from a UTF-8 file. The path must be inside the current working directory unless --allow-external-file is set.")
	cardAddCmd.Flags().Bool("allow-external-file", false, "Allow --content-file to read a path outside the current working directory")
	cardAddCmd.Flags().String("kind", "", "Which folder to file it under, free text. Slashes make levels: `工作流架构演进/04-验证与交付`. A folder exists exactly as long as a document names it, so there is nothing to create first — but `doc kinds` lists what is already there, and reusing a name beats inventing a near-duplicate.")
	cardAddCmd.Flags().String("issue", "", "Link the document to this issue (key or UUID)")
	cardAddCmd.Flags().String("output", "json", "Output format: table or json")

	cardUpdateCmd.Flags().String("title", "", "New title")
	cardUpdateCmd.Flags().String("content", "", "New content")
	cardUpdateCmd.Flags().Bool("content-stdin", false, "Read the new content from stdin")
	cardUpdateCmd.Flags().String("content-file", "", "Read the new content from a UTF-8 file")
	cardUpdateCmd.Flags().Bool("allow-external-file", false, "Allow --content-file to read a path outside the current working directory")
	cardUpdateCmd.Flags().String("kind", "", "Move it to another folder. Slashes make levels. Pass an empty value (--kind=) to leave it unfiled.")
	cardUpdateCmd.Flags().String("issue", "", "Link the document to this issue (key or UUID)")
	cardUpdateCmd.Flags().Bool("detach", false, "Remove the document's issue link. Mutually exclusive with --issue.")
	cardUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	rootCmd.AddCommand(cardCmd)
}

// cardRow is the CLI's view of a card. The server returns more; these are the
// fields any command here prints.
type cardRow struct {
	ID        string  `json:"id"`
	IssueID   *string `json:"issue_id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func runCardList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	// Scoping to an issue is a different endpoint, not a query parameter, so
	// the two paths diverge here rather than in the server.
	path := "/api/cards"
	issueRef, _ := cmd.Flags().GetString("issue")
	if strings.TrimSpace(issueRef) != "" {
		if search, _ := cmd.Flags().GetString("search"); strings.TrimSpace(search) != "" {
			return fmt.Errorf("--search and --issue cannot be combined; the issue-scoped read returns that issue's documents in full")
		}
		ref, resolveErr := resolveIssueRef(ctx, client, issueRef)
		if resolveErr != nil {
			return fmt.Errorf("resolve issue: %w", resolveErr)
		}
		path = "/api/issues/" + ref.ID + "/cards"
	} else {
		params := url.Values{}
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		params.Set("limit", fmt.Sprintf("%d", limit))
		if offset > 0 {
			params.Set("offset", fmt.Sprintf("%d", offset))
		}
		if search, _ := cmd.Flags().GetString("search"); strings.TrimSpace(search) != "" {
			params.Set("q", strings.TrimSpace(search))
		}
		// Changed, not the value: --kind= selects the uncategorised tab, which
		// is a different request from omitting it.
		if cmd.Flags().Changed("kind") {
			kind, _ := cmd.Flags().GetString("kind")
			params.Set("kind", strings.TrimSpace(kind))
		}
		path += "?" + params.Encode()
	}

	var resp struct {
		Cards []cardRow `json:"cards"`
		Total int       `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	if len(resp.Cards) == 0 {
		// "No documents yet" is false when 13 exist and none matched — the reader
		// would conclude the workspace is empty and stop looking.
		if search, _ := cmd.Flags().GetString("search"); strings.TrimSpace(search) != "" {
			fmt.Fprintf(os.Stderr, "No documents match %q.\n", strings.TrimSpace(search))
		} else {
			fmt.Fprintln(os.Stderr, "No documents yet.")
		}
		return nil
	}

	full, _ := cmd.Flags().GetBool("full-id")
	// CHARS is what an agent needs to decide whether to pull a document's full
	// body. The list already returns the content, so this costs nothing; without
	// it a 300-character note and an 11674-character SOP are the same row.
	// Counted in runes, not bytes — a CJK document would otherwise read as
	// roughly three times its length.
	headers := []string{"ID", "TITLE", "KIND", "ISSUE", "CHARS", "UPDATED"}
	rows := make([][]string, 0, len(resp.Cards))
	for _, card := range resp.Cards {
		issue := "—"
		if card.IssueID != nil && *card.IssueID != "" {
			issue = displayID(*card.IssueID, full)
		}
		kind := card.Kind
		if kind == "" {
			kind = "—"
		}
		rows = append(rows, []string{
			displayID(card.ID, full),
			cardTitleForTable(card),
			kind,
			issue,
			strconv.Itoa(cardCharCount(card.Content)),
			shortTimestamp(card.UpdatedAt),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	if resp.Total > len(resp.Cards) {
		fmt.Fprintf(os.Stderr, "Showing %d of %d documents.\n", len(resp.Cards), resp.Total)
	}
	return nil
}

// cardTitleForTable falls back to the opening of the body. A title is optional
// on the server, and a row of blank cells would make an untitled card
// unfindable in the one place it is listed.
// cardCharCount is how long a document is, in characters.
//
// Runes, not bytes: a CJK document would otherwise read as roughly three times
// its length. The app counts code points for the same reason, so the number in
// `doc list` and the number on the page are the same number — see docLength in
// packages/views/docs/doc-tree.ts.
func cardCharCount(content string) int {
	return utf8.RuneCountInString(content)
}

func cardTitleForTable(card cardRow) string {
	title := strings.TrimSpace(card.Title)
	if title != "" {
		return title
	}
	firstLine := strings.TrimSpace(strings.SplitN(card.Content, "\n", 2)[0])
	if firstLine == "" {
		return "(untitled)"
	}
	runes := []rune(firstLine)
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return firstLine
}

func runCardGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var card cardRow
	if err := client.GetJSON(ctx, "/api/cards/"+args[0], &card); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, card)
	}

	fmt.Fprintf(os.Stdout, "ID:      %s\n", card.ID)
	if card.Title != "" {
		fmt.Fprintf(os.Stdout, "Title:   %s\n", card.Title)
	}
	if card.IssueID != nil && *card.IssueID != "" {
		fmt.Fprintf(os.Stdout, "Issue:   %s\n", *card.IssueID)
	}
	fmt.Fprintf(os.Stdout, "Created: %s\n", card.CreatedAt)
	fmt.Fprintf(os.Stdout, "Updated: %s\n", card.UpdatedAt)
	fmt.Fprintf(os.Stdout, "\n%s\n", card.Content)
	return nil
}

func runCardAdd(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	title = strings.TrimSpace(title)
	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	// One of the two, not both required: a card can be a bare note with no
	// heading, or a heading with the body still to come.
	if title == "" && !hasContent {
		return fmt.Errorf("--title or --content (or --content-stdin / --content-file) is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"title": title, "content": content}
	if kind, _ := cmd.Flags().GetString("kind"); strings.TrimSpace(kind) != "" {
		body["kind"] = strings.TrimSpace(kind)
	}
	if issueRef, _ := cmd.Flags().GetString("issue"); strings.TrimSpace(issueRef) != "" {
		ref, resolveErr := resolveIssueRef(ctx, client, issueRef)
		if resolveErr != nil {
			return fmt.Errorf("resolve issue: %w", resolveErr)
		}
		body["issue_id"] = ref.ID
	}

	var card cardRow
	if err := client.PostJSON(ctx, "/api/cards", body, &card); err != nil {
		return fmt.Errorf("add document: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Document %s written.\n", card.ID)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, card)
}

func runCardUpdate(cmd *cobra.Command, args []string) error {
	detach, _ := cmd.Flags().GetBool("detach")
	issueRef, _ := cmd.Flags().GetString("issue")
	if detach && strings.TrimSpace(issueRef) != "" {
		return fmt.Errorf("--issue and --detach are mutually exclusive")
	}

	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		title, _ := cmd.Flags().GetString("title")
		body["title"] = strings.TrimSpace(title)
	}
	if hasContent {
		body["content"] = content
	}
	if cmd.Flags().Changed("kind") {
		kind, _ := cmd.Flags().GetString("kind")
		body["kind"] = strings.TrimSpace(kind)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if detach {
		// An explicit null detaches; omitting the field leaves the link alone.
		// json.RawMessage on the server is what makes those distinguishable,
		// so the null has to be a real null and not an empty string.
		body["issue_id"] = json.RawMessage("null")
	} else if strings.TrimSpace(issueRef) != "" {
		ref, resolveErr := resolveIssueRef(ctx, client, issueRef)
		if resolveErr != nil {
			return fmt.Errorf("resolve issue: %w", resolveErr)
		}
		body["issue_id"] = ref.ID
	}

	if len(body) == 0 {
		return fmt.Errorf("nothing to update; pass --title, --content, --kind, --issue or --detach")
	}

	var card cardRow
	if err := client.PutJSON(ctx, "/api/cards/"+args[0], body, &card); err != nil {
		return fmt.Errorf("update document: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Document %s updated.\n", card.ID)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, card)
}

func runCardKinds(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Kinds []struct {
			Kind  string `json:"kind"`
			Count int64  `json:"count"`
		} `json:"kinds"`
	}
	if err := client.GetJSON(ctx, "/api/cards/kinds", &resp); err != nil {
		return fmt.Errorf("list kinds: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if len(resp.Kinds) == 0 {
		fmt.Fprintln(os.Stderr, "No kinds yet — every document is unfiled.")
		return nil
	}
	rows := make([][]string, 0, len(resp.Kinds))
	for _, k := range resp.Kinds {
		rows = append(rows, []string{k.Kind, fmt.Sprintf("%d", k.Count)})
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "DOCS"}, rows)
	return nil
}

func runCardDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/cards/"+args[0]); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Document %s deleted.\n", args[0])
	return nil
}
