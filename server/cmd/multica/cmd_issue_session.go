package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// resolveIssueIndexSession picks the session id recorded on a new card.
//
// An explicit --session always wins; otherwise the id comes from the agent
// runtime this command is running inside, read exactly the way `worktree
// session --auto` reads it. Finding neither is not an error — the card is still
// filed with the field left empty, which is a real answer (filed from outside
// any agent session) rather than a missing one.
func resolveIssueIndexSession(cmd *cobra.Command) string {
	if explicit, _ := cmd.Flags().GetString("session"); strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	for _, candidate := range sessionEnv {
		if id := strings.TrimSpace(os.Getenv(candidate.env)); id != "" {
			return id
		}
	}
	return ""
}
