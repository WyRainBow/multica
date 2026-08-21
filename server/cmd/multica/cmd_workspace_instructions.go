package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica workspace instructions pull — copy the workspace's shared
// instructions into a local agent config file.
//
// The workspace's instructions reach agents the daemon dispatches, because it
// injects them into each task's working directory. They do NOT reach a session
// someone starts by hand in a terminal: that reads `~/.claude/CLAUDE.md` and
// the repository's own config, neither of which the daemon touches.
//
// So the same rules were being maintained twice — once in the workspace, once
// per machine — and nothing could tell when the two had drifted. Deleting the
// local copy would have fixed the duplication by breaking the surface people
// actually work on.
//
// This makes the local file a CACHE instead of a second source. The block is
// replaced wholesale on every pull and everything outside it is left byte for
// byte, so the file can carry personal rules above and below without this
// command having an opinion about them.
//
// It never runs on its own. Rewriting somebody's agent config as a side effect
// of an unrelated command is exactly the kind of surprise a tool should not
// spring.

const (
	// HTML comments so the markers are inert in every Markdown renderer and
	// harmless when the file is fed to an agent as instructions. Distinct from
	// the daemon's runtime markers on purpose: a task workdir can carry both,
	// and a shared marker would let one writer eat the other's block.
	instructionsMarkerBegin = "<!-- BEGIN MULTICA-INSTRUCTIONS (pulled; edit in the workspace, not here) -->"
	instructionsMarkerEnd   = "<!-- END MULTICA-INSTRUCTIONS -->"
)

var workspaceInstructionsCmd = &cobra.Command{
	Use:   "instructions",
	Short: "Work with the workspace's shared agent instructions",
	Long: `The instructions every agent in this workspace follows on every task.

Read or edit them with 'workspace get' / 'workspace update --context'; this
group is about getting them to a runtime the daemon does not reach.`,
}

var workspaceInstructionsPullCmd = &cobra.Command{
	Use:   "pull [workspace-id|slug|prefix]",
	Short: "Copy the workspace instructions into a local agent config file",
	Long: `Writes the workspace's instructions into a marked block in a local file.

The daemon injects these instructions into every task it dispatches, but a
session started by hand in a terminal never sees them — it reads the local
agent config instead. Pulling puts one copy where that session will find it.

The marked block is replaced in full each time; everything outside it is kept
exactly as it was, so personal rules can live in the same file. Running it
twice with no change upstream leaves the file byte-identical.

The local copy is a cache, not a second source. Edit the instructions in the
workspace and pull again; edits made inside the block are lost on the next
pull.

  multica workspace instructions pull --file ~/.claude/CLAUDE.md
  multica workspace instructions pull --file ~/.codex/AGENTS.md`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceInstructionsPull,
}

func init() {
	workspaceCmd.AddCommand(workspaceInstructionsCmd)
	workspaceInstructionsCmd.AddCommand(workspaceInstructionsPullCmd)

	workspaceInstructionsPullCmd.Flags().String("file", "",
		"Local agent config file to write into, e.g. ~/.claude/CLAUDE.md (required)")
	workspaceInstructionsPullCmd.Flags().Bool("dry-run", false,
		"Report what would change without writing")
	workspaceInstructionsPullCmd.Flags().Bool("force", false,
		"Replace a block that was pulled from a different workspace")

	workspaceInstructionsCmd.AddCommand(workspaceInstructionsStatusCmd)
	workspaceInstructionsStatusCmd.Flags().String("file", "",
		"Local agent config file to check, e.g. ~/.claude/CLAUDE.md (required)")
}

