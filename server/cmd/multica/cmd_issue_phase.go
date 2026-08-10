package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue phase {list|add|enter|complete|rename|delete} — the stations
// one issue passes through. A phase is a container, not a status: `status`
// answers "where is this now" and forgets the route, while a phase stays and
// holds the comments written while the issue was in it. See
// server/internal/handler/issue_phase.go for the server-side rules (unique
// name per issue, complete requires enter).

var issuePhaseCmd = &cobra.Command{
	Use:   "phase",
	Short: "Work with the phases of an issue",
	Long: `Work with the phases of an issue.

A phase is a station inside one issue. Comments filed under one stay grouped
with it, so a long-running issue reads as rounds instead of one flat thread.

The default route and what each station holds: run
    multica issue phase list --help

<phase> accepts the phase name (case-insensitive, unique prefix is enough) or
its full UUID.`,
}

var issuePhaseListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List the phases of an issue",
	Long: `List the phases of an issue.

Every new issue is created with five stations already on it, sub-issues
included:

  需求梳理    what this is and why — the reading, the boundaries, the constraints
  方案评审    whether this is the right thing to build
  代码评审    whether it was built right
  测试验收    the acceptance evidence: what was run, and what came back
  需求冻结    the closing verdict and whatever is left over

The two reviews are separate because they ask different questions of different
artifacts. One combined station puts both answers in the same pile, which is
the sorting a phase exists to do.

Either review recurs on its own — 方案评审 2, 代码评审 2 — and the rounds are
independent: revising the design does not reopen the code review.

Columns are NAME, STATE, COMMENTS, ENTERED, COMPLETED. STATE is derived, never
stored: completed is done, entered is current, neither is pending. A phase does
not have to be entered before comments can be filed into it. COMMENTS is the
only warning you get before ` + "`phase delete`" + `, which takes them with it.`,
	Args: exactArgs(1),
	RunE: runIssuePhaseList,
}

var issuePhaseAddCmd = &cobra.Command{
	Use:   "add <issue-id> <name>",
	Short: "Add a phase to an issue",
	Long: `Add a phase to an issue.

Appended to the end of the route unless --position is given. Names are unique
per issue (case-insensitive); a repeat is rejected with 409 rather than
producing two stations that read the same.`,
	Args: exactArgs(2),
	RunE: runIssuePhaseAdd,
}

var issuePhaseEnterCmd = &cobra.Command{
	Use:   "enter <issue-id> <phase>",
	Short: "Record that the issue arrived at a phase",
	Long: `Record that the issue arrived at a phase.

Keeps the first arrival time if the phase was entered before, and clears any
completion — re-entering means the work came back, not that it started over.`,
	Args: exactArgs(2),
	RunE: runIssuePhaseEnter,
}

var issuePhaseCompleteCmd = &cobra.Command{
	Use:   "complete <issue-id> <phase>",
	Short: "Record that the issue finished a phase",
	Long: `Record that the issue finished a phase.

Rejected with 409 if the phase was never entered — completing it would record
a route the work never took.`,
	Args: exactArgs(2),
	RunE: runIssuePhaseComplete,
}

var issuePhaseRenameCmd = &cobra.Command{
	Use:   "rename <issue-id> <phase> <new-name>",
	Short: "Rename a phase",
	Args:  exactArgs(3),
	RunE:  runIssuePhaseRename,
}

var issuePhaseDeleteCmd = &cobra.Command{
	Use:   "delete <issue-id> <phase>",
	Short: "Delete a phase and the comments filed under it",
	Long: `Delete a phase and the comments filed under it.

The comments go with the phase — a station's discussion is not meaningful
detached from it. --force is required once the phase holds any comment, so the
count is always seen before the delete.`,
	Args: exactArgs(2),
	RunE: runIssuePhaseDelete,
}

