package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica task brief — read back the brief a run was actually handed.
//
// The brief is rendered fresh from current data every time, so re-rendering it
// later answers "what would this issue produce now" rather than "what did that
// run see". Once a spec, a decision or the workspace rules have moved, those
// are different documents — and the second question is the one asked when a
// run went wrong.
//
// The snapshot was recorded before anything could read it. A record nobody can
// retrieve is worse than none: it looks like the answer is available right up
// to the moment somebody needs it.

var taskCmd = &cobra.Command{
	Use:     "task",
	Short:   "Work with individual agent runs",
	GroupID: groupCore,
}

var taskBriefCmd = &cobra.Command{
	Use:   "brief <task-id>",
	Short: "Print the brief this run was handed",
	Long: `Prints the runtime brief a dispatched run actually received.

Re-rendering it now would answer a different question: the brief is built from
current data, so a spec or decision that has moved since produces something the
run never saw. This is the copy taken when it was handed over.

Runs that predate the snapshot, and runs whose report never landed, have none.
The command says so rather than printing nothing.

  multica issue runs <issue-id> --output json     # find the task id
  multica task brief <task-id>`,
	Args: exactArgs(1),
	RunE: runTaskBrief,
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskBriefCmd)
	taskBriefCmd.Flags().String("output", "text", "Output format: text or json")
}

func runTaskBrief(cmd *cobra.Command, args []string) error {
	taskID := strings.TrimSpace(args[0])
	if !uuidRegexp.MatchString(taskID) {
		return fmt.Errorf(
			"task id %q is not a full UUID; take it from `multica issue runs <issue-id> --output json`", args[0])
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		TaskID   string `json:"task_id"`
		Recorded bool   `json:"recorded"`
		Brief    string `json:"brief"`
	}
	if err := client.GetJSON(ctx, "/api/tasks/"+taskID+"/brief", &resp); err != nil {
		return fmt.Errorf("read brief: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if !resp.Recorded {
		// Naming both causes matters: one means the feature is younger than the
		// run, the other means something went wrong at dispatch. Guessing
		// between them would send the reader looking in the wrong place.
		fmt.Fprintf(os.Stderr,
			"No brief recorded for this run — it either predates the snapshot or its report never landed.\n")
		return nil
	}
	fmt.Print(resp.Brief)
	if !strings.HasSuffix(resp.Brief, "\n") {
		fmt.Println()
	}
	return nil
}