func runWorkspaceInstructionsPull(cmd *cobra.Command, args []string) error {
	target := strings.TrimSpace(mustString(cmd, "file"))
	if target == "" {
		return fmt.Errorf("--file is required: name the agent config to write into, e.g. ~/.claude/CLAUDE.md")
	}
	expanded, err := expandUserPath(target)
	if err != nil {
		return err
	}

	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var ws struct {
		Slug    string `json:"slug"`
		Context string `json:"context"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}
	instructions := strings.TrimSpace(ws.Context)
	if instructions == "" {
		return fmt.Errorf(
			"this workspace has no instructions to pull; set them with " +
				"`multica workspace update --context-stdin` first")
	}

	existing, err := readFileAllowingMissing(expanded)
	if err != nil {
		return err
	}

	// A file already holding another workspace's rules is not a file to
	// silently overwrite: the two sets are both real, and picking one without
	// being asked swaps the rules an agent obeys.
	if prevSlug, _ := blockProvenance(existing); prevSlug != "" && ws.Slug != "" && prevSlug != ws.Slug {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			return fmt.Errorf(
				"%s already holds instructions pulled from workspace %q, and this pull is from %q. "+
					"Pass --force to replace them, or pull into a different file",
				target, prevSlug, ws.Slug)
		}
		fmt.Fprintf(os.Stderr, "Replacing instructions previously pulled from %q.\n", prevSlug)
	}

	updated := applyInstructionsBlock(existing, renderInstructionsBlock(ws.Slug, instructions))

	if updated == existing {
		fmt.Fprintf(os.Stderr, "%s is already current; nothing written.\n", target)
		return nil
	}
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		fmt.Fprintf(os.Stderr, "%s would change (%d bytes outside the block preserved).\n",
			target, len(existing)-len(extractInstructionsBlock(existing)))
		return nil
	}

	if dir := filepath.Dir(expanded); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := writeFileAtomic(expanded, []byte(updated)); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Fprintf(os.Stderr, "Pulled %s instructions (%d chars, %s) into %s.\n",
		ws.Slug, len([]rune(instructions)), instructionsFingerprint(instructions), target)
	return nil
}

var workspaceInstructionsStatusCmd = &cobra.Command{
	Use:   "status [workspace-id|slug|prefix]",
	Short: "Report whether a local copy is still current",
	Long: `Compares the block in a local agent config against the workspace.

A pull only happens when someone runs it, so a local copy goes stale quietly.
This answers the question the file itself cannot: is what a manual session
reads still what the workspace says?

Exits non-zero when the copy is stale or missing, so it can gate a shell
prompt or a session-start hook.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceInstructionsStatus,
}

func runWorkspaceInstructionsStatus(cmd *cobra.Command, args []string) error {
	target := strings.TrimSpace(mustString(cmd, "file"))
	if target == "" {
		return fmt.Errorf("--file is required: name the agent config to check, e.g. ~/.claude/CLAUDE.md")
	}
	expanded, err := expandUserPath(target)
	if err != nil {
		return err
	}

	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var ws struct {
		Slug    string `json:"slug"`
		Context string `json:"context"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}
	live := instructionsFingerprint(ws.Context)

	existing, err := readFileAllowingMissing(expanded)
	if err != nil {
		return err
	}
	localSlug, localPrint := blockProvenance(existing)

	switch {
	case extractInstructionsBlock(existing) == "":
		return fmt.Errorf("%s carries no pulled instructions; run `workspace instructions pull --file %s`", target, target)
	case localPrint == "":
		return fmt.Errorf("%s has a block from before provenance was recorded; pull again to make it checkable", target)
	case localSlug != "" && ws.Slug != "" && localSlug != ws.Slug:
		return fmt.Errorf("%s holds instructions from workspace %q, not %q", target, localSlug, ws.Slug)
	case localPrint != live:
		return fmt.Errorf("%s is stale: has %s, workspace %s has %s. Run `workspace instructions pull --file %s`",
			target, localPrint, ws.Slug, live, target)
	}
	fmt.Fprintf(os.Stderr, "%s is current with workspace %s (%s).\n", target, ws.Slug, live)
	return nil
}

// writeFileAtomic writes via a same-directory temp file plus a rename, so a
// reader — a shell starting an agent session, say — sees either the old file
// or the complete new one, never a half-written config it would obey.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".multica-instructions-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// renderInstructionsBlock wraps the instructions in the markers, with a line
// saying where to edit them. A reader who finds these rules in their own
// config file would otherwise reasonably edit them there.
//
// The provenance line names the workspace and fingerprints the content. The
// name is there because a machine may pull from more than one workspace and a
// block that does not say whose rules it holds cannot be checked against the
// right source. The fingerprint is there so staleness is answerable without
// re-fetching and re-diffing the whole body.
func renderInstructionsBlock(slug, instructions string) string {
	return instructionsMarkerBegin + "\n" +
		fmt.Sprintf("<!-- workspace: %s · sha256: %s -->\n", slug, instructionsFingerprint(instructions)) +
		"<!-- Pulled from the Multica workspace. Edits inside this block are lost on the next pull. -->\n\n" +
		instructions + "\n\n" +
		instructionsMarkerEnd
}

// instructionsFingerprint identifies a body of instructions. Short because it
// is read by humans scanning a config file, and collisions here cost a
// needless pull rather than a wrong one.
func instructionsFingerprint(instructions string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(instructions)))
	return hex.EncodeToString(sum[:])[:12]
}

// blockProvenance reads back what a pulled block says about itself: which
// workspace it came from and the fingerprint of what was pulled. Both empty
// when the file has no block, or carries one written before provenance was
// recorded — an older block is not an error, just unanswerable.
func blockProvenance(existing string) (slug, fingerprint string) {
	block := extractInstructionsBlock(existing)
	if block == "" {
		return "", ""
	}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<!-- workspace:") {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- workspace:"), "-->")
		parts := strings.Split(body, "·")
		if len(parts) > 0 {
			slug = strings.TrimSpace(parts[0])
		}
		for _, part := range parts[1:] {
			part = strings.TrimSpace(part)
			if after, ok := strings.CutPrefix(part, "sha256:"); ok {
				fingerprint = strings.TrimSpace(after)
			}
		}
		return slug, fingerprint
	}
	return "", ""
}

// applyInstructionsBlock replaces the marked block in existing, or appends one.
//
// Content outside the markers is preserved byte for byte: the file belongs to
// its owner, and this command owns exactly one region of it.
func applyInstructionsBlock(existing, block string) string {
	start := strings.Index(existing, instructionsMarkerBegin)
	if start < 0 {
		if strings.TrimSpace(existing) == "" {
			return block + "\n"
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + block + "\n"
	}
	end := strings.Index(existing[start:], instructionsMarkerEnd)
	if end < 0 {
		// A begin marker with no end: the file was truncated or hand-edited.
		// Everything from the marker on is unreadable as user content, so
		// replacing it is the only recoverable move — say so rather than
		// doing it quietly.
		fmt.Fprintf(os.Stderr,
			"Note: the existing block had no end marker; replacing from the begin marker onwards.\n")
		return existing[:start] + block + "\n"
	}
	after := existing[start+end+len(instructionsMarkerEnd):]
	return existing[:start] + block + after
}

// extractInstructionsBlock returns the current marked block, or "" if absent.
func extractInstructionsBlock(existing string) string {
	start := strings.Index(existing, instructionsMarkerBegin)
	if start < 0 {
		return ""
	}
	end := strings.Index(existing[start:], instructionsMarkerEnd)
	if end < 0 {
		return existing[start:]
	}
	return existing[start : start+end+len(instructionsMarkerEnd)]
}

func readFileAllowingMissing(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(raw), nil
}

// expandUserPath resolves a leading ~ so the flag can be written the way it is
// spoken. Only a leading ~/ is expanded; a literal tilde elsewhere in a path is
// a valid filename character and left alone.
func expandUserPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
