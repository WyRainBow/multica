package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica workspace skills pull — mirror the workspace's skills into a local
// skills directory.
//
// A workspace skill reaches an agent the daemon dispatches: it is materialised
// into the task's own skills directory. It never reaches a session started by
// hand, which reads ~/.claude/skills — a directory the daemon does not touch.
//
// So moving a rule out of a personal config file "into a skill" removes it
// from the surface a person works on, while leaving it in place for dispatched
// runs. One agent then obeys rules the other has never seen. This is the same
// gap the instructions pull closed, on the other half of the assets.
//
// The mirror is one-way and marked. Each pulled skill carries a sidecar naming
// where it came from and fingerprinting what was written, so the next pull can
// tell its own output from a skill the person wrote themselves — and refuse to
// overwrite the latter. The daemon can prefer workspace skills freely because
// it writes into a throwaway per-task directory; this writes into somebody's
// permanent one, where "workspace wins" would mean "your work is gone".

const pulledSkillSidecar = ".multica-pulled.json"

type pulledSkillMarker struct {
	Workspace   string `json:"workspace"`
	SkillID     string `json:"skill_id"`
	Fingerprint string `json:"fingerprint"`
	// Source separates a workspace skill from a platform built-in. A built-in
	// ships with the server binary, so its local copy goes stale when the
	// server moves and nothing local can tell — naming the source is what lets
	// status say so instead of reporting it current forever.
	Source string `json:"source,omitempty"`
}

var workspaceSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Work with the workspace's shared agent skills",
	Long: `The skills bound to agents in this workspace.

Create and edit them with 'multica skill'; this group is about getting them to
a runtime the daemon does not reach.`,
}

var workspaceSkillsPullCmd = &cobra.Command{
	Use:   "pull [workspace-id|slug|prefix]",
	Short: "Mirror the workspace's skills into a local skills directory",
	Long: `Writes each workspace skill into <dir>/<name>/SKILL.md.

The daemon materialises these skills for every task it dispatches, but a
session started by hand reads its own skills directory and never sees them.
Pulling puts a copy where that session will find it.

Each pulled skill carries a marker naming its source. A directory without that
marker is treated as yours and is never overwritten without --force, so a local
skill that happens to share a name is safe.

The mirror is one-way: edit skills in the workspace, then pull again.

  multica workspace skills pull --dir ~/.claude/skills
  multica workspace skills pull --dir ~/.codex/skills`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runWorkspaceSkills(cmd, args, false) },
}

var workspaceSkillsStatusCmd = &cobra.Command{
	Use:   "status [workspace-id|slug|prefix]",
	Short: "Report which local copies are missing or stale",
	Long: `Compares the mirrored skills in a local directory against the workspace.

Exits non-zero when anything is missing or out of date, so it can gate a shell
prompt or a session-start hook.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runWorkspaceSkills(cmd, args, true) },
}

func init() {
	workspaceCmd.AddCommand(workspaceSkillsCmd)
	workspaceSkillsCmd.AddCommand(workspaceSkillsPullCmd)
	workspaceSkillsCmd.AddCommand(workspaceSkillsStatusCmd)

	workspaceSkillsPullCmd.Flags().String("dir", "", "Local skills directory to mirror into, e.g. ~/.claude/skills (required)")
	workspaceSkillsPullCmd.Flags().Bool("force", false, "Overwrite a directory that this command did not create")
	workspaceSkillsPullCmd.Flags().Bool("include-builtin", false,
		"Also mirror the platform's built-in skills. They ship with the server, so a local copy goes stale when the server moves; status reports that.")
	workspaceSkillsStatusCmd.Flags().Bool("include-builtin", false,
		"Also check the platform's built-in skills")
	workspaceSkillsStatusCmd.Flags().String("dir", "", "Local skills directory to check (required)")
}

type workspaceSkill struct {
	ID          string      `json:"id"`
	Source      string      `json:"source,omitempty"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Content     string      `json:"content"`
	Files       []skillFile `json:"files"`
}

type skillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func runWorkspaceSkills(cmd *cobra.Command, args []string, statusOnly bool) error {
	dir := strings.TrimSpace(mustString(cmd, "dir"))
	if dir == "" {
		return fmt.Errorf("--dir is required: name the skills directory, e.g. ~/.claude/skills")
	}
	root, err := expandUserPath(dir)
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
		Slug string `json:"slug"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	skills, err := fetchWorkspaceSkills(ctx, client)
	if err != nil {
		return err
	}
	if includeBuiltin, _ := cmd.Flags().GetBool("include-builtin"); includeBuiltin {
		builtins, err := fetchBuiltinSkills(ctx, client)
		if err != nil {
			// A server too old to serve built-ins still serves workspace
			// skills; losing those too would be a worse answer than a note.
			fmt.Fprintf(os.Stderr, "Note: built-in skills unavailable (%v); mirroring workspace skills only.\n", err)
		} else {
			skills = append(skills, builtins...)
		}
	}
	if len(skills) == 0 {
		return fmt.Errorf("workspace %s has no skills to mirror", ws.Slug)
	}

	force, _ := cmd.Flags().GetBool("force")

	// Classify everything before writing anything. A loop that writes as it
	// goes leaves half the directory updated when it hits the first name it
	// must not touch, and "some of your skills are new and some are not" is a
	// worse state than either outcome.
	classified := classifyLocalSkills(root, skills, force)

	if statusOnly {
		return reportSkillStatus(dir, ws.Slug,
			classified.current, classified.stale, classified.missing, classified.conflicts)
	}
	toWrite := classified.toWrite
	conflicts, current := classified.conflicts, classified.current

	// Mirror everything that is ours, then report what was not. Refusing the
	// whole run over one conflicting name would let a single deliberate local
	// router — a thin file that exists precisely to defer to the workspace —
	// block every other skill from arriving.
	var written []string
	for _, p := range toWrite {
		if err := writePulledSkill(p.target, ws.Slug, p.skill, p.body, p.print); err != nil {
			return fmt.Errorf("write %s: %w", p.name, err)
		}
		written = append(written, p.name)
	}
	sort.Strings(written)
	// Recorded even when nothing needed writing: the directory is a mirror of
	// this workspace either way, and that is what the patrol needs to know.
	notePullTarget(pullTargetKindSkills, root, ws.Slug)
	switch {
	case len(written) > 0:
		fmt.Fprintf(os.Stderr, "Mirrored %d skill(s) from %s into %s: %s\n",
			len(written), ws.Slug, dir, strings.Join(written, ", "))
	case len(conflicts) == 0:
		fmt.Fprintf(os.Stderr, "%s is already current with workspace %s (%d skills).\n", dir, ws.Slug, len(current))
	}

	if len(conflicts) > 0 && !force {
		sort.Strings(conflicts)
		return fmt.Errorf(
			"left alone (not written by this command): %s. "+
				"Keep yours, mirror into a different directory, or pass --force to replace them",
			strings.Join(conflicts, ", "))
	}
	return nil
}

// skillPlan is one skill's local destination and the bytes that belong there.
type skillPlan struct {
	name, target, body, print string
	skill                     workspaceSkill
}

// localSkillState is how a local directory compares to the workspace.
type localSkillState struct {
	current, stale, missing, conflicts []string
	toWrite                            []skillPlan
}

// classifyLocalSkills compares the mirror on disk against the workspace
// without writing anything.
//
// Everything is classified before anything is written: a loop that writes as
// it goes leaves half the directory updated when it hits the first name it
// must not touch, and "some of your skills are new and some are not" is worse
// than either outcome.
//
// Extracted so the daily patrol asks this exactly the way `skills status`
// does. A second implementation of "is this copy current" is how the same
// mirror ends up described two ways.
func classifyLocalSkills(root string, skills []workspaceSkill, force bool) localSkillState {
	var state localSkillState
	for _, skill := range skills {
		name := sanitizePulledSkillName(skill.Name)
		if name == "" {
			fmt.Fprintf(os.Stderr, "Note: skipping skill %q (no usable directory name).\n", skill.Name)
			continue
		}
		target := filepath.Join(root, name)
		body := renderPulledSkill(skill)
		print := pulledSkillFingerprint(body, skill.Files)

		marker, hasMarker := readPulledSkillMarker(target)
		_, statErr := os.Stat(target)

		switch {
		case os.IsNotExist(statErr):
			state.missing = append(state.missing, name)
		case !hasMarker:
			// Somebody's own skill sits here. Never quietly replace it.
			state.conflicts = append(state.conflicts, name)
			if !force {
				continue
			}
		case marker.Fingerprint == print:
			state.current = append(state.current, name)
			continue
		default:
			state.stale = append(state.stale, name)
		}
		state.toWrite = append(state.toWrite, skillPlan{name, target, body, print, skill})
	}
	return state
}

func reportSkillStatus(dir, slug string, current, stale, missing, conflicts []string) error {
	for _, group := range []*[]string{&current, &stale, &missing, &conflicts} {
		sort.Strings(*group)
	}
	fmt.Fprintf(os.Stderr, "%s vs workspace %s: %d current", dir, slug, len(current))
	for label, group := range map[string][]string{"stale": stale, "missing": missing, "not ours": conflicts} {
		if len(group) > 0 {
			fmt.Fprintf(os.Stderr, ", %d %s (%s)", len(group), label, strings.Join(group, ", "))
		}
	}
	fmt.Fprintln(os.Stderr)

	if len(stale)+len(missing) > 0 {
		return fmt.Errorf("%d skill(s) need pulling", len(stale)+len(missing))
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("%d local skill(s) share a name with a workspace skill", len(conflicts))
	}
	return nil
}

func fetchWorkspaceSkills(ctx context.Context, client *cli.APIClient) ([]workspaceSkill, error) {
	var listed []struct {
		ID string `json:"id"`
	}
	if err := client.GetJSON(ctx, "/api/skills", &listed); err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	skills := make([]workspaceSkill, 0, len(listed))
	for _, row := range listed {
		var full workspaceSkill
		// The list endpoint carries no body, so each skill is fetched. One
		// unreadable skill must not lose the rest: report it and continue.
		if err := client.GetJSON(ctx, "/api/skills/"+row.ID, &full); err != nil {
			fmt.Fprintf(os.Stderr, "Note: skipping skill %s (%v).\n", row.ID, err)
			continue
		}
		skills = append(skills, full)
	}
	return skills, nil
}

// renderPulledSkill produces the SKILL.md body. Frontmatter is synthesized only
// when the content has none: a skill that ships its own must reach a manual
// session byte-identical to what the daemon delivers, or the two surfaces
// describe the same skill differently.
func renderPulledSkill(skill workspaceSkill) string {
	body := skill.Content
	if strings.HasPrefix(strings.TrimLeft(body, " \t\r\n"), "---") {
		return body
	}
	name := sanitizePulledSkillName(skill.Name)
	desc := strings.ReplaceAll(strings.TrimSpace(skill.Description), "\n", " ")
	return fmt.Sprintf("---\nname: %s\ndescription: %q\n---\n\n%s", name, desc, body)
}

// pulledSkillFingerprint covers the SKILL.md AND its supporting files. A skill
// whose references changed while its body did not is still a changed skill,
// and a fingerprint that ignored them would report it current forever.
func pulledSkillFingerprint(body string, files []skillFile) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(body)))
	names := make([]string, 0, len(files))
	byPath := make(map[string]string, len(files))
	for _, f := range files {
		names = append(names, f.Path)
		byPath[f.Path] = f.Content
	}
	// Sorted, so the server returning the same files in a different order does
	// not read as a change.
	sort.Strings(names)
	for _, name := range names {
		h.Write([]byte("\x00" + name + "\x00" + byPath[name]))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func writePulledSkill(target, slug string, skill workspaceSkill, body, print string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(body), 0o644); err != nil {
		return err
	}
	for _, f := range skill.Files {
		clean := filepath.Clean(f.Path)
		// A skill's supporting files are content from the server. A path that
		// climbs out of the skill directory would write anywhere the user can.
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			fmt.Fprintf(os.Stderr, "Note: skipping %s in %s (path escapes the skill directory).\n", f.Path, skill.Name)
			continue
		}
		path := filepath.Join(target, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	source := skill.Source
	if source == "" {
		source = "workspace"
	}
	marker, err := json.MarshalIndent(pulledSkillMarker{
		Workspace: slug, SkillID: skill.ID, Fingerprint: print, Source: source,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(target, pulledSkillSidecar), marker, 0o644)
}

func readPulledSkillMarker(target string) (pulledSkillMarker, bool) {
	raw, err := os.ReadFile(filepath.Join(target, pulledSkillSidecar))
	if err != nil {
		return pulledSkillMarker{}, false
	}
	var marker pulledSkillMarker
	if json.Unmarshal(raw, &marker) != nil {
		return pulledSkillMarker{}, false
	}
	return marker, true
}

// sanitizePulledSkillName keeps a skill name usable as a directory. Anything
// that could redirect the write — separators, dots, leading dashes — is
// dropped rather than escaped, because a name is not worth a path traversal.
func sanitizePulledSkillName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

// fetchBuiltinSkills reads the platform's built-in skills. They ship with the
// server binary rather than living in a workspace, so they carry a synthetic
// `builtin:<name>` id and the same stable hash the daemon uses.
func fetchBuiltinSkills(ctx context.Context, client *cli.APIClient) ([]workspaceSkill, error) {
	var builtins []workspaceSkill
	if err := client.GetJSON(ctx, "/api/skills/builtin", &builtins); err != nil {
		return nil, err
	}
	for i := range builtins {
		if builtins[i].Source == "" {
			builtins[i].Source = "builtin"
		}
	}
	return builtins, nil
}