func init() {
	issuePhaseCmd.AddCommand(issuePhaseListCmd)
	issuePhaseCmd.AddCommand(issuePhaseAddCmd)
	issuePhaseCmd.AddCommand(issuePhaseEnterCmd)
	issuePhaseCmd.AddCommand(issuePhaseCompleteCmd)
	issuePhaseCmd.AddCommand(issuePhaseRenameCmd)
	issuePhaseCmd.AddCommand(issuePhaseDeleteCmd)

	issuePhaseListCmd.Flags().String("output", "table", "Output format: table or json")
	issuePhaseListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issuePhaseAddCmd.Flags().String("output", "json", "Output format: table or json")
	issuePhaseAddCmd.Flags().Int32("position", 0, "Explicit sort position (default: append to the end)")
	issuePhaseEnterCmd.Flags().String("output", "json", "Output format: table or json")
	issuePhaseCompleteCmd.Flags().String("output", "json", "Output format: table or json")
	issuePhaseRenameCmd.Flags().String("output", "json", "Output format: table or json")
	issuePhaseDeleteCmd.Flags().Bool("force", false,
		"Delete even when the phase still holds comments")

	issueCmd.AddCommand(issuePhaseCmd)
}

// issuePhase is the CLI's view of one station. The server returns more fields;
// these are the ones any command here needs.
type issuePhase struct {
	ID           string
	Name         string
	EnteredAt    string
	CompletedAt  string
	CommentCount int64
}

// phaseState derives the state from the two timestamps rather than reading a
// stored one. A stored state would be a third source of truth able to disagree
// with the times it summarizes. Mirrors phaseState in
// packages/views/issues/components/phase-track.tsx.
func phaseState(p issuePhase) string {
	switch {
	case p.CompletedAt != "":
		return "done"
	case p.EnteredAt != "":
		return "current"
	default:
		return "pending"
	}
}

