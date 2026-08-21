package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueInterruptCmd = &cobra.Command{
	Use:   "interrupt <issue-id>",
	Short: "Interrupt the active run and interject: cancel, then wake the agent with your comment",
	Long: "One action for the moment an in-flight agent is going the wrong way (COC-283): " +
		"cancels the issue's active task (daemon-side interrupt stops tool calls promptly), " +
		"waits for it to reach a terminal state, then posts the comment with an explicit @mention " +
		"of the assignee agent — the only manual trigger — so a fresh run starts, resumes the same " +
		"CLI session and workdir, and reads your redirection. Without this, changing course means " +
		"waiting the run out or hand-rolling cancel + comment.",
	Args: exactArgs(1),
	RunE: runIssueInterrupt,
}

const (
	interruptPollEvery = 2 * time.Second
	interruptWaitFor   = 3 * time.Minute
)

func runIssueInterrupt(cmd *cobra.Command, args []string) error {
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

	var active map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/active-task", &active); err != nil {
		return fmt.Errorf("load active task: %w", err)
	}
	taskID := strVal(active, "id")
	status := strVal(active, "status")
	if taskID == "" || terminalRunStatus(status) {
		return fmt.Errorf("issue %s has no active task to interrupt (active status: %q)", issueRef.Display, status)
	}

	if v, _ := cmd.Flags().GetBool("content-stdin"); v {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		if _, err := cmd.Flags().GetString("comment"); err == nil {
			_ = cmd.Flags().Set("comment", string(raw))
		}
	}
	comment, _ := cmd.Flags().GetString("comment")
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return fmt.Errorf("nothing to interject: pass --comment or --content-stdin")
	}

	agentName := ""
	actors := loadActorDisplayLookup(ctx, client)
	if agentID := strVal(active, "agent_id"); agentID != "" {
		agentName = actors.agent(agentID)
	}

	// Cancel first: the daemon interrupts the in-flight process, and the
	// terminal transition must land before the waking comment enqueues a
	// successor — otherwise the pending-task dedup folds or defers it.
	if err := client.PostJSON(ctx, "/api/tasks/"+url.PathEscape(taskID)+"/cancel", map[string]any{}, nil); err != nil {
		return fmt.Errorf("cancel task %s: %w", taskID, err)
	}
	fmt.Fprintf(os.Stdout, "Cancelled %s (was %s); waiting for it to settle…\n", shortTaskID(taskID), status)

	if err := waitForRunTerminal(ctx, client, issueRef.ID, taskID); err != nil {
		return err
	}

	// The explicit @mention is the only comment path that wakes an agent
	// (implicit routing is off); the successor run resumes the prior session.
	if agentName != "" && !strings.Contains(comment, "@") {
		comment = "@" + agentName + " " + comment
	}
	var posted map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/comments", map[string]any{
		"content": comment,
	}, &posted); err != nil {
		return fmt.Errorf("post interjection: %w (task was cancelled — rerun with `issue rerun` or re-post the comment)", err)
	}
	fmt.Fprintf(os.Stdout, "Interjected on %s (comment %s). The agent resumes the same session with your redirection.\n",
		issueRef.Display, strVal(posted, "id"))
	return nil
}

// waitForRunTerminal polls the issue's run list until the given task reaches a
// terminal status or the wait budget is spent. Best-effort on timeout: the
// comment step still runs, since the cancel already succeeded.
func waitForRunTerminal(ctx context.Context, client *cli.APIClient, issueID, taskID string) error {
	deadline := time.Now().Add(interruptWaitFor)
	for time.Now().Before(deadline) {
		var runs []map[string]any
		if err := client.GetJSON(ctx, "/api/issues/"+issueID+"/task-runs", &runs); err == nil {
			for _, r := range runs {
				if strVal(r, "id") != taskID {
					continue
				}
				if terminalRunStatus(strVal(r, "status")) {
					return nil
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interruptPollEvery):
		}
	}
	return fmt.Errorf("task %s did not settle within %s; posting the interjection anyway", shortTaskID(taskID), interruptWaitFor)
}

func terminalRunStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

func shortTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
