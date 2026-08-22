package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica autopilot pause / resume — stopping one without deleting it.
//
// Until this existed the only way to stop a scheduled autopilot was to delete
// its trigger, which throws away the schedule itself: the cron expression, its
// timezone, its label. Turning the automation back on then means rebuilding
// all of that from memory, and the reason it was stopped lives nowhere at all.
//
// Pausing stops the SCHEDULE, not the autopilot. Timed and webhook triggers
// stop firing — both already gate on `status = 'active'` — while `autopilot
// trigger` still runs it by hand. That combination is the point: the usual
// reason to pause one is that its prompt needs work, and a prompt cannot be
// fixed without running it.

var autopilotPauseCmd = &cobra.Command{
	Use:   "pause <autopilot-id>",
	Short: "Stop an autopilot's triggers from firing, keeping the schedule intact",
	Long: `Stop an autopilot's triggers from firing.

Timed and webhook triggers stop; a manual 'autopilot trigger' still runs it, so
a paused autopilot can be tried out while whatever prompted the pause is fixed.

The schedule itself survives — resuming needs no rebuilding. --reason is worth
passing: the pause outlives the session that made it, and 'list' shows it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason := strings.TrimSpace(mustString(cmd, "reason"))
		return setAutopilotPauseState(cmd, args[0], "paused", reason)
	},
}

var autopilotResumeCmd = &cobra.Command{
	Use:   "resume <autopilot-id>",
	Short: "Let a paused autopilot's triggers fire again",
	Long: `Let a paused autopilot's triggers fire again.

Clears the pause reason. The next scheduled slot fires normally; a slot that
passed while it was paused is not made up.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setAutopilotPauseState(cmd, args[0], "active", "")
	},
}

func init() {
	autopilotCmd.AddCommand(autopilotPauseCmd)
	autopilotCmd.AddCommand(autopilotResumeCmd)
	autopilotPauseCmd.Flags().String("reason", "", "Why it is paused, in one line — shown by 'autopilot list'")
	autopilotPauseCmd.Flags().String("output", "table", "Output format: table or json")
	autopilotResumeCmd.Flags().String("output", "table", "Output format: table or json")
}

func setAutopilotPauseState(cmd *cobra.Command, ref, status, reason string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, ref)
	if err != nil {
		return err
	}

	// The reason travels with the status in one request. Two writes would let
	// an autopilot sit paused with the reason from a previous pause, which
	// reads as an explanation and is not one.
	body := map[string]any{"status": status}
	if reason != "" {
		body["pause_reason"] = reason
	}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/autopilots/"+autopilotRef.ID, body, &result); err != nil {
		return fmt.Errorf("update autopilot: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	title := strVal(result, "title")
	if title == "" {
		title = autopilotRef.ID
	}
	if status == "paused" {
		fmt.Fprintf(os.Stdout, "Paused %s. Timed and webhook triggers will not fire; `autopilot trigger` still runs it by hand.\n", title)
		if reason != "" {
			fmt.Fprintf(os.Stdout, "Reason: %s\n", reason)
		}
		return nil
	}
	fmt.Fprintf(os.Stdout, "Resumed %s. Its triggers fire again from the next slot; a slot missed while paused is not made up.\n", title)
	return nil
}
