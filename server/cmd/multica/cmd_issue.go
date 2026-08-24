package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
)

// resolveTextFlag picks between a `--<name>` inline value, a `--<name>-stdin`
// flag, and a `--<name>-file <path>` flag, mirroring the existing `--content`
// / `--content-stdin` pattern. It returns the resolved string and an error
// when more than one source is set, or when stdin/file is requested but
// produces no body. Inline flag values are passed through
// util.UnescapeBackslashEscapes so bash-double-quoted `\n` becomes a real
// newline; stdin and file bodies are returned verbatim so literal backslashes
// survive intact.
//
// The `-file` source exists for Windows agents: piping HEREDOC content to
// `--<name>-stdin` from Windows PowerShell silently drops non-ASCII bytes
// (PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping
// to a native command), so Chinese / Cyrillic / any non-ASCII content
// arrives as `?`. Reading a UTF-8 file directly bypasses the shell's pipe
// re-encoding entirely. See issues #2198 / #2236 / #2376.
func resolveTextFlag(cmd *cobra.Command, flagName string) (string, bool, error) {
	stdinFlag := flagName + "-stdin"
	fileFlag := flagName + "-file"
	useStdin, _ := cmd.Flags().GetBool(stdinFlag)
	inline, _ := cmd.Flags().GetString(flagName)
	filePath, _ := cmd.Flags().GetString(fileFlag)

	sources := 0
	if useStdin {
		sources++
	}
	if inline != "" {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if sources > 1 {
		return "", false, fmt.Errorf("--%s, --%s, and --%s are mutually exclusive", flagName, stdinFlag, fileFlag)
	}

	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --%s: %w", stdinFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("stdin content for --%s is empty", stdinFlag)
		}
		return body, true, nil
	}
	if filePath != "" {
		if err := ensureFileFlagWithinWorkdir(cmd, fileFlag, flagName, filePath); err != nil {
			return "", false, err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", false, fmt.Errorf("read file for --%s: %w", fileFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("file content for --%s is empty", fileFlag)
		}
		return body, true, nil
	}
	if inline == "" {
		return "", false, nil
	}
	return util.UnescapeBackslashEscapes(inline), true, nil
}

// ensureFileFlagWithinWorkdir fails closed when a --<name>-file path resolves
// outside the current working directory, unless --allow-external-file is set.
//
// Agent task workdirs are isolated per profile and per task; machine-shared
// scratch paths like /tmp are not. MUL-4252 traced a cross-environment context
// leak to exactly this gap: a quick-create run wrote its description to a fixed
// /tmp/desc.md, the write silently failed because a *different* environment's
// run had left a stale file there minutes earlier, and --description-file then
// fed that stale content into the new issue. Requiring the file to live under
// the workdir turns "silently read another run's file" into a loud command
// failure — an "incorrect content" bug becomes a "command errored" bug.
func ensureFileFlagWithinWorkdir(cmd *cobra.Command, fileFlag, flagName, filePath string) error {
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); allow {
		return nil
	}
	within, err := fileWithinWorkingDir(filePath)
	if err != nil {
		return fmt.Errorf("resolve --%s path %q: %w", fileFlag, filePath, err)
	}
	if !within {
		return fmt.Errorf(
			"--%s path %q resolves outside the current working directory; "+
				"write agent temp files inside the task workdir (e.g. ./%s.md) rather than machine-shared "+
				"paths like /tmp, where another run's stale file can be read by mistake. "+
				"Pass --allow-external-file to override.",
			fileFlag, filePath, flagName)
	}
	return nil
}

// fileWithinWorkingDir reports whether filePath resolves to a location inside
// the process working directory. Both sides are symlink-resolved so aliased
// roots (e.g. macOS /tmp -> /private/tmp) and symlinks planted inside the
// workdir fail closed. A path that does not exist yet is judged on its cleaned
// absolute form so the caller's os.ReadFile still surfaces the real not-found
// error afterwards.
func fileWithinWorkingDir(filePath string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	base := cwd
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		base = resolved
	}
	abs := filePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else {
		// The file may not exist yet (the caller's os.ReadFile surfaces that).
		// Resolve symlinks on the parent directory instead so the comparison
		// base and the candidate share the same canonical prefix — otherwise a
		// workdir under a symlinked root (e.g. macOS temp dirs) would falsely
		// read as "outside". A missing parent falls back to a plain clean.
		if resolvedParent, perr := filepath.EvalSymlinks(filepath.Dir(abs)); perr == nil {
			abs = filepath.Join(resolvedParent, filepath.Base(abs))
		} else {
			abs = filepath.Clean(abs)
		}
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Work with issues",
}

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues in the workspace",
	RunE:  runIssueList,
}

var issueGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get issue details",
	Args:  exactArgs(1),
	RunE:  runIssueGet,
}

var issuePullRequestsCmd = &cobra.Command{
	Use:     "pull-requests <id>",
	Aliases: []string{"prs"},
	Short:   "List pull requests linked to an issue",
	Args:    exactArgs(1),
	RunE:    runIssuePullRequests,
}

var issueChildrenCmd = &cobra.Command{
	Use:     "children <id>",
	Aliases: []string{"subissues"},
	Short:   "List an issue's sub-issues grouped by stage",
	Args:    exactArgs(1),
	RunE:    runIssueChildren,
}

var issueCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new issue",
	RunE:  runIssueCreate,
}

var issueUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an issue",
	Args:  exactArgs(1),
	RunE:  runIssueUpdate,
}

var issueAssignCmd = &cobra.Command{
	Use:   "assign <id>",
	Short: "Assign an issue to a member, agent, or squad",
	Args:  exactArgs(1),
	RunE:  runIssueAssign,
}

var issueStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Change issue status",
	Long: "Change an issue's status. Valid statuses: " +
		"backlog, todo, in_progress, in_review, done, blocked, cancelled.",
	Args: exactArgs(2),
	RunE: runIssueStatus,
}

var issueDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Permanently delete an issue",
	Long: "Permanently delete an issue, along with its comments, reactions and " +
		"attachments. This cannot be undone.\n\n" +
		"Sub-issues are NOT deleted — they lose their parent and become " +
		"top-level issues. Delete them first if that is not what you want, or " +
		"use `issue archive` to take a whole subtree out of view without " +
		"destroying anything.",
	Args: exactArgs(1),
	RunE: runIssueDelete,
}

var issueArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive an issue and its sub-issues",
	Long: "Take an issue out of view along with everything below it in the " +
		"sub-issue tree. Archiving is independent of status — the issue keeps " +
		"whatever status it ended on. Use `issue unarchive` to bring it back.",
	Args: exactArgs(1),
	RunE: runIssueArchive,
}

var issueUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id>",
	Short: "Restore an archived issue and its sub-issues",
	Args:  exactArgs(1),
	RunE:  runIssueUnarchive,
}

var issueReorderCmd = &cobra.Command{
	Use:   "reorder <id>",
	Short: "Move an issue within its status column",
	Long: "Reposition an issue inside its current status column by computing a new\n" +
		"ordering position, the same value the board's drag-and-drop sets.\n\n" +
		"Pick exactly one target:\n" +
		"  --before <id>  place it directly above another issue in the same column\n" +
		"  --after  <id>  place it directly below another issue in the same column\n" +
		"  --top          move it to the top of its column\n" +
		"  --bottom       move it to the bottom of its column\n\n" +
		"Reorder stays inside the issue's current column. To move an issue to a\n" +
		"different column, change its status first with `multica issue status`.",
	Args: exactArgs(1),
	RunE: runIssueReorder,
}

// Comment subcommands.

var issueCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Work with issue comments",
}

var issueCommentListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List comments on an issue",
	Args:  exactArgs(1),
	RunE:  runIssueCommentList,
}

var issueCommentAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Add a comment to an issue",
	Args:  exactArgs(1),
	RunE:  runIssueCommentAdd,
}

var issueCommentEditCmd = &cobra.Command{
	Use:   "edit <comment-id>",
	Short: "Edit a comment's content",
	Long: `Edit a comment's content — replace the whole body, splice one passage, or append.

` + "`--content`" + ` REPLACES the whole body — nothing merges, so send the complete
text. To fix one passage of a long comment, splice instead: the command fetches
the current body, replaces only the anchored span, and sends the result. Nobody
should have to retype a long comment just to change one line of it.

  --replace <text> --with <new>                       fix a short passage
  --replace-start <a> --replace-end <b> --with <new>  fix a long span by its edges
  --append <text>                                     add a paragraph at the end

An empty --with deletes the passage rather than replacing it; the blank line
it leaves behind is collapsed so repeated deletes do not widen the gap.

An anchor must resolve to exactly one passage. No match, several matches, or
an end that precedes its start is an error — the command never guesses which
passage you meant; copy a longer anchor instead. Whitespace inside anchors
matches loosely, so text copied out of a rendered comment still finds its
source. ` + "`--append`" + ` text is joined with one blank line.

The comment keeps its id, place, and author, and readers see it marked as
edited.

Only the author and workspace admins may edit; the server rejects anyone else
with 403. Attachments are left untouched.

An edit re-runs ` + "`@agent` / `@squad`" + ` mentions against the new body (and cancels
tasks the OLD body triggered), so an edit can hire an agent the original
never called — mind the mentions you leave in.

Use ` + "`--content-file`, `--with-stdin`, or `--append-stdin`" + ` for multi-line
text; the inline flags decode ` + "`\\n`" + ` escapes instead.`,
	Args: exactArgs(1),
	RunE: runIssueCommentEdit,
}

var issueCommentGetCmd = &cobra.Command{
	Use:   "get <comment-id>",
	Short: "Read one comment by id",
	Long: `Read one comment by id.

Takes the full comment UUID — the one copied off a comment card in the app, or
shown in the ID column of ` + "`multica issue comment list`" + `. Reaching the same comment
through ` + "`comment list`" + ` costs the issue's whole thread; this costs one comment.`,
	Args: exactArgs(1),
	RunE: runIssueCommentGet,
}

var issueCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete a comment",
	Args:  exactArgs(1),
	RunE:  runIssueCommentDelete,
}

var issueCommentResolveCmd = &cobra.Command{
	Use:   "resolve <comment-id>",
	Short: "Mark a comment as its thread's conclusion",
	Long: `Mark a comment as its thread's conclusion.

Pass the comment that HOLDS the conclusion, not the thread root. A resolved
thread folds on the default reads, and what survives depends on which comment
you passed:

  a reply     root + that reply
  the root    the root ALONE — every reply is dropped

So resolving the root when the conclusion sits in a reply hides the conclusion
from every later reader. Resolve the root only when the root already says it, or
the thread is a dead end worth no conclusion.

Resolve at the END. A new reply does NOT reopen a thread whose conclusion is a
reply, so replies added afterwards stay hidden with nothing warning anyone — run
` + "`comment unresolve`" + ` on the resolved comment first if the discussion reopens.

A thread holds at most one conclusion: resolving a second comment clears the
first.`,
	Args: exactArgs(1),
	RunE: runIssueCommentResolve,
}

var issueCommentPinCmd = &cobra.Command{
	Use:   "pin <comment-id>",
	Short: "Pin a thread to the top of the issue",
	Long: `Pin a thread to the top of the issue.

For the discussion someone arriving at this issue should read first. An issue
worked for a while collects threads faster than anyone re-reads them, and the
one that matters is rarely the newest.

Roots only — a reply is not somewhere a reader starts. Pinning is separate from
resolving: resolving answers "is this over", pinning answers "start here", and a
thread is often both. Pinned threads sort most-recently-pinned first, and
re-pinning keeps a thread's original place rather than jumping it to the front.`,
	Args: exactArgs(1),
	RunE: runIssueCommentPin,
}

var issueCommentUnpinCmd = &cobra.Command{
	Use:   "unpin <comment-id>",
	Short: "Remove a thread's pin",
	Args:  exactArgs(1),
	RunE:  runIssueCommentUnpin,
}

var issueCommentUnresolveCmd = &cobra.Command{
	Use:   "unresolve <comment-id>",
	Short: "Reopen a thread by clearing its conclusion",
	Long: `Reopen a thread by clearing its conclusion.

Pass the comment that actually carries the conclusion — ` + "`comment list`" + ` shows it
with type ` + "`resolution`" + `. Reopening the wrong one is a silent no-op.

Needed because replying does not always reopen: the automatic reopen only fires
when the THREAD ROOT is the resolved comment. When a reply holds the conclusion,
new replies are hidden from the default reads until this command clears it.`,
	Args: exactArgs(1),
	RunE: runIssueCommentUnresolve,
}

// Subscriber subcommands.

var issueSubscriberCmd = &cobra.Command{
	Use:   "subscriber",
	Short: "Work with issue subscribers",
}

var issueSubscriberListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List subscribers of an issue",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberList,
}

var issueSubscriberAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Subscribe a user or agent to an issue (defaults to the caller)",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberAdd,
}

var issueSubscriberRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id>",
	Short: "Unsubscribe a user or agent from an issue (defaults to the caller)",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberRemove,
}

// Execution history subcommands.

var issueRunsCmd = &cobra.Command{
	Use:   "runs <issue-id>",
	Short: "List execution history for an issue",
	Args:  exactArgs(1),
	RunE:  runIssueRuns,
}

var issueRunMessagesCmd = &cobra.Command{
	Use:   "run-messages <task-id>",
	Short: "List messages for an execution",
	Args:  exactArgs(1),
	RunE:  runIssueRunMessages,
}

