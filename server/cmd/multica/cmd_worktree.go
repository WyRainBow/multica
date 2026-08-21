package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica worktree — the code-progress ledger.
//
// A card says how far a decision has got. A worktree says where the code is:
// which checkout exists, which branch it carries, what it sits on, who is
// driving it right now, and what happened in it round by round.
//
// Three accounts with three different write paths, because they rot at
// different rates and for different reasons:
//
//	facts     `worktree sync`, run inside the checkout — measured, never typed
//	session   `worktree session` — one slot per tree, overwritten in place
//	entries   `worktree log` — append-only, one line per round of work
//
// `log` exists because a git commit is too expensive a unit for "what did I
// change this round". Committing every round is not realistic; losing the round
// is worse.

type worktreeSessionPayload struct {
	Agent      string  `json:"agent"`
	Resume     string  `json:"resume"`
	Owner      string  `json:"owner"`
	NextAction string  `json:"next_action"`
	UpdatedAt  *string `json:"updated_at"`
}

type worktreeRow struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Path       string                 `json:"path"`
	Repo       string                 `json:"repo"`
	Branch     string                 `json:"branch"`
	BaseRef    string                 `json:"base_ref"`
	Role       string                 `json:"role"`
	Status     string                 `json:"status"`
	HeadSHA    string                 `json:"head_sha"`
	MergedSHA  string                 `json:"merged_sha"`
	MergedInto string                 `json:"merged_into"`
	Dirty      bool                   `json:"dirty"`
	VerifiedAt *string                `json:"verified_at"`
	Session    worktreeSessionPayload `json:"session"`
	ParentID   *string                `json:"parent_id"`
	EntryCount int64                  `json:"entry_count"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

type worktreeEntryRow struct {
	ID         string  `json:"id"`
	WorktreeID string  `json:"worktree_id"`
	IssueID    *string `json:"issue_id"`
	Kind       string  `json:"kind"`
	Body       string  `json:"body"`
	SHA        string  `json:"sha"`
	AuthorType string  `json:"author_type"`
	CreatedAt  string  `json:"created_at"`
}

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Track branches, checkouts and what changed in them",
	Long: `Track branches, checkouts and what changed in them.

A worktree row is a checkout: its branch, what it is based on, which batch
branch it feeds, who is driving it, and a running log of what happened in it.

The log is the point. A commit per round of work is not realistic, so rounds
that never became commits would otherwise leave no trace at all.`,
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the worktrees in this workspace",
	Args:  cobra.NoArgs,
	RunE:  runWorktreeList,
}

var worktreeShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show one worktree and its recent entries",
	Args:  exactArgs(1),
	RunE:  runWorktreeShow,
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Register a checkout",
	Long: `Register a checkout.

The name is how everything else addresses this tree, so keep it short and
without spaces. Everything else can be filled in later, or measured by
` + "`worktree sync`" + `.`,
	Args: exactArgs(1),
	RunE: runWorktreeAdd,
}

var worktreeSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Change a worktree's declared fields",
	Long: `Change a worktree's declared fields.

Only the declared ones: path, base, role, status, parent. Branch heads and merge
SHAs are measured by ` + "`worktree sync`" + ` and are not settable by hand — a
merge claim nobody can re-check is the thing this ledger exists to avoid.`,
	Args: exactArgs(1),
	RunE: runWorktreeSet,
}

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Drop a worktree row and its entries",
	Long: `Drop a worktree row and its entries.

Removes the record, never the directory on disk. Prefer ` + "`set --status archived`" + `
for a tree whose history is still worth reading.`,
	Args: exactArgs(1),
	RunE: runWorktreeRemove,
}

var worktreeLogCmd = &cobra.Command{
	Use:   "log <name> <what happened>",
	Short: "Append one line to a worktree's ledger",
	Long: `Append one line to a worktree's ledger.