func fetchIssuePhases(ctx context.Context, client *cli.APIClient, issueID string) ([]issuePhase, error) {
	var resp struct {
		Phases []struct {
			ID           string  `json:"id"`
			Name         string  `json:"name"`
			EnteredAt    *string `json:"entered_at"`
			CompletedAt  *string `json:"completed_at"`
			CommentCount int64   `json:"comment_count"`
		} `json:"phases"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueID+"/phases", &resp); err != nil {
		return nil, err
	}
	phases := make([]issuePhase, 0, len(resp.Phases))
	for _, p := range resp.Phases {
		phase := issuePhase{ID: p.ID, Name: p.Name, CommentCount: p.CommentCount}
		if p.EnteredAt != nil {
			phase.EnteredAt = *p.EnteredAt
		}
		if p.CompletedAt != nil {
			phase.CompletedAt = *p.CompletedAt
		}
		phases = append(phases, phase)
	}
	return phases, nil
}

// resolveIssuePhase turns a user-typed reference into one phase of this issue.
//
// Phases are named by hand and scoped to a single issue, so the name is the
// reference people actually have; the UUID is accepted for scripts that
// already captured one. Matching is exact-first so a phase named "方案评审"
// stays reachable once "方案评审 2" exists — otherwise adding a round would make the
// original ambiguous.
func resolveIssuePhase(
	ctx context.Context,
	client *cli.APIClient,
	issueID, input string,
) (issuePhase, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return issuePhase{}, fmt.Errorf("phase is required")
	}

	phases, err := fetchIssuePhases(ctx, client, issueID)
	if err != nil {
		return issuePhase{}, fmt.Errorf("list phases: %w", err)
	}
	if len(phases) == 0 {
		return issuePhase{}, fmt.Errorf(
			"this issue has no phases yet; add one with `multica issue phase add <issue-id> <name>`")
	}

	if uuidRegexp.MatchString(trimmed) {
		for _, p := range phases {
			if p.ID == trimmed {
				return p, nil
			}
		}
		return issuePhase{}, fmt.Errorf("phase %s is not on this issue", trimmed)
	}

	var prefixMatches []issuePhase
	for _, p := range phases {
		if strings.EqualFold(p.Name, trimmed) {
			return p, nil
		}
		if strings.HasPrefix(strings.ToLower(p.Name), strings.ToLower(trimmed)) {
			prefixMatches = append(prefixMatches, p)
		}
	}
	switch len(prefixMatches) {
	case 1:
		return prefixMatches[0], nil
	case 0:
		return issuePhase{}, fmt.Errorf("no phase named %q on this issue; available: %s",
			trimmed, phaseNameList(phases))
	default:
		return issuePhase{}, fmt.Errorf("phase %q is ambiguous; matches: %s",
			trimmed, phaseNameList(prefixMatches))
	}
}

func phaseNameList(phases []issuePhase) string {
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func runIssuePhaseList(cmd *cobra.Command, args []string) error {
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

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		var raw map[string]any
		if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/phases", &raw); err != nil {
			return fmt.Errorf("list phases: %w", err)
		}
		return cli.PrintJSON(os.Stdout, raw)
	}

	phases, err := fetchIssuePhases(ctx, client, issueRef.ID)
	if err != nil {
		return fmt.Errorf("list phases: %w", err)
	}
	if len(phases) == 0 {
		fmt.Fprintf(os.Stderr, "Issue %s has no phases.\n", issueRef.Display)
		return nil
	}

	full, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "STATE", "COMMENTS", "ENTERED", "COMPLETED"}
	rows := make([][]string, 0, len(phases))
	for _, p := range phases {
		rows = append(rows, []string{
			displayID(p.ID, full),
			p.Name,
			phaseState(p),
			fmt.Sprintf("%d", p.CommentCount),
			shortTimestamp(p.EnteredAt),
			shortTimestamp(p.CompletedAt),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// shortTimestamp clips an RFC3339 stamp to "2006-01-02T15:04" so a six-column
// table still fits a terminal. Same clip the other issue tables use.
func shortTimestamp(value string) string {
	if len(value) >= 16 {
		return value[:16]
	}
	return value
}

func runIssuePhaseAdd(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[1])
	if name == "" {
		return fmt.Errorf("phase name must not be blank")
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

	body := map[string]any{"name": name}
	if cmd.Flags().Changed("position") {
		position, _ := cmd.Flags().GetInt32("position")
		body["position"] = position
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/phases", body, &result); err != nil {
		return fmt.Errorf("add phase: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Phase %q added to issue %s.\n", name, issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssuePhaseEnter(cmd *cobra.Command, args []string) error {
	return runIssuePhaseTransition(cmd, args, "enter", "entered")
}

func runIssuePhaseComplete(cmd *cobra.Command, args []string) error {
	return runIssuePhaseTransition(cmd, args, "complete", "completed")
}

// runIssuePhaseTransition shares enter/complete — same path shape, same
// response, only the verb differs.
func runIssuePhaseTransition(cmd *cobra.Command, args []string, action, past string) error {
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
	phase, err := resolveIssuePhase(ctx, client, issueRef.ID, args[1])
	if err != nil {
		return err
	}

	var result map[string]any
	path := "/api/issues/" + issueRef.ID + "/phases/" + phase.ID + "/" + action
	if err := client.PostJSON(ctx, path, map[string]any{}, &result); err != nil {
		return fmt.Errorf("%s phase: %w", action, err)
	}

	fmt.Fprintf(os.Stderr, "Phase %q %s on issue %s.\n", phase.Name, past, issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssuePhaseRename(cmd *cobra.Command, args []string) error {
	newName := strings.TrimSpace(args[2])
	if newName == "" {
		return fmt.Errorf("phase name must not be blank")
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
	phase, err := resolveIssuePhase(ctx, client, issueRef.ID, args[1])
	if err != nil {
		return err
	}

	var result map[string]any
	path := "/api/issues/" + issueRef.ID + "/phases/" + phase.ID
	if err := client.PutJSON(ctx, path, map[string]any{"name": newName}, &result); err != nil {
		return fmt.Errorf("rename phase: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Phase %q renamed to %q on issue %s.\n",
		phase.Name, newName, issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssuePhaseDelete(cmd *cobra.Command, args []string) error {
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
	phase, err := resolveIssuePhase(ctx, client, issueRef.ID, args[1])
	if err != nil {
		return err
	}

	// The server deletes the phase's comments with it. Reporting the count and
	// demanding --force is the only place that number is visible before the
	// rows are gone — the API has no dry run.
	force, _ := cmd.Flags().GetBool("force")
	if phase.CommentCount > 0 && !force {
		return fmt.Errorf(
			"phase %q holds %d comment(s), which are deleted with it; pass --force to confirm",
			phase.Name, phase.CommentCount)
	}

	path := "/api/issues/" + issueRef.ID + "/phases/" + phase.ID
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete phase: %w", err)
	}

	if phase.CommentCount > 0 {
		fmt.Fprintf(os.Stderr, "Phase %q and its %d comment(s) deleted from issue %s.\n",
			phase.Name, phase.CommentCount, issueRef.Display)
	} else {
		fmt.Fprintf(os.Stderr, "Phase %q deleted from issue %s.\n", phase.Name, issueRef.Display)
	}
	return nil
}