var issueUsageCmd = &cobra.Command{
	Use:   "usage <issue-id>",
	Short: "Show aggregated token usage for an issue",
	Args:  exactArgs(1),
	RunE:  runIssueUsage,
}

var issueRerunCmd = &cobra.Command{
	Use:   "rerun <id>",
	Short: "Re-enqueue an issue's current agent assignment as a fresh task",
	Args:  exactArgs(1),
	RunE:  runIssueRerun,
}

var issueCancelTaskCmd = &cobra.Command{
	Use:   "cancel-task <task-id>",
	Short: "Cancel a running or queued task (interrupts in-flight agent)",
	Long: "Cancel a single task by its ID. Accepts the short ID prefix shown by `issue runs`. " +
		"Use --issue to scope short-ID resolution to a specific issue when ambiguous. " +
		"Triggers daemon-side interrupt of any in-flight agent so it stops emitting tool calls promptly.",
	Args: exactArgs(1),
	RunE: runIssueCancelTask,
}

var issueSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search issues by title, description, or comments",
	Long: "Search issues in the current workspace. The query matches issue titles,\n" +
		"descriptions, and comment bodies, so a conclusion that only lives in a\n" +
		"comment thread is still findable.\n\n" +
		"A bare number or an identifier-shaped query (\"412\", \"AGE-412\") matches\n" +
		"the issue with that number. The prefix is not validated, and number\n" +
		"matches rank first, so pasting an identifier from another tracker can\n" +
		"put an unrelated local issue at the top of the results.\n\n" +
		"The MATCH column (match_source in --output json) reports the strongest\n" +
		"field that matched — title, description, or comment — and falls back to\n" +
		"\"comment\" for a number-only hit. Treat it as a display hint, not a\n" +
		"filter.",
	Args: cobra.ExactArgs(1),
	RunE: runIssueSearch,
}

var validIssueStatuses = []string{
	"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled",
}

var validIssuePriorities = []string{
	"urgent", "high", "medium", "low", "none",
}

// validIssueSortColumns are the sort keys `issue list --sort` accepts. They
// mirror the server's ListIssues handler. "position" is the default and is
// always sorted ascending (the board's manual drag order), so --direction is
// only meaningful for the other columns.
var validIssueSortColumns = []string{
	// updated_at is what "moved lately" means: a new comment bumps its issue
	// in the same statement that inserts it (see CreateComment), so this one
	// column covers comments, status changes and edits alike. The server has
	// always accepted it; only this list kept it out.
	"position", "title", "created_at", "updated_at", "start_date", "due_date", "priority",
}

// directionalIssueSortColumns are the sort keys for which --direction is
// meaningful: every valid column except "position". Derived from
// validIssueSortColumns so the two stay in sync.
var directionalIssueSortColumns = func() []string {
	cols := make([]string, 0, len(validIssueSortColumns)-1)
	for _, c := range validIssueSortColumns {
		if c != "position" {
			cols = append(cols, c)
		}
	}
	return cols
}()

func validateIssueStatus(status string) error {
	return validateIssueEnum("status", status, validIssueStatuses)
}

func validateIssuePriority(priority string) error {
	return validateIssueEnum("priority", priority, validIssuePriorities)
}

func validateIssueEnum(field, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", "))
}