Append-only: there is no edit and no delete. A line written in error is
corrected by writing the correction, the way a ledger works — which is what
makes the earlier lines worth trusting.`,
	Args: exactArgs(2),
	RunE: runWorktreeLog,
}

var worktreeEntriesCmd = &cobra.Command{
	Use:   "entries [name]",
	Short: "Read the ledger, for one tree or the whole workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorktreeEntries,
}

var worktreeSessionCmd = &cobra.Command{
	Use:   "session <name>",
	Short: "Set who is driving this tree and what is next",
	Long: `Set who is driving this tree and what is next.

One slot per tree, overwritten in place: the resume pointer for the session that
owns it, and a one-line next action. This is the navigation account — the thing
a pinned per-card comment used to carry, minus the copies that went stale one
card at a time.`,
	Args: exactArgs(1),
	RunE: runWorktreeSession,
}

var worktreeSyncCmd = &cobra.Command{
	Use:   "sync <name>",
	Short: "Measure the checkout with git and post the facts",
	Long: `Measure the checkout with git and post the facts.

Reads the branch, HEAD and dirty state from the working copy, and — when a
target is known — asks git whether HEAD is already an ancestor of it. If it is,
the tree is recorded as merged with the exact commit that landed.

The target is --into if given, otherwise the parent tree's branch, otherwise the
base ref. Full 40-character SHAs only: short ones are ambiguous and branch names
move.`,
	Args: exactArgs(1),
	RunE: runWorktreeSync,
}

func init() {
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeShowCmd)
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeCmd.AddCommand(worktreeSetCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	worktreeCmd.AddCommand(worktreeLogCmd)
	worktreeCmd.AddCommand(worktreeEntriesCmd)
	worktreeCmd.AddCommand(worktreeSessionCmd)
	worktreeCmd.AddCommand(worktreeSyncCmd)

	worktreeListCmd.Flags().String("output", "table", "Output format: table or json")
	worktreeListCmd.Flags().Bool("all", false, "Include merged and archived trees")

	worktreeShowCmd.Flags().String("output", "table", "Output format: table or json")
	worktreeShowCmd.Flags().Int("limit", 20, "How many ledger entries to show")

	for _, cmd := range []*cobra.Command{worktreeAddCmd, worktreeSetCmd} {
		cmd.Flags().String("path", "", "Absolute path of the checkout")
		cmd.Flags().String("repo", "", "Repository this checkout belongs to")
		cmd.Flags().String("branch", "", "Branch checked out here")
		cmd.Flags().String("base", "", "Branch this one is based on")
		cmd.Flags().String("role", "", "Pipeline position: base, feature, integration or launch")
		cmd.Flags().String("parent", "", "Name of the tree this one merges into")
		cmd.Flags().String("output", "json", "Output format: table or json")
	}
	worktreeAddCmd.Flags().String("status", "", "active, blocked, merged or archived (default active)")
	worktreeSetCmd.Flags().String("status", "", "active, blocked, merged or archived")
	worktreeSetCmd.Flags().String("name", "", "Rename the tree")
	worktreeSetCmd.Flags().Bool("clear-parent", false, "Detach from its parent tree")

	worktreeLogCmd.Flags().String("kind", "progress",
		"progress, branch, merge, blocked, handoff or verify")
	worktreeLogCmd.Flags().String("issue", "", "Card this line is about, if it is about one")
	worktreeLogCmd.Flags().String("sha", "", "Full 40-character commit SHA, when the line has one")
	worktreeLogCmd.Flags().String("output", "json", "Output format: table or json")

	worktreeEntriesCmd.Flags().String("output", "table", "Output format: table or json")
	worktreeEntriesCmd.Flags().Int("limit", 30, "How many entries to show")

	worktreeSessionCmd.Flags().String("agent", "", "Which agent holds this tree (claude, codex, …)")
	worktreeSessionCmd.Flags().String("resume", "", "Exact command that resumes that session")
	worktreeSessionCmd.Flags().String("owner", "", "Person accountable for the tree")
	worktreeSessionCmd.Flags().String("next", "", "One line: what happens next")
	worktreeSessionCmd.Flags().String("output", "json", "Output format: table or json")

	worktreeSyncCmd.Flags().String("dir", "", "Checkout to measure (default: the tree's path, else the current directory)")
	worktreeSyncCmd.Flags().String("into", "", "Branch to test HEAD against, overriding the parent/base default")
	worktreeSyncCmd.Flags().String("output", "json", "Output format: table or json")

	rootCmd.AddCommand(worktreeCmd)
}

// --- helpers ---

func worktreePath(name string) string {
	return "/api/worktrees/" + url.PathEscape(name)
}

// dashIfEmpty keeps a table column readable when a field has no value yet.
// Empty is a normal state here — a tree registered before its branch exists has
// no branch — so the cell says "nothing", not nothing at all.
func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func clip(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}

// shortSHA abbreviates for display only. What is stored is always the full
// object name; a short one read back out of the ledger could be ambiguous.
func shortSHA(sha string) string {
	if len(sha) < 12 {
		return sha
	}
	return sha[:12]
}

func fetchWorktree(ctx context.Context, client *cli.APIClient, name string) (worktreeRow, error) {
	var tree worktreeRow
	if err := client.GetJSON(ctx, worktreePath(name), &tree); err != nil {
		return worktreeRow{}, err
	}
	return tree, nil
}

// worktreeStale marks a row whose facts were measured long enough ago that they
// should be re-measured before being relied on. A ledger that cannot show its
// own age invites exactly the mistake it exists to prevent.
func worktreeFactsAge(verifiedAt *string) string {
	if verifiedAt == nil || *verifiedAt == "" {
		return "never"
	}
	return shortTimestamp(*verifiedAt)
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// --- commands ---

func runWorktreeList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Worktrees []worktreeRow `json:"worktrees"`
	}
	if err := client.GetJSON(ctx, "/api/worktrees", &resp); err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}

	all, _ := cmd.Flags().GetBool("all")
	visible := make([]worktreeRow, 0, len(resp.Worktrees))
	for _, tree := range resp.Worktrees {
		if !all && (tree.Status == "merged" || tree.Status == "archived") {
			continue
		}
		visible = append(visible, tree)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"worktrees": visible})
	}
	if len(visible) == 0 {
		fmt.Fprintln(os.Stderr, "No worktrees yet. Register one with `multica worktree add <name>`.")
		return nil
	}

	headers := []string{"NAME", "ROLE", "STATUS", "BRANCH", "BASE", "SESSION", "NEXT", "MEASURED", "LOG"}
	rows := make([][]string, 0, len(visible))
	for _, tree := range visible {
		branch := tree.Branch
		if tree.Dirty {
			branch += " *"
		}
		rows = append(rows, []string{
			tree.Name,
			tree.Role,
			tree.Status,
			dashIfEmpty(branch),
			dashIfEmpty(tree.BaseRef),
			dashIfEmpty(tree.Session.Agent),
			dashIfEmpty(clip(tree.Session.NextAction, 40)),
			worktreeFactsAge(tree.VerifiedAt),
			strconv.FormatInt(tree.EntryCount, 10),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorktreeShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	tree, err := fetchWorktree(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	var entries struct {
		Entries []worktreeEntryRow `json:"entries"`
	}
	entriesPath := fmt.Sprintf("%s/entries?limit=%d", worktreePath(args[0]), limit)
	if err := client.GetJSON(ctx, entriesPath, &entries); err != nil {
		return fmt.Errorf("list entries: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"worktree": tree, "entries": entries.Entries})
	}

	fmt.Printf("%s  (%s, %s)\n", tree.Name, tree.Role, tree.Status)
	fmt.Printf("  path      %s\n", dashIfEmpty(tree.Path))
	fmt.Printf("  branch    %s\n", dashIfEmpty(tree.Branch))
	fmt.Printf("  base      %s\n", dashIfEmpty(tree.BaseRef))
	if tree.MergedSHA != "" {
		fmt.Printf("  merged    %s into %s\n", tree.MergedSHA, dashIfEmpty(tree.MergedInto))
	}
	fmt.Printf("  measured  %s", worktreeFactsAge(tree.VerifiedAt))
	if tree.Dirty {
		fmt.Print("  (working copy dirty)")
	}
	fmt.Println()
	if tree.Session.Agent != "" || tree.Session.Resume != "" || tree.Session.NextAction != "" {
		fmt.Println("  session")
		fmt.Printf("    agent   %s\n", dashIfEmpty(tree.Session.Agent))
		fmt.Printf("    resume  %s\n", dashIfEmpty(tree.Session.Resume))
		fmt.Printf("    next    %s\n", dashIfEmpty(tree.Session.NextAction))
	}
	if len(entries.Entries) == 0 {
		fmt.Println("\nNo ledger entries yet.")
		return nil
	}
	fmt.Println()
	headers := []string{"WHEN", "KIND", "WHAT", "SHA"}
	rows := make([][]string, 0, len(entries.Entries))
	for _, entry := range entries.Entries {
		rows = append(rows, []string{
			shortTimestamp(entry.CreatedAt),
			entry.Kind,
			clip(entry.Body, 70),
			dashIfEmpty(shortSHA(entry.SHA)),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorktreeAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"name": strings.TrimSpace(args[0])}
	for flag, field := range map[string]string{
		"path": "path", "repo": "repo", "branch": "branch",
		"base": "base_ref", "role": "role", "status": "status",
	} {
		if value, _ := cmd.Flags().GetString(flag); strings.TrimSpace(value) != "" {
			body[field] = strings.TrimSpace(value)
		}
	}
	if parent, _ := cmd.Flags().GetString("parent"); strings.TrimSpace(parent) != "" {
		parentTree, err := fetchWorktree(ctx, client, strings.TrimSpace(parent))
		if err != nil {
			return fmt.Errorf("resolve parent worktree: %w", err)
		}
		body["parent_id"] = parentTree.ID
	}

	var tree worktreeRow
	if err := client.PostJSON(ctx, "/api/worktrees", body, &tree); err != nil {
		return fmt.Errorf("add worktree: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Worktree %s registered.\n", tree.Name)

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, tree)
}

func runWorktreeSet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	for flag, field := range map[string]string{
		"name": "name", "path": "path", "repo": "repo", "branch": "branch",
		"base": "base_ref", "role": "role", "status": "status",
	} {
		if cmd.Flags().Changed(flag) {
			value, _ := cmd.Flags().GetString(flag)
			body[field] = strings.TrimSpace(value)
		}
	}
	if clear, _ := cmd.Flags().GetBool("clear-parent"); clear {
		body["parent_id"] = ""
	} else if parent, _ := cmd.Flags().GetString("parent"); strings.TrimSpace(parent) != "" {
		parentTree, err := fetchWorktree(ctx, client, strings.TrimSpace(parent))
		if err != nil {
			return fmt.Errorf("resolve parent worktree: %w", err)
		}
		body["parent_id"] = parentTree.ID
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to change; pass at least one of --name, --path, --repo, --branch, --base, --role, --status, --parent, --clear-parent")
	}

	var tree worktreeRow
	if err := client.PutJSON(ctx, worktreePath(args[0]), body, &tree); err != nil {
		return fmt.Errorf("update worktree: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Worktree %s updated.\n", tree.Name)

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, tree)
}

func runWorktreeRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, worktreePath(args[0])); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Worktree %s removed. The directory on disk was not touched.\n", args[0])
	return nil
}

func runWorktreeLog(cmd *cobra.Command, args []string) error {
	body := strings.TrimSpace(args[1])
	if body == "" {
		return fmt.Errorf("write what happened; an empty line records nothing")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	kind, _ := cmd.Flags().GetString("kind")
	payload := map[string]any{"kind": strings.TrimSpace(kind), "body": body}
	if sha, _ := cmd.Flags().GetString("sha"); strings.TrimSpace(sha) != "" {
		payload["sha"] = strings.TrimSpace(sha)
	}
	if issue, _ := cmd.Flags().GetString("issue"); strings.TrimSpace(issue) != "" {
		issueRef, err := resolveIssueRef(ctx, client, strings.TrimSpace(issue))
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		payload["issue_id"] = issueRef.ID
	}

	var entry worktreeEntryRow
	if err := client.PostJSON(ctx, worktreePath(args[0])+"/entries", payload, &entry); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Logged to %s.\n", args[0])

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, entry)
}

func runWorktreeEntries(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	limit, _ := cmd.Flags().GetInt("limit")
	path := fmt.Sprintf("/api/worktree-entries?limit=%d", limit)
	scope := "workspace"
	if len(args) == 1 {
		path = fmt.Sprintf("%s/entries?limit=%d", worktreePath(args[0]), limit)
		scope = args[0]
	}

	var resp struct {
		Entries []worktreeEntryRow `json:"entries"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list entries: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if len(resp.Entries) == 0 {
		fmt.Fprintf(os.Stderr, "No entries for %s.\n", scope)
		return nil
	}
	headers := []string{"WHEN", "KIND", "WHAT", "SHA"}
	rows := make([][]string, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		rows = append(rows, []string{
			shortTimestamp(entry.CreatedAt),
			entry.Kind,
			clip(entry.Body, 70),
			dashIfEmpty(shortSHA(entry.SHA)),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorktreeSession(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	for flag, field := range map[string]string{
		"agent": "agent", "resume": "resume", "owner": "owner", "next": "next_action",
	} {
		if cmd.Flags().Changed(flag) {
			value, _ := cmd.Flags().GetString(flag)
			body[field] = strings.TrimSpace(value)
		}
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to set; pass at least one of --agent, --resume, --owner, --next")
	}

	var tree worktreeRow
	if err := client.PutJSON(ctx, worktreePath(args[0])+"/session", body, &tree); err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Session updated on %s.\n", tree.Name)

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, tree)
}

func runWorktreeSync(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	tree, err := fetchWorktree(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		dir = tree.Path
	}
	root, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("%s is not a git working tree; pass --dir", dashIfEmpty(dir))
	}
	// A path recorded on the row that no longer matches where we measured is
	// worth saying out loud: the numbers below are about this directory, not
	// about the one the ledger names.
	if tree.Path != "" && dir != "" && root != tree.Path {
		fmt.Fprintf(os.Stderr, "Note: measured %s, but the ledger records %s.\n", root, tree.Path)
	}

	branch, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("read branch: %w", err)
	}
	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read HEAD: %w", err)
	}
	status, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read working copy state: %w", err)
	}

	payload := map[string]any{
		"branch":   branch,
		"head_sha": head,
		"dirty":    strings.TrimSpace(status) != "",
	}

	// Where should this tree have landed? An explicit --into wins; otherwise
	// the tree it feeds, otherwise what it was branched from.
	target, _ := cmd.Flags().GetString("into")
	target = strings.TrimSpace(target)
	if target == "" && tree.ParentID != nil && *tree.ParentID != "" {
		var parent worktreeRow
		if err := client.GetJSON(ctx, "/api/worktrees/"+*tree.ParentID, &parent); err == nil {
			target = parent.Branch
		}
	}
	if target == "" {
		target = tree.BaseRef
	}

	if target != "" {
		// The evidence, not the claim: git says whether this commit is already
		// contained in the target. Exit status 0 means it is.
		if _, err := runGit(root, "merge-base", "--is-ancestor", head, target); err == nil {
			payload["merged_sha"] = head
			payload["merged_into"] = target
			payload["status"] = "merged"
		} else {
			fmt.Fprintf(os.Stderr, "HEAD is not yet contained in %s.\n", target)
		}
	}

	var updated worktreeRow
	if err := client.PostJSON(ctx, worktreePath(args[0])+"/sync", payload, &updated); err != nil {
		return fmt.Errorf("sync worktree: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Measured %s at %s.\n", updated.Name, shortSHA(head))

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, updated)
}
