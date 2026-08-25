package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// resolveIssueIndexSession picks the session id recorded on a new card.
//
// An explicit --session always wins; otherwise the id comes from the agent
// runtime this command is running inside. Finding none is not an error — the
// card is filed with the field empty, which is a real answer (filed from
// outside any agent session) rather than a missing one.
func resolveIssueIndexSession(cmd *cobra.Command) string {
	if explicit, _ := cmd.Flags().GetString("session"); strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	return detectAgentSession()
}

// detectAgentSession finds the session this command is running inside.
//
// Environment first, because a runtime that tells its children which session
// they are in is stating a fact. zcode does not: it sets no session variable at
// all (measured, not assumed), so every card it filed recorded no session. For
// that one runtime the id is recovered from the rollout log it is writing right
// now — see zcodeSessionFromRollout for why that inference is bounded.
func detectAgentSession() string {
	for _, candidate := range sessionEnv {
		if id := strings.TrimSpace(os.Getenv(candidate.env)); id != "" {
			return id
		}
	}
	return zcodeSessionFromRollout("")
}

// zcodeRolloutSessionRE pulls the session id out of a rollout file name:
// "model-io-sess_d8ad4170-fed5-46a5-ba47-ce3b3284cb42.jsonl".
var zcodeRolloutSessionRE = regexp.MustCompile(`^model-io-(sess_[0-9a-fA-F-]{36})\.jsonl$`)

// zcodeSessionFromRollout reads the most recently written rollout file's name.
//
// This is an inference and is deliberately the last resort. zcode appends to
// `~/.zcode/cli/rollout/model-io-sess_<uuid>.jsonl` as it runs, so the newest
// mtime is the session doing something right now — which, when this command is
// the thing it is doing, is this session. It is wrong in exactly one case: two
// zcode sessions running concurrently, where the other one wrote last.
//
// That is worth it here and would not be elsewhere. The value is a provenance
// note on a card, and the alternative on this runtime is not a better answer
// but no answer at all. Nothing downstream acts on it, so a rare wrong id
// misattributes a card rather than sending work to the wrong place.
//
// root is for tests; empty means the real location under the user's home.
func zcodeSessionFromRollout(root string) string {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".zcode", "cli", "rollout")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}

	type rollout struct {
		session string
		modTime int64
	}
	found := make([]rollout, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := zcodeRolloutSessionRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		found = append(found, rollout{session: m[1], modTime: info.ModTime().UnixNano()})
	}
	if len(found) == 0 {
		return ""
	}
	// Newest first. Ties break on the id so the answer is stable rather than
	// dependent on directory order, which is not guaranteed.
	sort.Slice(found, func(i, j int) bool {
		if found[i].modTime != found[j].modTime {
			return found[i].modTime > found[j].modTime
		}
		return found[i].session > found[j].session
	})
	return found[0].session
}