func init() {
	issueCmd.AddCommand(issueListCmd)
	issueCmd.AddCommand(issueGetCmd)
	issueCmd.AddCommand(issuePullRequestsCmd)
	issueCmd.AddCommand(issueChildrenCmd)
	issueCmd.AddCommand(issueCreateCmd)
	issueCmd.AddCommand(issueUpdateCmd)
	issueCmd.AddCommand(issueAssignCmd)
	issueCmd.AddCommand(issueStatusCmd)
	issueCmd.AddCommand(issueDeleteCmd)
	issueCmd.AddCommand(issueArchiveCmd)
	issueCmd.AddCommand(issueUnarchiveCmd)
	issueCmd.AddCommand(issueReorderCmd)
	issueCmd.AddCommand(issueCommentCmd)
	issueCmd.AddCommand(issueSubscriberCmd)
	issueCmd.AddCommand(issueRunsCmd)
	issueCmd.AddCommand(issueRunMessagesCmd)
	issueCmd.AddCommand(issueUsageCmd)
	issueCmd.AddCommand(issueRerunCmd)
	issueCmd.AddCommand(issueCancelTaskCmd)
	issueCmd.AddCommand(issueReceiptCmd)
	issueCmd.AddCommand(issueInterruptCmd)
	issueCmd.AddCommand(issueSearchCmd)

	issueCommentCmd.AddCommand(issueCommentListCmd)
	issueCommentCmd.AddCommand(issueCommentGetCmd)
	issueCommentCmd.AddCommand(issueCommentAddCmd)
	issueCommentCmd.AddCommand(issueCommentEditCmd)
	issueCommentCmd.AddCommand(issueCommentDeleteCmd)
	issueCommentCmd.AddCommand(issueCommentResolveCmd)
	issueCommentCmd.AddCommand(issueCommentUnresolveCmd)
	issueCommentCmd.AddCommand(issueCommentPinCmd)
	issueCommentCmd.AddCommand(issueCommentUnpinCmd)

	issueSubscriberCmd.AddCommand(issueSubscriberListCmd)
	issueSubscriberCmd.AddCommand(issueSubscriberAddCmd)
	issueSubscriberCmd.AddCommand(issueSubscriberRemoveCmd)

	// issue delete
	issueDeleteCmd.Flags().Bool("force", false,
		"Delete even when the issue still has sub-issues")

	// issue archive / unarchive
	issueArchiveCmd.Flags().String("output", "json", "Output format: table or json")
	issueUnarchiveCmd.Flags().String("output", "json", "Output format: table or json")

	// issue list
	issueListCmd.Flags().String("output", "table", "Output format: table or json")
	issueListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issueListCmd.Flags().Bool("include-archived", false, "Include archived issues")
	issueListCmd.Flags().String("status", "", "Filter by status")
	issueListCmd.Flags().String("priority", "", "Filter by priority")
	issueListCmd.Flags().String("assignee", "", "Filter by assignee name (member, agent, or squad; fuzzy match)")
	issueListCmd.Flags().String("assignee-id", "", "Filter by assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	issueListCmd.Flags().String("project", "", "Filter by project ID")
	issueListCmd.Flags().StringSlice("metadata", nil, "Filter by metadata key=value (repeatable; combined with AND). Value is JSON-parsed: 'true'/'false' → bool, numbers → number, otherwise string. Wrap as '\"42\"' to force a string when the value would otherwise sniff as a number.")
	issueListCmd.Flags().Int("limit", 50, "Maximum number of issues to return")
	issueListCmd.Flags().Int("offset", 0, "Number of issues to skip (for pagination)")
	issueListCmd.Flags().String("sort", "", "Sort column: position (default, manual board order), title, created_at, updated_at, start_date, due_date, priority")
	issueListCmd.Flags().Duration("updated-since", 0,
		"Only issues with activity inside this window, e.g. 24h. A comment counts as activity. Pair with --sort updated_at --direction desc so the page holds the newest")
	issueListCmd.Flags().String("direction", "", "Sort direction (asc or desc); requires --sort to be a non-position column (position is always ascending)")

	// issue get
	issueGetCmd.Flags().String("output", "json", "Output format: table or json")
	issueGetCmd.Flags().String("quote-start", "",
		"Return only a span of the description instead of the whole issue: the text the span starts with, copied verbatim")
	issueGetCmd.Flags().String("quote-end", "",
		"Text the span ends with, copied verbatim. Must land in exactly one place after the start, "+
			"so a bare \"。\" is rejected rather than silently ending the span at the first sentence. "+
			"Omit to return just the --quote-start text")
	issueGetCmd.Flags().String("quote-prefix", "",
		"Text immediately before the passage, used to pick between several matches")
	issueGetCmd.Flags().String("quote-suffix", "",
		"Text immediately after the passage, used to pick between several matches")

	// issue pull-requests
	issuePullRequestsCmd.Flags().String("output", "table", "Output format: table or json")

	issueChildrenCmd.Flags().String("output", "table", "Output format: table or json")
	issueChildrenCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// issue create
	issueCreateCmd.Flags().String("title", "", "Issue title (required)")
	issueCreateCmd.Flags().Bool("test", false,
		"Mark this as a throwaway test issue: prefixes the title with "+testIssueTitlePrefix+" so it is separable from real work at a glance. Idempotent — a title that already carries the prefix is left alone.")
	issueCreateCmd.Flags().String("description", "", "Issue description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	issueCreateCmd.Flags().Bool("description-stdin", false, "Read issue description from stdin (preserves multi-line content verbatim)")
	issueCreateCmd.Flags().String("description-file", "", "Read issue description from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	issueCreateCmd.Flags().Bool("allow-external-file", false, "Allow --description-file / --attachment to read a path outside the current working directory. Off by default so a stale file from another run/environment can't be picked up (MUL-4252).")
	issueCreateCmd.Flags().String("status", "", "Issue status")
	issueCreateCmd.Flags().String("priority", "", "Issue priority")
	issueCreateCmd.Flags().String("assignee", "", "Assignee name (member, agent, or squad; fuzzy match)")
	issueCreateCmd.Flags().String("assignee-id", "", "Assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	issueCreateCmd.Flags().Bool("no-assignee", false, "Create the issue unassigned instead of assigning it to you")
	issueCreateCmd.Flags().String("parent", "", "Parent issue ID")
	issueCreateCmd.Flags().Int("stage", 0, "Stage ordinal (>=1) grouping this sub-issue into an ordered barrier group under its parent; omit for unstaged. The parent assignee is woken only when every sub-issue in a stage finishes.")
	issueCreateCmd.Flags().String("project", "", "Project ID")
	issueCreateCmd.Flags().String("start-date", "", "Start date (calendar day, YYYY-MM-DD)")
	issueCreateCmd.Flags().String("due-date", "", "Due date (calendar day, YYYY-MM-DD)")
	issueCreateCmd.Flags().Bool("allow-duplicate", false, "Allow creating an issue even when an active duplicate exists")
	issueCreateCmd.Flags().String("output", "json", "Output format: table or json")
	issueCreateCmd.Flags().StringSlice("attachment", nil, "File path(s) to attach (can be specified multiple times)")
	issueCreateCmd.Flags().StringSlice("attachment-id", nil, "Existing attachment UUID(s) to bind to the created issue (can be specified multiple times)")
	issueCreateCmd.Flags().String("session", "", "Session id to record in the card's automatic index, as a snapshot of which session filed it. Defaults to the agent session this command runs inside (CLAUDE_CODE_SESSION_ID / CODEX_SESSION_ID); the index says \"未记录\" when there is neither.")

	// issue update
	issueUpdateCmd.Flags().String("title", "", "New title")
	issueUpdateCmd.Flags().String("description", "", "New description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	issueUpdateCmd.Flags().Bool("description-stdin", false, "Read new description from stdin (preserves multi-line content verbatim)")
	issueUpdateCmd.Flags().String("description-file", "", "Read new description from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	issueUpdateCmd.Flags().Bool("allow-external-file", false, "Allow --description-file to read a path outside the current working directory. Off by default so a stale temp file from another run/environment can't be picked up (MUL-4252).")
	issueUpdateCmd.Flags().String("status", "", "New status")
	issueUpdateCmd.Flags().String("priority", "", "New priority")
	issueUpdateCmd.Flags().String("assignee", "", "New assignee name (member, agent, or squad; fuzzy match)")
	issueUpdateCmd.Flags().String("assignee-id", "", "New assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	issueUpdateCmd.Flags().String("project", "", "Project ID")
	issueUpdateCmd.Flags().String("start-date", "", "New start date (calendar day, YYYY-MM-DD; pass empty string to clear)")
	issueUpdateCmd.Flags().String("due-date", "", "New due date (calendar day, YYYY-MM-DD)")
	issueUpdateCmd.Flags().String("parent", "", "Parent issue ID (use --parent \"\" to clear)")
	issueUpdateCmd.Flags().Int("stage", 0, "Stage ordinal (>=1) for this sub-issue; see `issue create --stage`")
	issueUpdateCmd.Flags().Float64("position", 0, "Ordering position within the board column (lower sorts first); prefer `issue reorder` for relative moves")
	issueUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// issue status
	issueStatusCmd.Flags().String("output", "table", "Output format: table or json")
	issueStatusCmd.Flags().String("comment-review-file", "", "JSON file with summary and per-thread resolve/keep_unresolved dispositions returned by the done gate")
	issueStatusCmd.Flags().Bool("allow-external-file", false, "Allow --comment-review-file outside the current working directory")

	// issue reorder
	registerIssueReorderFlags(issueReorderCmd)

	// issue assign
	issueAssignCmd.Flags().String("to", "", "Assignee name (member, agent, or squad; fuzzy match)")
	issueAssignCmd.Flags().String("to-id", "", "Assignee UUID — member, agent, or squad (mutually exclusive with --to)")
	issueAssignCmd.Flags().Bool("unassign", false, "Remove current assignee")
	issueAssignCmd.Flags().String("output", "json", "Output format: table or json")

	// issue comment list
	issueCommentListCmd.Flags().String("output", "table", "Output format: table or json")
	issueCommentListCmd.Flags().String("since", "", "Only return comments created after this timestamp (RFC3339)")
	issueCommentListCmd.Flags().String("thread", "", "Comment UUID — return the thread containing this comment (root + every descendant). May be a root or a reply id.")
	issueCommentListCmd.Flags().Int("tail", 0, "Only valid with --thread. Cap reply count to the N most recent replies; the thread root is always included (even with --tail 0). Use --before/--before-id to scroll to older replies.")
	issueCommentListCmd.Flags().Int("recent", 0, "Return the N most recently active threads. N caps THREADS, not comments: every thread carries its root plus EVERY descendant with no per-thread cap, so on an issue with fewer than N root threads this returns the entire history (minus folded resolved threads). Prefer two bounded reads: scan with --roots-only --summary, then open selected threads with --thread <id> --tail N. Use --before/--before-id from the previous response to scroll to older threads.")
	issueCommentListCmd.Flags().Bool("roots-only", false, "Only return top-level comments (parent_id is null). Each root also carries reply_count + last_activity_at so you can triage which thread to open.")
	issueCommentListCmd.Flags().Bool("compact", false, "JSON output only: drop response fields that carry no information for a reader — the issue_id echoed from the request path, source_task_id, updated_at when identical to created_at, null-valued fields, and empty arrays. Content and identity fields pass through untouched. Recommended for agent reads; composes with any mode.")
	issueCommentListCmd.Flags().Bool("summary", false, "Clip each comment's content to a short preview (sets content_truncated) so you can scan a list without pulling full bodies. Composes with any mode.")
	issueCommentListCmd.Flags().Bool("full", false, "Escape hatch: return every comment in resolved threads verbatim. By default the complete-thread reads (default list, --recent, --thread without --tail) are folded — a resolved thread collapses to its root + conclusion, with the dropped count reported on the root — so you do not pay tokens for settled discussion. Pass --full when you need the folded discussion. No effect on --since/--tail/--roots-only reads, which are never folded.")
	issueCommentListCmd.Flags().String("before", "", "Cursor (RFC3339Nano timestamp). With --recent: thread cursor (last_activity_at). With --thread + --tail: reply cursor (reply created_at). Read from the X-Multica-Next-Before response header, printed on stderr as \"Next thread cursor\" / \"Next reply cursor\"; must be paired with --before-id.")
	issueCommentListCmd.Flags().String("phase", "", "Only return comments filed under this phase — name (case-insensitive, unique prefix is enough) or UUID. Filtered client-side, so it cannot be combined with --recent / --tail / --thread / --before.")
	issueCommentListCmd.Flags().String("before-id", "", "Cursor UUID. With --recent: thread root UUID. With --thread + --tail: oldest reply UUID. Read from the X-Multica-Next-Before-Id response header; must be paired with --before.")

	// issue runs
	issueRunsCmd.Flags().String("output", "table", "Output format: table or json")
	issueRunsCmd.Flags().Bool("full-id", false, "Show full task UUIDs in table output")

	// issue usage
	issueUsageCmd.Flags().String("output", "table", "Output format: table or json")

	// issue rerun
	issueRerunCmd.Flags().String("output", "json", "Output format: table or json")
	// issue cancel-task
	// issue receipt
	issueReceiptCmd.Flags().String("result", "", "Receipt result: merged | delivered_without_mr | abandoned | unknown (omit to show latest)")
	issueReceiptCmd.Flags().String("reason", "", "Reason (required for unknown)")
	issueReceiptCmd.Flags().String("verify-local", "", "Local repo path: run machine checks (SHA object + ancestry) and attach evidence")
	issueReceiptCmd.Flags().String("target", "origin/HEAD", "Target ref for the ancestry check with --verify-local")
	issueReceiptCmd.Flags().String("output", "table", "Output format: table or json")
	// issue interrupt
	issueInterruptCmd.Flags().String("comment", "", "Text to interject (posted with an explicit @mention of the assignee agent)")
	issueInterruptCmd.Flags().Bool("content-stdin", false, "Read the interjection text from stdin")
	issueCancelTaskCmd.Flags().String("output", "json", "Output format: table or json")
	issueCancelTaskCmd.Flags().String("issue", "", "Issue ID/key to scope short task ID prefix resolution")
	// issue run-messages
	issueRunMessagesCmd.Flags().String("output", "json", "Output format: table or json")
	issueRunMessagesCmd.Flags().Int("since", 0, "Only return messages after this sequence number")
	issueRunMessagesCmd.Flags().String("issue", "", "Issue ID/key to scope short task ID prefix resolution")

	// issue comment add
	issueCommentGetCmd.Flags().String("output", "json", "Output format: table or json")
	issueCommentAddCmd.Flags().String("content", "", "Comment content (decodes \\n, \\r, \\t, \\\\; pipe via --content-stdin for multi-line bodies or to preserve literal backslashes)")
	issueCommentAddCmd.Flags().Bool("content-stdin", false, "Read comment content from stdin (preserves multi-line content verbatim)")
	issueCommentAddCmd.Flags().String("content-file", "", "Read comment content from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	issueCommentAddCmd.Flags().Bool("allow-external-file", false, "Allow --content-file / --attachment to read a path outside the current working directory. Off by default so a stale file from another run/environment can't be picked up (MUL-4252).")
	issueCommentAddCmd.Flags().String("parent", "",
		"Parent comment ID to reply under. Use it whenever this comment ANSWERS an existing one — "+
			"a verdict on a review, adopting or refuting its findings, a follow-up. A comment can never be "+
			"re-parented, so two top-level comments where one settles the other stay flat forever. Omit it "+
			"only for a question nobody asked yet. A comment-triggered agent task must reply under its "+
			"trigger comment; omitting --parent to post a top-level comment is rejected")
	issueCommentAddCmd.Flags().String("anchor", "",
		"Anchor the comment to this exact passage of the issue description (inline comment). "+
			"The text must appear verbatim in the current description; the CLI locates it and "+
			"fails if it does not match, so a comment can never be filed against a passage that "+
			"is not there.")
	issueCommentAddCmd.Flags().Int("anchor-occurrence", 1,
		"Which occurrence of --anchor to attach to when the passage appears more than once (1-based)")
	issueCommentAddCmd.Flags().String("phase", "",
		"File this comment under a phase of the issue — name (case-insensitive, unique prefix is enough) or UUID. List them with `multica issue phase list <issue-id>`.")
	issueCommentAddCmd.Flags().StringSlice("attachment", nil, "File path(s) to attach (can be specified multiple times)")
	issueCommentAddCmd.Flags().String("output", "json", "Output format: table or json")
	issueCommentEditCmd.Flags().String("content", "", "Whole replacement comment content (decodes \\n, \\r, \\t, \\\\; pipe via --content-stdin for multi-line bodies or to preserve literal backslashes)")
	issueCommentEditCmd.Flags().Bool("content-stdin", false, "Read the whole replacement content from stdin (preserves multi-line content verbatim)")
	issueCommentEditCmd.Flags().String("content-file", "", "Read replacement content from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	issueCommentEditCmd.Flags().Bool("allow-external-file", false, "Allow --content-file to read a path outside the current working directory. Off by default so a stale file from another run/environment can't be picked up (MUL-4252).")
	issueCommentEditCmd.Flags().String("replace", "", "Replace only the passage matching this text, with --with; an error unless it matches exactly one passage")
	issueCommentEditCmd.Flags().String("replace-start", "", "With --replace-end: replace the span that starts at this text (whitespace matches loosely)")
	issueCommentEditCmd.Flags().String("replace-end", "", "With --replace-start: the replaced span stops at the end of this text")
	issueCommentEditCmd.Flags().String("with", "", "Replacement for the --replace / --replace-start passage (decodes \\n escapes; pipe via --with-stdin for multi-line text)")
	issueCommentEditCmd.Flags().Bool("with-stdin", false, "Read the --with replacement text from stdin (preserves multi-line content verbatim)")
	issueCommentEditCmd.Flags().String("append", "", "Append this text as a new paragraph at the end of the comment (decodes \\n escapes)")
	issueCommentEditCmd.Flags().Bool("append-stdin", false, "Read the --append text from stdin (preserves multi-line content verbatim)")
	issueCommentEditCmd.Flags().String("output", "json", "Output format: table or json")

	// issue comment resolve/unresolve
	issueCommentResolveCmd.Flags().String("output", "json", "Output format: table or json")
	issueCommentUnresolveCmd.Flags().String("output", "json", "Output format: table or json")
	issueCommentPinCmd.Flags().String("output", "json", "Output format: table or json")
	issueCommentUnpinCmd.Flags().String("output", "json", "Output format: table or json")

	// issue search
	issueSearchCmd.Flags().Int("limit", 20, "Maximum number of results to return")
	issueSearchCmd.Flags().Bool("include-closed", false, "Include done and cancelled issues")
	issueSearchCmd.Flags().String("output", "table", "Output format: table or json")

	// issue subscriber list
	issueSubscriberListCmd.Flags().String("output", "table", "Output format: table or json")

	// issue subscriber add
	issueSubscriberAddCmd.Flags().String("user", "", "Member or agent name to subscribe (fuzzy match; defaults to the caller)")
	issueSubscriberAddCmd.Flags().String("user-id", "", "Member or agent UUID to subscribe (mutually exclusive with --user)")
	issueSubscriberAddCmd.Flags().String("output", "json", "Output format: table or json")

	// issue subscriber remove
	issueSubscriberRemoveCmd.Flags().String("user", "", "Member or agent name to unsubscribe (fuzzy match; defaults to the caller)")
	issueSubscriberRemoveCmd.Flags().String("user-id", "", "Member or agent UUID to unsubscribe (mutually exclusive with --user)")
	issueSubscriberRemoveCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Issue commands
// ---------------------------------------------------------------------------

func runIssueList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if v, _ := cmd.Flags().GetBool("include-archived"); v {
		params.Set("include_archived", "true")
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		params.Set("priority", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	_, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		params.Set("assignee_id", aID)
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		params.Set("project_id", project.ID)
	}
	if mdFlags, _ := cmd.Flags().GetStringSlice("metadata"); len(mdFlags) > 0 {
		filter, err := buildMetadataFilterQueryParam(mdFlags)
		if err != nil {
			return err
		}
		params.Set("metadata", filter)
	}
	sortVal, _ := cmd.Flags().GetString("sort")
	if sortVal != "" {
		valid := false
		for _, c := range validIssueSortColumns {
			if c == sortVal {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid --sort %q; valid values: %s", sortVal, strings.Join(validIssueSortColumns, ", "))
		}
		params.Set("sort", sortVal)
	}
	if v, _ := cmd.Flags().GetString("direction"); v != "" {
		d := strings.ToLower(v)
		if d != "asc" && d != "desc" {
			return fmt.Errorf("invalid --direction %q; valid values: asc, desc", v)
		}
		// position (the manual board order) is always ascending, so the server
		// ignores --direction for it. Reject the combination up front rather
		// than silently dropping the flag — a passed-but-ignored flag is a
		// footgun, especially in scripts.
		if sortVal == "" || sortVal == "position" {
			return fmt.Errorf("--direction requires --sort to be one of %s; position (the default manual board order) is always ascending", strings.Join(directionalIssueSortColumns, ", "))
		}
		params.Set("direction", d)
	}

	path := "/api/issues"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	issuesRaw, _ := result["issues"].([]any)
	issuesRaw = filterIssuesUpdatedSince(cmd, issuesRaw)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		total, _ := result["total"].(float64)
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		hasMore := offset+len(issuesRaw) < int(total)
		wrapped := map[string]any{
			"issues":   issuesRaw,
			"total":    int(total),
			"limit":    limit,
			"offset":   offset,
			"has_more": hasMore,
		}
		return cli.PrintJSON(os.Stdout, wrapped)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	if fullID {
		headers = []string{"KEY", "ID", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	}
	actors := loadActorDisplayLookup(ctx, client)
	rows := make([][]string, 0, len(issuesRaw))
	for _, raw := range issuesRaw {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		assignee := formatAssignee(issue, actors)
		startDate := strVal(issue, "start_date")
		if startDate != "" && len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(issue, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		row := []string{
			issueDisplayKey(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
			assignee,
			startDate,
			dueDate,
		}
		if fullID {
			row = []string{
				issueDisplayKey(issue),
				strVal(issue, "id"),
				strVal(issue, "title"),
				strVal(issue, "status"),
				strVal(issue, "priority"),
				assignee,
				startDate,
				dueDate,
			}
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssuePullRequests(cmd *cobra.Command, args []string) error {
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

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/pull-requests", &result); err != nil {
		return fmt.Errorf("list issue pull requests: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	prs, _ := result["pull_requests"].([]any)
	printIssuePullRequestsTable(normalizePullRequestList(prs))
	return nil
}

func normalizePullRequestList(raw []any) []map[string]any {
	prs := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		pr, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prs = append(prs, pr)
	}
	return prs
}

func printIssuePullRequestsTable(prs []map[string]any) {
	headers := []string{"NUMBER", "STATE", "TITLE", "URL"}
	rows := make([][]string, 0, len(prs))
	for _, pr := range prs {
		rows = append(rows, []string{
			strVal(pr, "number"),
			strVal(pr, "state"),
			strVal(pr, "title"),
			pullRequestURL(pr),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func pullRequestURL(pr map[string]any) string {
	if url := strVal(pr, "url"); url != "" {
		return url
	}
	return strVal(pr, "html_url")
}

func runIssueGet(cmd *cobra.Command, args []string) error {
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

	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID, &issue); err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	// Emitted before the payload and on stderr, so it reaches the reader in
	// both output modes without becoming a field a JSON consumer has to parse
	// around.
	warnTerminalIssueIsARecord(issue)

	// Checked before the full payload is printed: asking for a span and getting
	// the whole description back would be indistinguishable from a long quote.
	spec, quoting, err := quoteSpecFromFlags(cmd)
	if err != nil {
		return err
	}
	if quoting {
		return printIssueQuote(cmd, issue, spec)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		assignee := formatAssignee(issue, actors)
		startDate := strVal(issue, "start_date")
		if startDate != "" && len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(issue, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE", "DESCRIPTION"}
		rows := [][]string{{
			issueDisplayKey(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
			assignee,
			startDate,
			dueDate,
			strVal(issue, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, issue)
}

// terminalIssueStatuses are the statuses that close an issue's body: the
// server refuses title/description writes on them (409). Mirrors
// isTerminalIssueStatus in server/internal/handler/issue.go.
var terminalIssueStatuses = map[string]bool{"done": true, "cancelled": true}

// warnTerminalIssueIsARecord tells the reader what a finished issue is, before
// they act on it.
//
// The response carries `status: "done"` and nothing else — a reader has to
// already know what that implies. What it implies is two separate things, and
// only one of them is about writing:
//
//   - The body is closed. Rewriting it is a 409, so anything read here is
//     final, not a draft that might still move.
//   - The body is a record of what was true when the work finished. That makes
//     it accurate about the past and silent about the present: a design
//     described here may have been replaced since, and the issue carries no
//     pointer to whatever replaced it.
//
// Deliberately NOT phrased as "expired". Why a decision was made and what was
// rejected are usually only written down here and are still true; an agent
// told the issue is stale discounts exactly the part that is worth reading.
func warnTerminalIssueIsARecord(issue map[string]any) {
	status := strVal(issue, "status")
	if !terminalIssueStatuses[status] {
		return
	}
	key := issueDisplayKey(issue)
	fmt.Fprintf(os.Stderr,
		"Note: %s is %s — a closed record. Its title and description are read-only (409 on write), "+
			"and they describe what was true when it finished, not necessarily what is true now. "+
			"Accurate about the past; not authoritative about the present. "+
			"Check the current state before acting on anything here, and look for a `superseded_by` "+
			"key in `multica issue metadata list %s` if a newer issue replaced it.\n",
		key, status, key)
}

// childStage extracts the integer stage from a child issue response map.
// Returns ok=false when the child is unstaged (stage null/absent).
func childStage(m map[string]any) (int, bool) {
	v, ok := m["stage"]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func runIssueChildren(cmd *cobra.Command, args []string) error {
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
		Issues []map[string]any `json:"issues"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/children", &resp); err != nil {
		return fmt.Errorf("list child issues: %w", err)
	}
	children := resp.Issues

	// Order by stage ascending (unstaged last), preserving the API's
	// within-stage order (position, then created_at desc).
	sort.SliceStable(children, func(i, j int) bool {
		si, oki := childStage(children[i])
		sj, okj := childStage(children[j])
		if oki != okj {
			return oki // staged before unstaged
		}
		return si < sj
	})

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		headers := []string{"STAGE", "KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE"}
		rows := make([][]string, 0, len(children))
		for _, c := range children {
			stageCell := "-"
			if s, ok := childStage(c); ok {
				stageCell = strconv.Itoa(s)
			}
			rows = append(rows, []string{
				stageCell,
				issueDisplayKey(c),
				strVal(c, "title"),
				strVal(c, "status"),
				strVal(c, "priority"),
				formatAssignee(c, actors),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	// JSON: group by stage so an agent can see, at a glance, how many
	// sub-issues there are and which stage each belongs to.
	type stageGroup struct {
		Stage  int              `json:"stage"`
		Total  int              `json:"total"`
		Done   int              `json:"done"`
		Issues []map[string]any `json:"issues"`
	}
	stages := []stageGroup{}
	unstaged := []map[string]any{}
	idxByStage := map[int]int{}
	for _, c := range children {
		s, ok := childStage(c)
		if !ok {
			unstaged = append(unstaged, c)
			continue
		}
		gi, seen := idxByStage[s]
		if !seen {
			stages = append(stages, stageGroup{Stage: s})
			gi = len(stages) - 1
			idxByStage[s] = gi
		}
		stages[gi].Issues = append(stages[gi].Issues, c)
		stages[gi].Total++
		if st := strVal(c, "status"); st == "done" || st == "cancelled" {
			stages[gi].Done++
		}
	}
	return cli.PrintJSON(os.Stdout, map[string]any{
		"total":    len(children),
		"stages":   stages,
		"unstaged": unstaged,
	})
}

// isHTTPURL reports whether path is an http:// or https:// URL.
// Used to skip URL-shaped values passed to --attachment, which only
// accepts local file paths. Trims surrounding whitespace because
// agent-generated commands sometimes copy URLs with stray spaces.
func isHTTPURL(path string) bool {
	p := strings.TrimSpace(path)
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

// ensureAttachmentWithinWorkdir applies the same workdir containment guard as
// --description-file / --content-file (MUL-4252) to a local --attachment path.
// An agent that writes a chart/report to a machine-shared path like /tmp and
// then attaches it could otherwise pick up another run's — possibly another
// workspace's — stale file (the image version of the /tmp/desc.md leak). URL
// values are filtered by the caller and never reach here. --allow-external-file
// overrides, mirroring the text-flag escape hatch.
func ensureAttachmentWithinWorkdir(cmd *cobra.Command, filePath string) error {
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); allow {
		return nil
	}
	within, err := fileWithinWorkingDir(filePath)
	if err != nil {
		return fmt.Errorf("resolve --attachment path %q: %w", filePath, err)
	}
	if !within {
		return fmt.Errorf(
			"--attachment path %q resolves outside the current working directory; "+
				"attach files generated inside the task workdir rather than machine-shared "+
				"paths like /tmp, where another run's stale file can be attached by mistake. "+
				"Pass --allow-external-file to override.",
			filePath)
	}
	return nil
}

// pendingAttachment is a local --attachment file that passed URL filtering and
// the workdir guard and has been read into memory, ready to upload.
type pendingAttachment struct {
	path string
	data []byte
}

// collectLocalAttachments validates and reads ALL local --attachment paths up
// front, before any upload. URL-shaped values are warned and skipped (the API
// only accepts local paths). Each remaining path is run through the MUL-4252
// workdir guard and read into memory; the first invalid or unreadable path
// returns an error with nothing uploaded. Both `issue create` and
// `comment add` share this so an invalid attachment can never leave an earlier
// one uploaded as an orphaned issue attachment while the issue/comment is never
// created (which would duplicate on retry).
func collectLocalAttachments(cmd *cobra.Command, attachments []string) ([]pendingAttachment, error) {
	pending := make([]pendingAttachment, 0, len(attachments))
	for _, filePath := range attachments {
		if isHTTPURL(filePath) {
			fmt.Fprintf(os.Stderr, "Skipping --attachment %q: URLs are not supported here, only local file paths.\n", filePath)
			continue
		}
		if err := ensureAttachmentWithinWorkdir(cmd, filePath); err != nil {
			return nil, err
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("read attachment %s: %w", filePath, readErr)
		}
		pending = append(pending, pendingAttachment{path: filePath, data: data})
	}
	return pending, nil
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	out := make([]string, 0, len(dst)+len(values))
	for _, v := range append(dst, values...) {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func quickCreateAttachmentIDsFromEnv() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("MULTICA_QUICK_CREATE_ATTACHMENT_IDS"))
	if raw == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("parse MULTICA_QUICK_CREATE_ATTACHMENT_IDS: %w", err)
	}
	return appendUniqueStrings(nil, ids...), nil
}

// currentMemberID returns the member id behind the current token, or "" when
// there is not one (agent tokens) or the lookup fails. Callers treat "" as
// "no default" rather than as an error.
func currentMemberID(ctx context.Context, client *cli.APIClient) string {
	var me struct {
		ID string `json:"id"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return ""
	}
	return me.ID
}

// testIssueTitlePrefix marks an issue as a throwaway.
//
// A title prefix rather than a label, because the surface that matters here is
// `multica issue list`, whose table has no labels column — a label would be
// invisible in exactly the place these need to be told apart. Labels remain the
// better answer for anything a person filters in the web UI.
const testIssueTitlePrefix = "[测试]"

// markTestIssueTitle adds the prefix unless it is already there. Idempotent
// because a caller who passes both --test and an already-prefixed title means
// one prefix, not two — and because agents retrying a create should not
// accumulate them.
func markTestIssueTitle(title string) string {
	trimmed := strings.TrimSpace(title)
	if strings.HasPrefix(trimmed, testIssueTitlePrefix) {
		return trimmed
	}
	return testIssueTitlePrefix + " " + trimmed
}

func runIssueCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	if isTest, _ := cmd.Flags().GetBool("test"); isTest {
		title = markTestIssueTitle(title)
	}
	statusFlag, _ := cmd.Flags().GetString("status")
	if statusFlag != "" {
		if err := validateIssueStatus(statusFlag); err != nil {
			return err
		}
	}
	priorityFlag, _ := cmd.Flags().GetString("priority")
	if priorityFlag != "" {
		if err := validateIssuePriority(priorityFlag); err != nil {
			return err
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// Use a longer timeout when attachments are present (file uploads can be slow).
	timeout := cli.APITimeout()
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = cli.AtLeastAPITimeout(60 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{"title": title}
	desc, hasDesc, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}
	if hasDesc {
		if err := guardLocalPathLinks(desc, "issue description",
			"Deliver the file itself with `multica issue create --attachment <path>` (repeatable) and drop the link."); err != nil {
			return err
		}
		body["description"] = desc
	}
	if statusFlag != "" {
		body["status"] = statusFlag
	}
	if priorityFlag != "" {
		body["priority"] = priorityFlag
	}
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		parent, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve parent issue: %w", err)
		}
		body["parent_issue_id"] = parent.ID
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		body["project_id"] = project.ID
	}
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetInt("stage")
		if stage < 1 {
			return fmt.Errorf("--stage must be >= 1")
		}
		body["stage"] = stage
	}
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		body["start_date"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}
	if v, _ := cmd.Flags().GetBool("allow-duplicate"); v {
		body["allow_duplicate"] = true
	}
	aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	} else if noAssignee, _ := cmd.Flags().GetBool("no-assignee"); !noAssignee {
		// Default the assignee to whoever is running the command. Filing an
		// issue from the CLI is almost always filing it for yourself, and an
		// unassigned issue is invisible in "my issues" — the list its author
		// is most likely to look at next.
		//
		// Best effort on purpose: an agent token has no member identity, and
		// /api/me can fail for reasons that have nothing to do with the issue
		// being created. Either way the issue is still created, just
		// unassigned — the old behaviour.
		if id := currentMemberID(ctx, client); id != "" {
			body["assignee_type"] = "member"
			body["assignee_id"] = id
		}
	}

	// Quick-create stamp: when the daemon sets MULTICA_QUICK_CREATE_TASK_ID
	// before invoking the agent, the agent's `multica issue create` call
	// inherits the env var and tags the new issue with origin_type=
	// quick_create + origin_id=<task_id>. The completion handler then
	// locates the issue deterministically by origin instead of "most
	// recent issue by this agent", which is racy when max_concurrent_tasks
	// > 1 and the agent is creating other issues in parallel.
	if taskID := os.Getenv("MULTICA_QUICK_CREATE_TASK_ID"); taskID != "" {
		body["origin_type"] = "quick_create"
		body["origin_id"] = taskID
	}
	attachmentIDs, _ := cmd.Flags().GetStringSlice("attachment-id")
	envAttachmentIDs, err := quickCreateAttachmentIDsFromEnv()
	if err != nil {
		return err
	}
	attachmentIDs = appendUniqueStrings(attachmentIDs, envAttachmentIDs...)
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}

	// Pre-validate attachments BEFORE creating the issue so a bad path can
	// never produce a half-created issue (which would otherwise trigger
	// callers — especially the agent doing quick-create — to retry the whole
	// `issue create` and end up with duplicates). URLs are warned+skipped, the
	// workdir guard is applied, and every local path is read upfront; a failure
	// here surfaces pre-create so the issue never lands.
	pending, err := collectLocalAttachments(cmd, attachments)
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		if msg, ok := activeDuplicateIssueCreateMessage(err); ok {
			return errors.New(msg)
		}
		return fmt.Errorf("create issue: %w", err)
	}

	// Upload attachments and link them to the newly created issue.
	// Failures here are partial-success: the issue exists already, so
	// turning a non-zero exit on the caller would invite a retry that
	// duplicates the issue. Warn on stderr and continue.
	issueID := strVal(result, "id")
	for _, att := range pending {
		if _, uploadErr := client.UploadFile(ctx, att.data, att.path, issueID); uploadErr != nil {
			fmt.Fprintf(os.Stderr, "warning: upload attachment %s failed (issue already created, %s): %v\n",
				att.path, strVal(result, "identifier"), uploadErr)
			continue
		}
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
	}

	// Give the new card its pinned ledger index, so the rule holds without
	// anyone having to remember it. Best effort — see postIssueIndexComment.
	postIssueIndexComment(ctx, client, issueID, strVal(result, "identifier"), resolveIssueIndexSession(cmd))

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			issueDisplayKey(result),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func activeDuplicateIssueCreateMessage(err error) (string, bool) {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return "", false
	}
	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(httpErr.Body), &payload) != nil {
		return "", false
	}
	if payload.Code != "active_duplicate_issue" || payload.Error == "" {
		return "", false
	}
	return payload.Error, true
}

func runIssueUpdate(cmd *cobra.Command, args []string) error {
	statusChanged := cmd.Flags().Changed("status")
	statusFlag, _ := cmd.Flags().GetString("status")
	if statusChanged {
		if err := validateIssueStatus(statusFlag); err != nil {
			return err
		}
	}
	priorityChanged := cmd.Flags().Changed("priority")
	priorityFlag, _ := cmd.Flags().GetString("priority")
	if priorityChanged {
		if err := validateIssuePriority(priorityFlag); err != nil {
			return err
		}
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

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") || cmd.Flags().Changed("description-file") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return err
		}
		// `issue update` has no --attachment flag, so the hint must point at the
		// command that does. Telling the agent to "pass --attachment" here would
		// name an argument this command rejects.
		if err := guardLocalPathLinks(desc, "issue description",
			"`multica issue update` cannot carry files — deliver the file with `multica issue comment add <issue-id> --attachment <path>` instead, and drop the link."); err != nil {
			return err
		}
		body["description"] = desc
	}
	if statusChanged {
		body["status"] = statusFlag
	}
	if priorityChanged {
		body["priority"] = priorityFlag
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			project, err := resolveProjectID(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve project: %w", err)
			}
			body["project_id"] = project.ID
		}
	}
	if cmd.Flags().Changed("start-date") {
		v, _ := cmd.Flags().GetString("start-date")
		body["start_date"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}
	if cmd.Flags().Changed("assignee") || cmd.Flags().Changed("assignee-id") {
		aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		if hasAssignee {
			body["assignee_type"] = aType
			body["assignee_id"] = aID
		}
	}
	if cmd.Flags().Changed("parent") {
		v, _ := cmd.Flags().GetString("parent")
		if v == "" {
			body["parent_issue_id"] = nil
		} else {
			parent, err := resolveIssueRef(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve parent issue: %w", err)
			}
			body["parent_issue_id"] = parent.ID
		}
	}
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetInt("stage")
		if stage < 1 {
			return fmt.Errorf("--stage must be >= 1")
		}
		body["stage"] = stage
	}
	if cmd.Flags().Changed("position") {
		v, _ := cmd.Flags().GetFloat64("position")
		body["position"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --priority, --assignee, etc.")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID, body, &result); err != nil {
		return fmt.Errorf("update issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			issueDisplayKey(result),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runIssueAssign(cmd *cobra.Command, args []string) error {
	toName, _ := cmd.Flags().GetString("to")
	unassign, _ := cmd.Flags().GetBool("unassign")
	toNameSet := cmd.Flags().Changed("to")
	toIDSet := cmd.Flags().Changed("to-id")

	if !toNameSet && !toIDSet && !unassign {
		return fmt.Errorf("provide --to <name>, --to-id <uuid>, or --unassign")
	}
	if (toNameSet || toIDSet) && unassign {
		return fmt.Errorf("--to/--to-id and --unassign are mutually exclusive")
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

	body := map[string]any{}
	displayTarget := toName
	if unassign {
		body["assignee_type"] = nil
		body["assignee_id"] = nil
	} else {
		aType, aID, _, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "to", "to-id", issueAssigneeKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
		if displayTarget == "" {
			displayTarget = loadActorDisplayLookup(ctx, client).actor(aType, aID)
		}
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID, body, &result); err != nil {
		return fmt.Errorf("assign issue: %w", err)
	}

	if unassign {
		fmt.Fprintf(os.Stderr, "Issue %s unassigned.\n", issueDisplayKey(result))
	} else {
		fmt.Fprintf(os.Stderr, "Issue %s assigned to %s.\n", issueDisplayKey(result), displayTarget)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// runIssueDelete removes an issue for good.
//
// The guard is about the ONE consequence a caller cannot see coming: the
// parent link is ON DELETE SET NULL, so deleting a parent silently promotes
// its children to top-level issues rather than removing them. Refusing by
// default turns that into a decision instead of a discovery; --force is the
// way to say "yes, orphan them".
func runIssueDelete(cmd *cobra.Command, args []string) error {
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

	force, _ := cmd.Flags().GetBool("force")
	if !force {
		var children struct {
			Issues []map[string]any `json:"issues"`
		}
		if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/children", &children); err != nil {
			return fmt.Errorf("check sub-issues: %w", err)
		}
		if len(children.Issues) > 0 {
			return fmt.Errorf(
				"%s has %d sub-issue(s), which would be orphaned rather than deleted; "+
					"re-run with --force to delete it anyway, or use `issue archive %s` "+
					"to take the whole subtree out of view",
				issueRef.Display, len(children.Issues), issueRef.Display)
		}
	}

	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID); err != nil {
		return fmt.Errorf("delete issue: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Issue %s deleted.\n", issueRef.Display)
	return nil
}

func runIssueArchive(cmd *cobra.Command, args []string) error {
	return runIssueArchiveState(cmd, args[0], true)
}

func runIssueUnarchive(cmd *cobra.Command, args []string) error {
	return runIssueArchiveState(cmd, args[0], false)
}

// runIssueArchiveState drives both directions. The server moves the whole
// sub-issue subtree, so the response is a list and the count is worth
// reporting — archiving one requirement can take a dozen cards off the board.
func runIssueArchiveState(cmd *cobra.Command, id string, archive bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, id)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	action := "unarchive"
	if archive {
		action = "archive"
	}

	var result struct {
		Issues []map[string]any `json:"issues"`
	}
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/"+action, nil, &result); err != nil {
		return fmt.Errorf("%s issue: %w", action, err)
	}

	verb := "archived"
	if !archive {
		verb = "restored"
	}
	fmt.Fprintf(os.Stderr, "Issue %s %s (%d issue(s) affected).\n",
		issueRef.Display, verb, len(result.Issues))

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Issues)
	}
	return nil
}

func runIssueStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]

	if err := validateIssueStatus(status); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, id)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body, err := issueStatusBody(cmd, status)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Issue %s status changed to %s.\n", issueDisplayKey(result), status)

	// Finishing is the moment the run is still fresh enough to reconstruct.
	// A line, not a step: see retro_prompt.go for why this never runs itself.
	if shouldPromptRetro(status, "") {
		writeRetroPrompt(os.Stderr, issueDisplayKey(result), "状态改为 "+status)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Reorder command
// ---------------------------------------------------------------------------

// registerIssueReorderFlags wires the reorder command's flags and declares its
// target selector as a cobra flag group: mutually exclusive so at most one of
// --before/--after/--top/--bottom is accepted, and one-required so at least one
// must be given. Declaring the rule (instead of counting the flags by hand in
// runIssueReorder) lets cobra reject zero-target and multi-target invocations
// before RunE with a canonical message, and makes shell completion group-aware
// — it hides the sibling target flags once one of them is set. It is shared with
// the command's tests so they exercise the exact flag-group wiring that ships.
func registerIssueReorderFlags(cmd *cobra.Command) {
	cmd.Flags().String("before", "", "Place the issue directly above this issue (same column)")
	cmd.Flags().String("after", "", "Place the issue directly below this issue (same column)")
	cmd.Flags().Bool("top", false, "Move the issue to the top of its status column")
	cmd.Flags().Bool("bottom", false, "Move the issue to the bottom of its status column")
	cmd.Flags().String("output", "json", "Output format: table or json")
	cmd.MarkFlagsMutuallyExclusive("before", "after", "top", "bottom")
	cmd.MarkFlagsOneRequired("before", "after", "top", "bottom")
}

// runIssueReorder repositions an issue inside its current status column. The
// new position is computed client-side by computeReorderPosition, which mirrors
// the board/list drag-and-drop math (computePosition in
// packages/views/issues/utils/drag-utils.ts) so the CLI and UI agree on where
// an issue lands. Only the issue's own position changes; its column membership
// (status) is left untouched, so cross-column moves still go through
// `issue status`.
func runIssueReorder(cmd *cobra.Command, args []string) error {
	before, _ := cmd.Flags().GetString("before")
	after, _ := cmd.Flags().GetString("after")
	top, _ := cmd.Flags().GetBool("top")
	bottom, _ := cmd.Flags().GetBool("bottom")

	// "Exactly one of --before/--after/--top/--bottom" is enforced declaratively
	// by the command's mutually-exclusive, one-required flag group (see
	// registerIssueReorderFlags), so cobra rejects zero-target and multi-target
	// invocations before RunE runs. Cobra keys off flag *presence* (Changed),
	// not value, so guard the cases it cannot see: a --before/--after passed
	// empty (e.g. an unset shell variable), or a --top/--bottom explicitly set
	// to false (e.g. `--top=false` from a generated command). Each counts as
	// "set" for the group yet selects no real target, and would otherwise fall
	// through to a confusing "not found in column" error.
	if cmd.Flags().Changed("before") && before == "" {
		return fmt.Errorf("--before requires an issue ID or key")
	}
	if cmd.Flags().Changed("after") && after == "" {
		return fmt.Errorf("--after requires an issue ID or key")
	}
	if cmd.Flags().Changed("top") && !top {
		return fmt.Errorf("--top cannot be set to false; pass it on its own to move the issue to the top of its column")
	}
	if cmd.Flags().Changed("bottom") && !bottom {
		return fmt.Errorf("--bottom cannot be set to false; pass it on its own to move the issue to the bottom of its column")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wsID := client.WorkspaceID
	if wsID == "" {
		wsID, err = requireWorkspaceID(cmd)
		if err != nil {
			return err
		}
	}

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	target, err := fetchIssue(ctx, client, issueRef.ID)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}
	status := strVal(target, "status")
	if status == "" {
		return fmt.Errorf("issue %s has no status, cannot determine its column", issueRef.Display)
	}

	// Resolve the relative target up front, before any no-op shortcut, so a bad
	// --before/--after value (typo, self-reference) always errors instead of
	// being swallowed by the single-issue-column fast path below.
	relative := before != "" || after != ""
	var otherRef resolvedID
	if relative {
		otherInput := before
		if after != "" {
			otherInput = after
		}
		otherRef, err = resolveIssueRef(ctx, client, otherInput)
		if err != nil {
			return fmt.Errorf("resolve target issue: %w", err)
		}
		if otherRef.ID == issueRef.ID {
			return fmt.Errorf("cannot reorder issue %s relative to itself", issueRef.Display)
		}
	}

	// Scope the column to the issue's own project (when it has one) so the
	// computed position lands relative to the project board the issue lives on,
	// not a blend of every project's issues in this status.
	projectID := strVal(target, "project_id")
	column, err := fetchIssueColumn(ctx, client, wsID, projectID, status)
	if err != nil {
		return fmt.Errorf("list %s column: %w", status, err)
	}

	// Build the column order with the target removed, plus a lookup of every
	// position in the column (the target's own included, for the no-op check).
	positions := make(map[string]float64, len(column))
	ordered := make([]string, 0, len(column))
	for _, raw := range column {
		id := strVal(raw, "id")
		if id == "" {
			continue
		}
		positions[id] = floatVal(raw, "position")
		if id != issueRef.ID {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) == 0 {
		// The active issue is alone in its column. A relative move cannot
		// succeed here (its target is necessarily in another column), so report
		// that rather than a misleading "nothing to reorder".
		if relative {
			return reorderTargetNotInColumnError(ctx, client, otherRef, issueRef, status)
		}
		fmt.Fprintf(os.Stderr, "Issue %s is the only issue in the %s column; nothing to reorder.\n", issueRef.Display, status)
		return issueReorderOutput(cmd, target)
	}

	insertIdx := 0
	switch {
	case top:
		insertIdx = 0
	case bottom:
		insertIdx = len(ordered)
	default:
		idx := indexOfString(ordered, otherRef.ID)
		if idx == -1 {
			return reorderTargetNotInColumnError(ctx, client, otherRef, issueRef, status)
		}
		if before != "" {
			insertIdx = idx
		} else {
			insertIdx = idx + 1
		}
	}

	reordered := make([]string, 0, len(ordered)+1)
	reordered = append(reordered, ordered[:insertIdx]...)
	reordered = append(reordered, issueRef.ID)
	reordered = append(reordered, ordered[insertIdx:]...)

	currentPos := positions[issueRef.ID]
	newPos := computeReorderPosition(reordered, issueRef.ID, positions, currentPos)
	if newPos == currentPos {
		fmt.Fprintf(os.Stderr, "Issue %s is already in that position.\n", issueRef.Display)
		return issueReorderOutput(cmd, target)
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID), map[string]any{"position": newPos}, &result); err != nil {
		return fmt.Errorf("reorder issue: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Issue %s reordered.\n", issueDisplayKey(result))
	return issueReorderOutput(cmd, result)
}

// issueReorderOutput prints an issue map as a table or JSON, matching the
// update command's output contract.
func issueReorderOutput(cmd *cobra.Command, issue map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			issueDisplayKey(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, issue)
}

// reorderTargetNotInColumnError explains why a --before/--after target could
// not be used. It fetches the target only to report its actual column in the
// message, so the common mistake (target lives in a different column) gets a
// precise, actionable error instead of a bare "not found".
func reorderTargetNotInColumnError(ctx context.Context, client *cli.APIClient, otherRef, issueRef resolvedID, status string) error {
	if other, err := fetchIssue(ctx, client, otherRef.ID); err == nil {
		if otherStatus := strVal(other, "status"); otherStatus != "" && otherStatus != status {
			return fmt.Errorf("issue %s is in the %q column but %s is in %q; move one with `multica issue status` first, or pick a target in the same column", otherRef.Display, otherStatus, issueRef.Display, status)
		}
	}
	return fmt.Errorf("issue %s was not found in the %q column", otherRef.Display, status)
}

// fetchIssue retrieves a single issue by canonical ID.
func fetchIssue(ctx context.Context, client *cli.APIClient, id string) (map[string]any, error) {
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(id), &issue); err != nil {
		return nil, err
	}
	return issue, nil
}

// fetchIssueColumn returns every issue in a status column ordered by position
// ascending, paginating through the list endpoint so columns larger than one
// page (the server caps a page at 100) still produce a complete, correctly
// ordered set. A non-empty projectID scopes the column to that project,
// matching a project board; an empty projectID lists the whole workspace
// column.
func fetchIssueColumn(ctx context.Context, client *cli.APIClient, workspaceID, projectID, status string) ([]map[string]any, error) {
	var all []map[string]any
	offset := 0
	for {
		params := url.Values{}
		params.Set("workspace_id", workspaceID)
		params.Set("status", status)
		if projectID != "" {
			params.Set("project_id", projectID)
		}
		params.Set("sort", "position")
		params.Set("limit", "100")
		params.Set("offset", fmt.Sprintf("%d", offset))

		var result map[string]any
		if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &result); err != nil {
			return nil, err
		}
		page, _ := result["issues"].([]any)
		for _, raw := range page {
			if m, ok := raw.(map[string]any); ok {
				all = append(all, m)
			}
		}
		total, _ := result["total"].(float64)
		offset += len(page)
		if len(page) == 0 || offset >= int(total) {
			break
		}
	}
	return all, nil
}

// computeReorderPosition computes the position that places activeID at its
// index in ids. It mirrors computePosition in
// packages/views/issues/utils/drag-utils.ts: the top of a column is one less
// than the next item, the bottom is one more than the previous, and any
// interior slot is the midpoint of its neighbours. fallback is returned when
// activeID is alone in (or absent from) the column, leaving its position
// unchanged.
func computeReorderPosition(ids []string, activeID string, positions map[string]float64, fallback float64) float64 {
	idx := indexOfString(ids, activeID)
	if idx == -1 || len(ids) == 1 {
		return fallback
	}
	if idx == 0 {
		return positions[ids[1]] - 1
	}
	if idx == len(ids)-1 {
		return positions[ids[idx-1]] + 1
	}
	return (positions[ids[idx-1]] + positions[ids[idx+1]]) / 2
}

func indexOfString(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

func floatVal(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

// ---------------------------------------------------------------------------
// Comment commands
// ---------------------------------------------------------------------------

func runIssueCommentList(cmd *cobra.Command, args []string) error {
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

	since, _ := cmd.Flags().GetString("since")
	thread, _ := cmd.Flags().GetString("thread")
	recent, _ := cmd.Flags().GetInt("recent")
	tail, _ := cmd.Flags().GetInt("tail")
	rootsOnly, _ := cmd.Flags().GetBool("roots-only")
	summary, _ := cmd.Flags().GetBool("summary")
	full, _ := cmd.Flags().GetBool("full")
	// Flags().Changed distinguishes "user did not pass --recent" from
	// "user explicitly passed --recent 0" (or a negative value). The
	// GetInt zero-value collapses both cases, which would otherwise
	// cause us to silently drop an invalid value and fall back to the
	// default unparameterized list — exactly the drift Elon flagged in
	// the PR #2787 second review. --tail follows the same pattern, and
	// also keeps "--tail 0" (root-only) distinguishable from "no --tail".
	recentSet := cmd.Flags().Changed("recent")
	tailSet := cmd.Flags().Changed("tail")
	before, _ := cmd.Flags().GetString("before")
	beforeID, _ := cmd.Flags().GetString("before-id")

	// Mirror the server-side combination rules client-side so the user gets
	// a clear local error instead of a 400 round-trip. These match the
	// validation in handler.ListComments (server/internal/handler/comment.go).
	if recentSet && recent <= 0 {
		return fmt.Errorf("--recent must be a positive integer")
	}
	if tailSet && tail < 0 {
		return fmt.Errorf("--tail must be a non-negative integer (0 returns just the thread root)")
	}
	if thread != "" && recentSet {
		return fmt.Errorf("--thread and --recent are mutually exclusive")
	}
	if rootsOnly && thread != "" {
		return fmt.Errorf("--roots-only and --thread are mutually exclusive")
	}
	if rootsOnly && recentSet {
		return fmt.Errorf("--roots-only and --recent are mutually exclusive")
	}
	if rootsOnly && tailSet {
		return fmt.Errorf("--roots-only and --tail are mutually exclusive")
	}
	if rootsOnly && before != "" {
		return fmt.Errorf("--roots-only does not support --before / --before-id")
	}
	if tailSet && thread == "" {
		return fmt.Errorf("--tail requires --thread (it is a thread-scoped limit)")
	}
	if (before == "") != (beforeID == "") {
		return fmt.Errorf("--before and --before-id must be set together (composite cursor for stable pagination)")
	}
	if before != "" && !recentSet && !(thread != "" && tailSet) {
		return fmt.Errorf("--before / --before-id require --recent (thread cursor) or --thread + --tail (reply cursor)")
	}
	// --phase filters the returned comments client-side; the API has no phase
	// parameter. That is exact against the default list (which returns every
	// comment) but a lie next to the windowing flags: those pick a window
	// first, so "the 10 most recent threads, of which the ones in 评审 2" would
	// silently look like "everything in 评审 2". Reject rather than mislead.
	phaseRef, _ := cmd.Flags().GetString("phase")
	phaseRef = strings.TrimSpace(phaseRef)
	if phaseRef != "" {
		if recentSet || tailSet || thread != "" || before != "" {
			return fmt.Errorf("--phase cannot be combined with --recent / --tail / --thread / --before " +
				"(those pick a window before the phase filter could apply); use --phase with the default list, --since, --roots-only, or --summary")
		}
	}

	params := url.Values{}
	if since != "" {
		params.Set("since", since)
	}
	if rootsOnly {
		params.Set("roots_only", "true")
	}
	if summary {
		params.Set("summary", "true")
	}
	// Resolve-aware folding is the default on the complete-thread reads (default
	// list, --recent, --thread without --tail): a resolved thread collapses to
	// root + conclusion so an agent does not pay tokens for settled discussion.
	// --full opts out. The partial-thread reads (--since / --tail) and
	// --roots-only cannot be folded safely (server rejects fold there), so we
	// never send fold for them and --full is a harmless no-op. Sending fold only
	// when eligible keeps every existing read command — including the prompt's
	// `--thread <id> --tail 30` cold path — working unchanged.
	foldEligible := !rootsOnly && since == "" && !tailSet
	if foldEligible && !full {
		params.Set("fold", "true")
	}
	if thread != "" {
		params.Set("thread", thread)
	}
	if tailSet {
		params.Set("tail", fmt.Sprintf("%d", tail))
	}
	if recentSet {
		params.Set("recent", fmt.Sprintf("%d", recent))
	}
	if before != "" {
		params.Set("before", before)
		params.Set("before_id", beforeID)
	}

	path := "/api/issues/" + issueRef.ID + "/comments"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var comments []map[string]any
	respHeaders, err := client.GetJSONWithHeaders(ctx, path, &comments)
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}
	// The server emits the next-page cursor in headers when there is likely
	// an older page. Surface it on stderr so an operator (and the agent
	// prompt update that follows this PR) can scroll deeper without having
	// to dig into the raw HTTP response. Label depends on which paging mode
	// the caller is in — under --recent the cursor is a thread cursor;
	// under --thread + --tail it is a reply cursor inside that thread.
	if nb := respHeaders.Get("X-Multica-Next-Before"); nb != "" {
		if nbid := respHeaders.Get("X-Multica-Next-Before-Id"); nbid != "" {
			label := "Next thread cursor"
			if thread != "" && tailSet {
				label = "Next reply cursor"
			}
			fmt.Fprintf(os.Stderr, "%s: --before %s --before-id %s\n", label, nb, nbid)
		}
	}

	if phaseRef != "" {
		phase, resolveErr := resolveIssuePhase(ctx, client, issueRef.ID, phaseRef)
		if resolveErr != nil {
			return resolveErr
		}
		filtered := make([]map[string]any, 0, len(comments))
		for _, c := range comments {
			if strVal(c, "phase_id") == phase.ID {
				filtered = append(filtered, c)
			}
		}
		comments = filtered
		fmt.Fprintf(os.Stderr, "Phase %q: %d comment(s).\n", phase.Name, len(comments))
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		if compact, _ := cmd.Flags().GetBool("compact"); compact {
			compactComments(comments)
		}
		return cli.PrintJSON(os.Stdout, comments)
	}

	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "PARENT", "AUTHOR", "TYPE", "CONTENT", "CREATED"}
	rows := make([][]string, 0, len(comments))
	for _, c := range comments {
		content := strVal(c, "content")
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		created := strVal(c, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		parentID := strVal(c, "parent_id")
		if parentID == "" {
			parentID = "—"
		}
		rows = append(rows, []string{
			strVal(c, "id"),
			parentID,
			actors.actor(strVal(c, "author_type"), strVal(c, "author_id")),
			commentTableKind(c),
			content,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// commentTableKind is what the TYPE column says for one comment.
//
// The resolution is the one comment in a thread a reader has to be able to
// find: it is what a folded read keeps, and what the app marks in green. The
// JSON has carried resolved_at all along, but a table reader could not see it,
// so a settled thread printed as an undifferentiated list.
//
// Folded into TYPE rather than given a column: the row is already six columns
// wide, and no comment is usefully both a resolution and something else — a
// reader asking "which one is the conclusion" is not also asking whether it was
// a system note.
func commentTableKind(c map[string]any) string {
	if strVal(c, "resolved_at") != "" {
		return "resolution"
	}
	return strVal(c, "type")
}

// locateAnchorInDescription finds the character offset of an anchor passage in
// the issue's current description.
//
// The offset is resolved here rather than asked of the caller because it is a
// property of the document, not of the comment: an agent choosing a passage
// knows the words, not where they sit in a flattened string. Resolving it also
// makes a mistyped passage fail HERE, loudly, instead of silently becoming a
// comment that never highlights anything.
func locateAnchorInDescription(
	ctx context.Context,
	client *cli.APIClient,
	issueID, anchor string,
	occurrence int,
) (int, error) {
	if occurrence < 1 {
		return 0, fmt.Errorf("--anchor-occurrence must be 1 or greater")
	}
	var issue struct {
		Description *string `json:"description"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueID, &issue); err != nil {
		return 0, fmt.Errorf("read issue description: %w", err)
	}
	description := ""
	if issue.Description != nil {
		description = *issue.Description
	}

	offset, err := anchorOffsetInText(description, anchor, occurrence)
	if err != nil {
		return 0, fmt.Errorf("%w (issue %s)", err, issueID)
	}
	return offset, nil
}

// anchorOffsetInText locates the nth occurrence of `anchor` in `text`, in
// CHARACTER offsets.
//
// Characters, not bytes, because that is the coordinate system the editor
// resolves anchors in; a byte offset would land mid-character on any CJK
// description and the highlight would never match.
func anchorOffsetInText(text, anchor string, occurrence int) (int, error) {
	needle := strings.TrimSpace(anchor)
	if needle == "" {
		return 0, fmt.Errorf("--anchor must not be blank")
	}
	haystack := []rune(text)
	target := []rune(needle)
	found := 0
	for i := 0; i+len(target) <= len(haystack); i++ {
		if string(haystack[i:i+len(target)]) != needle {
			continue
		}
		found++
		if found == occurrence {
			return i, nil
		}
	}
	if found == 0 {
		return 0, fmt.Errorf("--anchor text does not appear in the description; copy the passage verbatim")
	}
	return 0, fmt.Errorf(
		"--anchor text appears %d time(s) in the description, but occurrence %d was requested",
		found, occurrence)
}

func runIssueCommentAdd(cmd *cobra.Command, args []string) error {
	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if !hasContent {
		return fmt.Errorf("--content, --content-stdin, or --content-file is required")
	}
	if err := guardLocalPathLinks(content, "comment body",
		"Deliver the file itself with `multica issue comment add <issue-id> --attachment <path>` (repeatable) and drop the link."); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// Use a longer timeout when attachments are present (file uploads can be slow).
	timeout := cli.APITimeout()
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = cli.AtLeastAPITimeout(60 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	issueID := issueRef.ID

	// Validate and read ALL attachments before uploading any. URLs are skipped
	// with a warning — `--attachment` only accepts local file paths. Reading
	// everything up front means a later invalid path (external / symlink escape
	// caught by the workdir guard) aborts the call with ZERO uploads, instead
	// of leaving an earlier file uploaded as an orphaned issue attachment while
	// the comment is never posted (which would duplicate on retry — MUL-4252).
	pending, err := collectLocalAttachments(cmd, attachments)
	if err != nil {
		return err
	}
	var attachmentIDs []string
	for _, att := range pending {
		id, uploadErr := client.UploadFile(ctx, att.data, att.path, issueID)
		if uploadErr != nil {
			return fmt.Errorf("upload attachment %s: %w", att.path, uploadErr)
		}
		attachmentIDs = append(attachmentIDs, id)
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
	}

	body := map[string]any{"content": content}
	if parentID, _ := cmd.Flags().GetString("parent"); parentID != "" {
		body["parent_id"] = parentID
	}
	if anchor, _ := cmd.Flags().GetString("anchor"); strings.TrimSpace(anchor) != "" {
		occurrence, _ := cmd.Flags().GetInt("anchor-occurrence")
		offset, locateErr := locateAnchorInDescription(ctx, client, issueID, anchor, occurrence)
		if locateErr != nil {
			return locateErr
		}
		body["anchor_text"] = strings.TrimSpace(anchor)
		body["anchor_offset"] = offset
	}
	if phaseRef, _ := cmd.Flags().GetString("phase"); strings.TrimSpace(phaseRef) != "" {
		// Resolved here rather than passed through: the server takes a phase
		// UUID, and the name is what a person or an agent actually has.
		phase, resolveErr := resolveIssuePhase(ctx, client, issueID, phaseRef)
		if resolveErr != nil {
			return resolveErr
		}
		body["phase_id"] = phase.ID
	}
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueID+"/comments", body, &result); err != nil {
		return fmt.Errorf("add comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment added to issue %s.\n", issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueCommentEdit(cmd *cobra.Command, args []string) error {
	commentID := strings.TrimSpace(args[0])
	// Same guard as `comment get`: the server would 400 a truncated id, but
	// only the CLI knows the caller probably copied a shortened one out of
	// a table and can say where the full UUID comes from.
	if !uuidRegexp.MatchString(commentID) {
		return fmt.Errorf(
			"comment id %q is not a full UUID; copy it from a comment card in the app, "+
				"or from the ID column of `multica issue comment list <issue-id>`", args[0])
	}

	target, err := commentEditTargetFromFlags(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	// A span or append edit cannot know its own result — cannot even tell
	// whether the anchors are ambiguous — until the current body arrives, so
	// the read comes first and every anchor error is decided against real
	// text rather than a guess.
	var current string
	if target.kind != commentEditReplaceBody {
		var comment map[string]any
		if err := client.GetJSON(ctx, "/api/comments/"+commentID, &comment); err != nil {
			return fmt.Errorf("read comment before edit: %w", err)
		}
		current = strVal(comment, "content")
	}

	content, err := target.apply(current)
	if err != nil {
		return err
	}
	// `edit` takes no --attachment, so the escape hatch stays `add`: say so
	// in the guard's remedy rather than pointing at a flag this command
	// doesn't have.
	if err := guardLocalPathLinks(content, "comment body",
		"Deliver the file itself with `multica issue comment add <issue-id> --attachment <path>` (repeatable) and drop the link."); err != nil {
		return err
	}

	// No attachment_ids key: the server treats a missing key as "keep the
	// existing attachments" and only a non-nil (even empty) list as replace.
	body := map[string]any{"content": content}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/comments/"+commentID, body, &result); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment %s updated.\n", commentID)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueCommentGet(cmd *cobra.Command, args []string) error {
	commentID := strings.TrimSpace(args[0])
	// Rejected here rather than sent: the server would answer 400 for a
	// truncated id, but the message that matters is WHICH id to use, and only
	// the CLI knows the caller probably copied a shortened one out of a table.
	if !uuidRegexp.MatchString(commentID) {
		return fmt.Errorf(
			"comment id %q is not a full UUID; copy it from a comment card in the app, "+
				"or from the ID column of `multica issue comment list <issue-id>`", args[0])
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var comment map[string]any
	if err := client.GetJSON(ctx, "/api/comments/"+commentID, &comment); err != nil {
		return fmt.Errorf("get comment: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, comment)
	}

	actors := loadActorDisplayLookup(ctx, client)
	fmt.Fprintf(os.Stdout, "ID:      %s\n", strVal(comment, "id"))
	fmt.Fprintf(os.Stdout, "Issue:   %s\n", strVal(comment, "issue_id"))
	fmt.Fprintf(os.Stdout, "Author:  %s\n",
		actors.actor(strVal(comment, "author_type"), strVal(comment, "author_id")))
	fmt.Fprintf(os.Stdout, "Created: %s\n", strVal(comment, "created_at"))
	if parent := strVal(comment, "parent_id"); parent != "" {
		fmt.Fprintf(os.Stdout, "Parent:  %s\n", parent)
	}
	if resolved := strVal(comment, "resolved_at"); resolved != "" {
		fmt.Fprintf(os.Stdout, "Resolved: %s\n", resolved)
	}
	fmt.Fprintf(os.Stdout, "\n%s\n", strVal(comment, "content"))
	return nil
}

func runIssueCommentDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/comments/"+args[0]); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment %s deleted.\n", args[0])
	return nil
}

func runIssueCommentResolve(cmd *cobra.Command, args []string) error {
	return runIssueCommentResolution(cmd, args[0], true)
}

func runIssueCommentUnresolve(cmd *cobra.Command, args []string) error {
	return runIssueCommentResolution(cmd, args[0], false)
}

func runIssueCommentPin(cmd *cobra.Command, args []string) error {
	return runIssueCommentPinning(cmd, args[0], true)
}

func runIssueCommentUnpin(cmd *cobra.Command, args []string) error {
	return runIssueCommentPinning(cmd, args[0], false)
}

func runIssueCommentPinning(cmd *cobra.Command, commentID string, pin bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/comments/" + url.PathEscape(commentID) + "/pin"
	var result map[string]any
	if pin {
		if err := client.PostJSON(ctx, path, nil, &result); err != nil {
			return fmt.Errorf("pin comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s pinned.\n", commentID)
	} else {
		if err := client.DeleteJSONResponse(ctx, path, &result); err != nil {
			return fmt.Errorf("unpin comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s unpinned.\n", commentID)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueCommentResolution(cmd *cobra.Command, commentID string, resolve bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/comments/" + url.PathEscape(commentID) + "/resolve"
	var result map[string]any
	if resolve {
		if err := client.PostJSON(ctx, path, nil, &result); err != nil {
			return fmt.Errorf("resolve comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s resolved.\n", commentID)
	} else {
		if err := client.DeleteJSONResponse(ctx, path, &result); err != nil {
			return fmt.Errorf("unresolve comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s unresolved.\n", commentID)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Execution history commands
// ---------------------------------------------------------------------------

func runIssueRuns(cmd *cobra.Command, args []string) error {
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

	var runs []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/task-runs", &runs); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runs)
	}

	actors := loadActorDisplayLookup(ctx, client)
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "AGENT", "STATUS", "BRANCH", "CHANGED", "STARTED", "COMPLETED", "ERROR"}
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		started := strVal(r, "started_at")
		if len(started) >= 16 {
			started = started[:16]
		}
		completed := strVal(r, "completed_at")
		if len(completed) >= 16 {
			completed = completed[:16]
		}
		errMsg := strVal(r, "error")
		if utf8.RuneCountInString(errMsg) > 50 {
			runes := []rune(errMsg)
			errMsg = string(runes[:47]) + "..."
		}
		branch, changed := runDeliverySummary(r)
		rows = append(rows, []string{
			displayID(strVal(r, "id"), fullID),
			actors.agent(strVal(r, "agent_id")),
			strVal(r, "status"),
			branch,
			changed,
			started,
			completed,
			errMsg,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// runDeliverySummary extracts the per-run delivery snapshot (COC-285) from a
// task-run row: the completion request persists inside the result JSONB, so
// branch and changed-file count surface without a dedicated endpoint.
func runDeliverySummary(run map[string]any) (branch, changed string) {
	result, _ := run["result"].(map[string]any)
	if result == nil {
		return "", ""
	}
	snap, _ := result["delivery_snapshot"].(map[string]any)
	if snap == nil {
		return "", ""
	}
	branch, _ = snap["branch"].(string)
	files, _ := snap["changed_files"].([]any)
	if len(files) > 0 {
		changed = fmt.Sprintf("%d", len(files))
	}
	if dirty, _ := snap["dirty"].(bool); dirty && changed == "" {
		changed = "?"
	}
	return branch, changed
}

func runIssueUsage(cmd *cobra.Command, args []string) error {
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

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/usage", &result); err != nil {
		return fmt.Errorf("get issue usage: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	// JSON numbers decode to float64; formatMetadataValue renders them as clean
	// integers (no scientific notation for large cache-token counts).
	headers := []string{"INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE", "RUNS"}
	rows := [][]string{{
		formatMetadataValue(result["total_input_tokens"]),
		formatMetadataValue(result["total_output_tokens"]),
		formatMetadataValue(result["total_cache_read_tokens"]),
		formatMetadataValue(result["total_cache_write_tokens"]),
		formatMetadataValue(result["task_count"]),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueRunMessages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueID := ""
	if issueInput, _ := cmd.Flags().GetString("issue"); issueInput != "" {
		issueRef, err := resolveIssueRef(ctx, client, issueInput)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		issueID = issueRef.ID
	}
	taskRef, err := resolveTaskRunID(ctx, client, issueID, args[0])
	if err != nil {
		return fmt.Errorf("resolve task run: %w", err)
	}

	path := "/api/tasks/" + url.PathEscape(taskRef.ID) + "/messages"
	if since, _ := cmd.Flags().GetInt("since"); since > 0 {
		path += fmt.Sprintf("?since=%d", since)
	}

	var messages []map[string]any
	if err := client.GetJSON(ctx, path, &messages); err != nil {
		return fmt.Errorf("list run messages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, messages)
	}

	headers := []string{"SEQ", "TYPE", "TOOL", "CONTENT"}
	rows := make([][]string, 0, len(messages))
	for _, m := range messages {
		content := strVal(m, "content")
		if content == "" {
			content = strVal(m, "output")
		}
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		seq := ""
		if v, ok := m["seq"]; ok {
			seq = fmt.Sprintf("%v", v)
		}
		rows = append(rows, []string{
			seq,
			strVal(m, "type"),
			strVal(m, "tool"),
			content,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Search command
// ---------------------------------------------------------------------------

func runIssueRerun(cmd *cobra.Command, args []string) error {
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

	var task map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/rerun", map[string]any{}, &task); err != nil {
		return fmt.Errorf("rerun issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, task)
	}
	agent := loadActorDisplayLookup(ctx, client).agent(strVal(task, "agent_id"))
	fmt.Fprintf(os.Stdout, "Re-enqueued task %s on agent %s\n", strVal(task, "id"), agent)
	return nil
}

// runIssueCancelTask cancels a single task by ID. It accepts the short ID
// prefix shown by `issue runs` (resolved through resolveTaskRunID), and uses
// /api/tasks/{taskId}/cancel which both updates the DB row to status=cancelled
// and triggers the daemon-side interrupt path (#2107) so an in-flight agent
// stops emitting tool calls promptly instead of running until its own timeout.
func runIssueCancelTask(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueScope := ""
	if issueInput, _ := cmd.Flags().GetString("issue"); issueInput != "" {
		issueRef, err := resolveIssueRef(ctx, client, issueInput)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		issueScope = issueRef.ID
	}
	taskRef, err := resolveTaskRunID(ctx, client, issueScope, args[0])
	if err != nil {
		return fmt.Errorf("resolve task run: %w", err)
	}

	var result map[string]any
	path := "/api/tasks/" + url.PathEscape(taskRef.ID) + "/cancel"
	if err := client.PostJSON(ctx, path, map[string]any{}, &result); err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	status := strVal(result, "status")
	if status == "" {
		status = "cancelled"
	}
	fmt.Fprintf(os.Stdout, "Task %s -> status=%s\n", taskRef.ID, status)
	return nil
}

func runIssueSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	params.Set("q", args[0])
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetBool("include-closed"); v {
		params.Set("include_closed", "true")
	}

	path := "/api/issues/search?" + params.Encode()

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("search issues: %w", err)
	}

	issuesRaw, _ := result["issues"].([]any)
	issuesRaw = filterIssuesUpdatedSince(cmd, issuesRaw)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"KEY", "TITLE", "STATUS", "MATCH"}
	rows := make([][]string, 0, len(issuesRaw))
	for _, raw := range issuesRaw {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		matchInfo := strVal(issue, "match_source")
		if snippet := strVal(issue, "matched_snippet"); snippet != "" {
			if utf8.RuneCountInString(snippet) > 50 {
				runes := []rune(snippet)
				snippet = string(runes[:47]) + "..."
			}
			matchInfo += ": " + snippet
		}
		rows = append(rows, []string{
			strVal(issue, "identifier"),
			strVal(issue, "title"),
			strVal(issue, "status"),
			matchInfo,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Subscriber commands
// ---------------------------------------------------------------------------

func runIssueSubscriberList(cmd *cobra.Command, args []string) error {
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

	var subscribers []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/subscribers", &subscribers); err != nil {
		return fmt.Errorf("list subscribers: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, subscribers)
	}

	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"USER", "REASON", "CREATED"}
	rows := make([][]string, 0, len(subscribers))
	for _, s := range subscribers {
		created := strVal(s, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		rows = append(rows, []string{
			actors.actor(strVal(s, "user_type"), strVal(s, "user_id")),
			strVal(s, "reason"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueSubscriberAdd(cmd *cobra.Command, args []string) error {
	return runIssueSubscriberMutation(cmd, args[0], "subscribe")
}

func runIssueSubscriberRemove(cmd *cobra.Command, args []string) error {
	return runIssueSubscriberMutation(cmd, args[0], "unsubscribe")
}

// runIssueSubscriberMutation shares subscribe/unsubscribe logic — both endpoints
// take the same request body and only differ in the path.
func runIssueSubscriberMutation(cmd *cobra.Command, issueID, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issueID)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{}
	userName, _ := cmd.Flags().GetString("user")
	uType, uID, hasUser, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "user", "user-id", memberOrAgentKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve user: %w", resolveErr)
	}
	if hasUser {
		body["user_type"] = uType
		body["user_id"] = uID
	}

	var result map[string]any
	path := "/api/issues/" + issueRef.ID + "/" + action
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("%s issue: %w", action, err)
	}

	target := "caller"
	if userName != "" {
		target = userName
	} else if hasUser {
		target = loadActorDisplayLookup(ctx, client).actor(uType, uID)
	}
	if action == "subscribe" {
		fmt.Fprintf(os.Stderr, "Subscribed %s to issue %s.\n", target, issueRef.Display)
	} else {
		fmt.Fprintf(os.Stderr, "Unsubscribed %s from issue %s.\n", target, issueRef.Display)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type assigneeMatch struct {
	Type string // "member", "agent", or "squad"
	ID   string // user_id for members, agent id for agents, squad id for squads
	Name string
}

// assigneeKinds is the set of entity types a given flag is allowed to resolve
// to. Issue assignees accept all three (`issueAssigneeKinds`), while
// project lead and issue subscribers are member-or-agent only
// (`memberOrAgentKinds`) — the DB CHECK on `project.lead_type` and the
// `isWorkspaceEntity` switch in the subscriber handler both reject `squad`,
// so resolving to (squad, ...) for those callers would surface as a 500 /
// 403 instead of a clean CLI-side resolution error (MUL-2165 follow-up).
type assigneeKinds struct {
	member, agent, squad bool
}

var (
	issueAssigneeKinds = assigneeKinds{member: true, agent: true, squad: true}
	memberOrAgentKinds = assigneeKinds{member: true, agent: true}
)

var assigneeResolveRetrySleep = func(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}

func getAssigneeJSON(ctx context.Context, client *cli.APIClient, path string, out any) error {
	delays := []time.Duration{100 * time.Millisecond, 250 * time.Millisecond}
	var err error
	for attempt := 0; attempt <= len(delays); attempt++ {
		err = client.GetJSON(ctx, path, out)
		if err == nil || !isRetryableAssigneeResolveError(err) || attempt == len(delays) {
			return err
		}
		if assigneeResolveRetrySleep(ctx, delays[attempt]) {
			return ctx.Err()
		}
	}
	return err
}

func isRetryableAssigneeResolveError(err error) bool {
	var netErr *cli.NetworkError
	return errors.As(err, &netErr)
}

func (k assigneeKinds) describe() string {
	parts := make([]string, 0, 3)
	if k.member {
		parts = append(parts, "member")
	}
	if k.agent {
		parts = append(parts, "agent")
	}
	if k.squad {
		parts = append(parts, "squad")
	}
	switch len(parts) {
	case 0:
		return "<none>"
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " or " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", or " + parts[len(parts)-1]
	}
}

func resolveAssignee(ctx context.Context, client *cli.APIClient, name string, kinds assigneeKinds) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	input := normalizeAssigneeLookupInput(name)
	if input == "" {
		return "", "", fmt.Errorf("no %s found matching %q", kinds.describe(), name)
	}
	inputLower := strings.ToLower(input)

	// Matches are collected into three priority buckets. Higher-priority buckets
	// short-circuit lower-priority matching so that, e.g., an exact name match
	// always wins over a substring collision with another candidate.
	//   1. idMatches        — full UUID or 8-char ShortID (as shown by `truncateID`).
	//   2. exactMatches     — case-insensitive full name equality.
	//   3. substringMatches — preserves the existing partial-name UX.
	var idMatches, exactMatches, substringMatches []assigneeMatch
	var errs []error
	var fetchAttempts int

	classify := func(entityType, id, displayName string) {
		match := assigneeMatch{Type: entityType, ID: id, Name: displayName}
		if id != "" && (strings.EqualFold(id, input) || strings.EqualFold(truncateID(id), input)) {
			idMatches = append(idMatches, match)
			return
		}
		if strings.EqualFold(displayName, input) {
			exactMatches = append(exactMatches, match)
			return
		}
		if strings.Contains(strings.ToLower(displayName), inputLower) {
			substringMatches = append(substringMatches, match)
		}
	}

	// Search members.
	if kinds.member {
		fetchAttempts++
		var members []map[string]any
		if err := getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err != nil {
			errs = append(errs, fmt.Errorf("fetch members: %w", err))
		} else {
			for _, m := range members {
				classify("member", strVal(m, "user_id"), strVal(m, "name"))
			}
		}
	}

	// Search agents.
	if kinds.agent {
		fetchAttempts++
		var agents []map[string]any
		agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
		if err := getAssigneeJSON(ctx, client, agentPath, &agents); err != nil {
			errs = append(errs, fmt.Errorf("fetch agents: %w", err))
		} else {
			for _, a := range agents {
				classify("agent", strVal(a, "id"), strVal(a, "name"))
			}
		}
	}

	// Search squads. The platform allows issues to be assigned to a squad
	// (the leader agent then coordinates delegation), so squad names must
	// resolve here too for issue-assignee callers — otherwise a user saying
	// "assign to <SquadName>" silently falls through and the autopilot
	// prompt emits "Unrecognized assignee: <SquadName>" (MUL-2165). Callers
	// whose target schema is member-or-agent only (project lead, subscriber)
	// must opt out via `kinds.squad = false`.
	if kinds.squad {
		fetchAttempts++
		var squads []map[string]any
		if err := getAssigneeJSON(ctx, client, "/api/squads", &squads); err != nil {
			errs = append(errs, fmt.Errorf("fetch squads: %w", err))
		} else {
			for _, s := range squads {
				if strVal(s, "archived_at") != "" {
					continue
				}
				classify("squad", strVal(s, "id"), strVal(s, "name"))
			}
		}
	}

	// If every fetch failed, report the errors instead of a misleading "not found".
	if fetchAttempts > 0 && len(errs) == fetchAttempts {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return "", "", fmt.Errorf("failed to resolve assignee: %s", strings.Join(msgs, "; "))
	}

	for _, bucket := range [][]assigneeMatch{idMatches, exactMatches, substringMatches} {
		switch len(bucket) {
		case 0:
			continue
		case 1:
			return bucket[0].Type, bucket[0].ID, nil
		default:
			return "", "", ambiguousAssigneeError(input, bucket)
		}
	}
	return "", "", fmt.Errorf("no %s found matching %q", kinds.describe(), input)
}

func normalizeAssigneeLookupInput(raw string) string {
	input := strings.TrimSpace(raw)
	if m := util.MentionRe.FindStringSubmatch(input); len(m) == 4 && m[0] == input {
		switch m[2] {
		case "member", "agent", "squad":
			return m[3]
		}
	}
	input = strings.TrimLeftFunc(input, func(r rune) bool {
		return r == '@' || r == '＠'
	})
	return strings.TrimSpace(input)
}

func ambiguousAssigneeError(input string, matches []assigneeMatch) error {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("  %s %q (%s)", m.Type, m.Name, truncateID(m.ID)))
	}
	return fmt.Errorf("ambiguous assignee %q; matches:\n%s", input, strings.Join(parts, "\n"))
}

// resolveAssigneeByID strictly resolves a canonical UUID to (assignee_type,
// assignee_id) by looking it up against the workspace's members, agents, and
// (when allowed) squads. It is the deterministic counterpart to
// resolveAssignee: callers that already hold a UUID (e.g. agents reading IDs
// from `multica workspace member list --output json`) should use this instead of
// round-tripping through name matching, which can be ambiguous in workspaces
// with overlapping names.
func resolveAssigneeByID(ctx context.Context, client *cli.APIClient, id string, kinds assigneeKinds) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	input := strings.TrimSpace(id)
	if !uuidRegexp.MatchString(input) {
		return "", "", fmt.Errorf("expected a canonical UUID, got %q", id)
	}

	var members []map[string]any
	var memberErr error
	if kinds.member {
		memberErr = getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members)
	}

	var agents []map[string]any
	var agentErr error
	if kinds.agent {
		agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
		agentErr = getAssigneeJSON(ctx, client, agentPath, &agents)
	}

	var squads []map[string]any
	var squadErr error
	if kinds.squad {
		squadErr = getAssigneeJSON(ctx, client, "/api/squads", &squads)
	}

	allFailed := true
	hasFetch := false
	for _, pair := range []struct {
		enabled bool
		err     error
	}{{kinds.member, memberErr}, {kinds.agent, agentErr}, {kinds.squad, squadErr}} {
		if !pair.enabled {
			continue
		}
		hasFetch = true
		if pair.err == nil {
			allFailed = false
		}
	}
	if hasFetch && allFailed {
		return "", "", fmt.Errorf("failed to resolve assignee: %v; %v; %v", memberErr, agentErr, squadErr)
	}

	for _, m := range members {
		if strings.EqualFold(strVal(m, "user_id"), input) {
			return "member", strVal(m, "user_id"), nil
		}
	}
	for _, a := range agents {
		if strings.EqualFold(strVal(a, "id"), input) {
			return "agent", strVal(a, "id"), nil
		}
	}
	for _, s := range squads {
		if strings.EqualFold(strVal(s, "id"), input) {
			return "squad", strVal(s, "id"), nil
		}
	}

	return "", "", fmt.Errorf("no %s found with ID %q", kinds.describe(), input)
}

// pickAssigneeFromFlags reads a (name-flag, id-flag) pair off cmd and resolves
// it to (assignee_type, assignee_id), restricted to the entity types in
// kinds. The third return reports whether either flag was *explicitly set*;
// callers use it to decide whether to write `assignee_*` into the request
// body. The two flags are mutually exclusive — passing both is rejected
// up-front so a script that accidentally sets both never silently applies one
// over the other.
//
// Presence is detected via Flags().Changed (not value-emptiness): a script
// that interpolates an empty env var (`--assignee-id "$MAYBE_UUID"`) must
// fail loudly through resolveAssignee/resolveAssigneeByID rather than silently
// degrade to "no filter / unassigned / subscribe caller", which would defeat
// the strict-UUID guarantee the new flags exist for.
func pickAssigneeFromFlags(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, nameFlag, idFlag string, kinds assigneeKinds) (string, string, bool, error) {
	nameSet := cmd.Flags().Changed(nameFlag)
	idSet := cmd.Flags().Changed(idFlag)
	if nameSet && idSet {
		return "", "", false, fmt.Errorf("--%s and --%s are mutually exclusive", nameFlag, idFlag)
	}
	if idSet {
		idVal, _ := cmd.Flags().GetString(idFlag)
		t, i, err := resolveAssigneeByID(ctx, client, idVal, kinds)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	if nameSet {
		name, _ := cmd.Flags().GetString(nameFlag)
		t, i, err := resolveAssignee(ctx, client, name, kinds)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	return "", "", false, nil
}

func formatAssignee(issue map[string]any, actors actorDisplayLookup) string {
	aType := strVal(issue, "assignee_type")
	aID := strVal(issue, "assignee_id")
	if aType == "" || aID == "" {
		return ""
	}
	return actors.actor(aType, aID)
}

func truncateID(id string) string {
	if utf8.RuneCountInString(id) > 8 {
		runes := []rune(id)
		return string(runes[:8])
	}
	return id
}

// compactComments strips response fields that carry no information for a
// reader: the issue_id echoed from the request path, per-run bookkeeping
// (source_task_id), updated_at when identical to created_at, null-valued
// keys, and empty arrays. Everything else — content, identity, thread
// summary fields — passes through untouched. Opt-in via --compact and
// applied to JSON output only: the two bounded reads the agent workflow
// prescribes are dominated by exactly this metadata (56% of a
// --roots-only --summary scan, 21% of a --thread --tail read, measured on
// a production issue), and it compounds through the prompt-cache prefix
// (#5999 follow-up, MUL-5442).
func compactComments(comments []map[string]any) {
	for _, c := range comments {
		delete(c, "issue_id")
		delete(c, "source_task_id")
		if ua, ok := c["updated_at"]; ok && ua == c["created_at"] {
			delete(c, "updated_at")
		}
		for k, v := range c {
			switch vv := v.(type) {
			case nil:
				delete(c, k)
			case []any:
				if len(vv) == 0 {
					delete(c, k)
				}
			}
		}
	}
}

// filterIssuesUpdatedSince keeps the rows that moved inside the window.
//
// The filter is client-side because the server has no such parameter, and it
// is honest about what that means: it narrows the PAGE the server returned,
// not the whole workspace. Paired with `--sort updated_at --direction desc`
// the newest rows are the ones on that page, which is exactly the question
// "what moved today" asks — and it stays one bounded request rather than a
// walk over every issue in the workspace.
func filterIssuesUpdatedSince(cmd *cobra.Command, rows []any) []any {
	window, _ := cmd.Flags().GetDuration("updated-since")
	if window <= 0 {
		return rows
	}
	cutoff := time.Now().Add(-window)
	kept := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		updated, _ := row["updated_at"].(string)
		at, err := time.Parse(time.RFC3339, updated)
		// A row whose timestamp will not parse is kept rather than dropped:
		// silently losing an issue is worse than showing one extra.
		if err != nil || !at.Before(cutoff) {
			kept = append(kept, raw)
		}
	}
	return kept
}
