package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica workspace export / apply — the workspace's assets, under version control.
//
// Skills and instructions live only in the database. `multica skill` has no
// history, no diff and no rollback, so a rule changes and the only trace is
// whatever the person who changed it happened to write in a comment. This
// mirrors all three asset classes into a git repository, which supplies both
// the history and — through pull requests — the approval step the database
// cannot enforce on its own.
//
// The database stays the runtime source of truth. Nothing here touches the
// injection path: `skills pull`, `instructions pull` and the daemon's brief
// read the same rows they always did.
//
// The two directions are deliberately asymmetric, and the asymmetry is the
// design rather than an omission:
//
//   - skills and instructions flow repo -> db, gated by a merged pull request.
//   - AgentWiki flows db -> repo only. Drafts and decision cards are working
//     memory written by patrol runs and retros; routing them through review
//     would stall the daily loop, and their own promotion step already has a
//     human in it.
//
// So `apply` writes skills and instructions and never touches agentwiki/. A
// command that applied both directions would let an export overwrite a review
// that was mid-flight.

const (
	mirrorManifestFile = ".multica-export.json"
	// mirrorSkillSidecar keeps a skill's identity BESIDE its body rather than
	// inside it. SKILL.md's frontmatter is a runtime contract — name and
	// description are parsed for routing — so an id or a timestamp in there
	// would make the mirrored copy differ from what the daemon serves, force
	// `apply` to strip fields back out, and put a changed export timestamp in
	// front of every real diff a reviewer is trying to read.
	mirrorSkillSidecar = ".multica.yml"
	// mirrorWikiPageLimit is one page of the card listing. The workspace has
	// under a hundred documents today; paging is here so it keeps working when
	// it does not.
	mirrorWikiPageLimit = 100
)

// mirrorManifest records what the last export produced. Fingerprints let a
// re-export skip unchanged files, which is what keeps a diff meaningful: a run
// that rewrote every file would bury one real change under sixty no-ops.
type mirrorManifest struct {
	Workspace    string            `json:"workspace"`
	ExportedAt   string            `json:"exported_at"`
	Instructions string            `json:"instructions"`
	Skills       map[string]string `json:"skills"`
	Wiki         map[string]string `json:"wiki"`
}

var workspaceExportCmd = &cobra.Command{
	Use:   "export [workspace-id|slug|prefix]",
	Short: "Mirror the workspace's skills, instructions and AgentWiki into a git repository",
	Long: `Mirror the workspace's assets into a directory, one file per asset.

Skills and instructions have no version history in the database. This writes
them where git can keep one, alongside the workspace's own AgentWiki pages.

Read-only against the workspace: it writes files, never rows. Unchanged files
are left alone so a diff shows what actually changed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceExport,
}

var workspaceApplyCmd = &cobra.Command{
	Use:   "apply [workspace-id|slug|prefix]",
	Short: "Write the repository's approved skills and instructions back into the workspace",
	Long: `Write the repository's skills and instructions back into the workspace.

This is the half of the loop a merged pull request feeds: the review happened
on GitHub, and this puts the approved text into the database.

AgentWiki is deliberately NOT applied — it flows the other way, from the
database into the mirror, so a patrol writing a draft is never blocked on
review.

Refuses to run unless the directory is a clean git checkout of its main
branch: anything else is content nobody approved. --check reports what would
change and writes nothing.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceApply,
}

func init() {
	workspaceCmd.AddCommand(workspaceExportCmd)
	workspaceCmd.AddCommand(workspaceApplyCmd)
	workspaceExportCmd.Flags().String("dir", "", "Mirror directory, e.g. ~/开源工具/multica-workspace (required)")
	_ = workspaceExportCmd.MarkFlagRequired("dir")
	workspaceApplyCmd.Flags().String("dir", "", "Mirror directory to read the approved assets from (required)")
	_ = workspaceApplyCmd.MarkFlagRequired("dir")
	workspaceApplyCmd.Flags().Bool("check", false, "Report what would change without writing anything")
	workspaceApplyCmd.Flags().Bool("allow-dirty", false,
		"Run against a dirty or non-main checkout. Only for the first import, before anything has been reviewed")
}

// ---------------------------------------------------------------- export

func runWorkspaceExport(cmd *cobra.Command, args []string) error {
	wsID, client, err := mirrorTarget(cmd, args)
	if err != nil {
		return err
	}
	root, err := expandUserPath(strings.TrimSpace(mustString(cmd, "dir")))
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

	previous := readMirrorManifest(root)
	next := mirrorManifest{
		Workspace:  ws.Slug,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Skills:     map[string]string{},
		Wiki:       map[string]string{},
	}
	var written, skipped int

	// Instructions.
	if body := strings.TrimSpace(ws.Context); body != "" {
		print := instructionsFingerprint(ws.Context)
		next.Instructions = print
		path := filepath.Join(root, "instructions", "workspace.md")
		if previous.Instructions == print && fileExists(path) {
			skipped++
		} else {
			doc := renderFrontmatter(map[string]string{
				"workspace":   ws.Slug,
				"fingerprint": print,
				"exported_at": next.ExportedAt,
			}, ws.Context)
			if err := writeMirrorFile(path, doc); err != nil {
				return err
			}
			written++
		}
	}

	// Skills.
	skills, err := fetchWorkspaceSkills(ctx, client)
	if err != nil {
		return fmt.Errorf("list skills: %w", err)
	}
	for _, skill := range skills {
		name := sanitizePulledSkillName(skill.Name)
		body := mirrorSkillBody(skill)
		print := pulledSkillFingerprint(body, skill.Files)
		next.Skills[name] = print
		dir := filepath.Join(root, "skills", name)
		if previous.Skills[name] == print && fileExists(filepath.Join(dir, "SKILL.md")) {
			skipped++
			continue
		}
		// SKILL.md is written byte-for-byte as the database holds it, so the
		// pull request diff is the rule change and nothing else.
		if err := writeMirrorFile(filepath.Join(dir, "SKILL.md"), body); err != nil {
			return err
		}
		sidecar := fmt.Sprintf("id: %s\nname: %s\nfingerprint: %s\nexported_at: %s\n",
			skill.ID, skill.Name, print, next.ExportedAt)
		if err := writeMirrorFile(filepath.Join(dir, mirrorSkillSidecar), sidecar); err != nil {
			return err
		}
		for _, f := range skill.Files {
			rel, ok := safeRelativePath(f.Path)
			if !ok {
				fmt.Fprintf(os.Stderr, "Note: skipping %s in %s (path escapes the skill directory).\n", f.Path, skill.Name)
				continue
			}
			if err := writeMirrorFile(filepath.Join(dir, rel), f.Content); err != nil {
				return err
			}
		}
		written++
	}

	// Every document the workspace has written down, AgentWiki and issue
	// artefacts alike. The kind is already a path, so it maps onto
	// directories without inventing a second taxonomy — and the top level is
	// `wiki/` rather than `agentwiki/` because only part of that tree is
	// AgentWiki; filing COC-311/spec under an agentwiki/ directory would
	// label it as something it is not.
	cards, err := fetchAllCards(ctx, client)
	if err != nil {
		return fmt.Errorf("list cards: %w", err)
	}
	used := map[string]string{}
	for _, card := range cards {
		rel := wikiRelativePath(card, used)
		next.Wiki[card.ID] = rel
		doc := renderFrontmatter(map[string]string{
			"id":         card.ID,
			"kind":       card.Kind,
			"title":      card.Title,
			"issue":      card.IssueID,
			"created_at": card.CreatedAt,
			"updated_at": card.UpdatedAt,
		}, card.Content)
		path := filepath.Join(root, rel)
		if existing, err := os.ReadFile(path); err == nil && string(existing) == doc {
			skipped++
			continue
		}
		if err := writeMirrorFile(path, doc); err != nil {
			return err
		}
		written++
	}

	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := writeMirrorFile(filepath.Join(root, mirrorManifestFile), string(raw)+"\n"); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Exported workspace %s into %s: %d written, %d unchanged (%d skills, %d documents).\n",
		ws.Slug, root, written, skipped, len(skills), len(cards))
	return nil
}

// ---------------------------------------------------------------- apply

func runWorkspaceApply(cmd *cobra.Command, args []string) error {
	wsID, client, err := mirrorTarget(cmd, args)
	if err != nil {
		return err
	}
	root, err := expandUserPath(strings.TrimSpace(mustString(cmd, "dir")))
	if err != nil {
		return err
	}
	check, _ := cmd.Flags().GetBool("check")
	allowDirty, _ := cmd.Flags().GetBool("allow-dirty")
	if !check && !allowDirty {
		if err := requireReviewedCheckout(root); err != nil {
			return err
		}
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
	if manifest := readMirrorManifest(root); manifest.Workspace != "" && manifest.Workspace != ws.Slug {
		return fmt.Errorf("this mirror was exported from workspace %q but the current profile points at %q; "+
			"applying it would overwrite one workspace with another's rules", manifest.Workspace, ws.Slug)
	}

	var changed, unchanged int

	// Instructions.
	if raw, err := os.ReadFile(filepath.Join(root, "instructions", "workspace.md")); err == nil {
		_, body := splitFrontmatter(string(raw))
		switch {
		case strings.TrimSpace(body) == "":
			fmt.Fprintln(os.Stderr, "Note: instructions/workspace.md has no body; leaving the workspace instructions alone.")
		case instructionsFingerprint(body) == instructionsFingerprint(ws.Context):
			unchanged++
		case check:
			fmt.Fprintf(os.Stdout, "would update instructions (%s -> %s)\n",
				instructionsFingerprint(ws.Context), instructionsFingerprint(body))
			changed++
		default:
			var updated map[string]any
			if err := client.PatchJSON(ctx, "/api/workspaces/"+wsID, map[string]any{"context": body}, &updated); err != nil {
				return fmt.Errorf("update instructions: %w", err)
			}
			// Read back rather than trusting the write. A silent no-op here
			// would leave the repository claiming a rule the runtime never got.
			var after struct {
				Context string `json:"context"`
			}
			if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &after); err != nil {
				return fmt.Errorf("read back instructions: %w", err)
			}
			if instructionsFingerprint(after.Context) != instructionsFingerprint(body) {
				return fmt.Errorf("instructions did not land: workspace still reports %s",
					instructionsFingerprint(after.Context))
			}
			fmt.Fprintln(os.Stdout, "updated instructions")
			changed++
		}
	}

	// Skills.
	live, err := fetchWorkspaceSkills(ctx, client)
	if err != nil {
		return fmt.Errorf("list skills: %w", err)
	}
	byID := make(map[string]workspaceSkill, len(live))
	seen := map[string]bool{}
	for _, s := range live {
		byID[s.ID] = s
	}

	entries, err := os.ReadDir(filepath.Join(root, "skills"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, "skills", entry.Name())
		body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Note: %s has no SKILL.md; skipping.\n", entry.Name())
			continue
		}
		id := sidecarField(dir, "id")
		if id == "" {
			// A directory with no sidecar is a skill someone added by hand.
			// Creating it is safe; guessing that an existing skill is "the
			// same one" by name is not.
			if check {
				fmt.Fprintf(os.Stdout, "would create skill %s\n", entry.Name())
				changed++
				continue
			}
			var created struct {
				ID string `json:"id"`
			}
			payload := map[string]any{"name": entry.Name(), "content": string(body)}
			if err := client.PostJSON(ctx, "/api/skills", payload, &created); err != nil {
				return fmt.Errorf("create skill %s: %w", entry.Name(), err)
			}
			sidecar := fmt.Sprintf("id: %s\nname: %s\nfingerprint: %s\nexported_at: %s\n",
				created.ID, entry.Name(), pulledSkillFingerprint(string(body), nil),
				time.Now().UTC().Format(time.RFC3339))
			if err := writeMirrorFile(filepath.Join(dir, mirrorSkillSidecar), sidecar); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "created skill %s (%s)\n", entry.Name(), created.ID)
			changed++
			continue
		}
		seen[id] = true
		current, known := byID[id]
		if !known {
			fmt.Fprintf(os.Stderr, "Note: %s names skill %s, which the workspace does not have; skipping.\n",
				entry.Name(), id)
			continue
		}
		if strings.TrimSpace(current.Content) == strings.TrimSpace(string(body)) {
			unchanged++
			continue
		}
		if check {
			fmt.Fprintf(os.Stdout, "would update skill %s (%s)\n", entry.Name(), id)
			changed++
			continue
		}
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/skills/"+id, map[string]any{"content": string(body)}, &result); err != nil {
			return fmt.Errorf("update skill %s: %w", entry.Name(), err)
		}
		var after workspaceSkill
		if err := client.GetJSON(ctx, "/api/skills/"+id, &after); err != nil {
			return fmt.Errorf("read back skill %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(after.Content) != strings.TrimSpace(string(body)) {
			return fmt.Errorf("skill %s did not land: the workspace still holds different content", entry.Name())
		}
		fmt.Fprintf(os.Stdout, "updated skill %s\n", entry.Name())
		changed++
	}

	// Deletion is reported, never performed. A skill missing from the mirror
	// is more often a stale checkout than an intent to delete, and deleting
	// the wrong rule cannot be undone from here.
	var orphans []string
	for _, s := range live {
		if !seen[s.ID] {
			orphans = append(orphans, s.Name)
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		fmt.Fprintf(os.Stderr,
			"Note: the workspace has %d skill(s) the mirror does not: %s. Nothing was deleted — export first, or remove them by hand if that is the intent.\n",
			len(orphans), strings.Join(orphans, ", "))
	}

	verb := "applied"
	if check {
		verb = "would apply"
	}
	fmt.Fprintf(os.Stdout, "%s: %d changed, %d already current.\n", verb, changed, unchanged)
	return nil
}

// requireReviewedCheckout is the gate. The whole point of the mirror is that a
// human approved what is in it; a dirty tree or a feature branch holds text
// nobody signed off on, and applying it would route around the review this
// command exists to enforce.
func requireReviewedCheckout(root string) error {
	if out, err := runGitLocal(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("%s is not a git checkout (%s); apply reads only from a reviewed repository",
			root, strings.TrimSpace(out))
	}
	branch, err := runGitLocal(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("read branch: %w", err)
	}
	if b := strings.TrimSpace(branch); b != "main" && b != "master" {
		return fmt.Errorf("on branch %q; apply runs on main only, because a branch is by definition not yet reviewed", b)
	}
	status, err := runGitLocal(root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("the checkout has uncommitted changes; commit or stash them first, "+
			"otherwise unreviewed edits ride in with the approved ones:\n%s", strings.TrimSpace(status))
	}
	return nil
}

// ---------------------------------------------------------------- shared

func mirrorTarget(cmd *cobra.Command, args []string) (string, *cli.APIClient, error) {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return "", nil, err
	}
	if wsID == "" {
		return "", nil, fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return "", nil, err
	}
	return wsID, client, nil
}

type mirrorCard struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func fetchAllCards(ctx context.Context, client *cli.APIClient) ([]mirrorCard, error) {
	var all []mirrorCard
	for offset := 0; ; offset += mirrorWikiPageLimit {
		var page struct {
			Cards []mirrorCard `json:"cards"`
			Total int          `json:"total"`
		}
		path := fmt.Sprintf("/api/cards?limit=%d&offset=%d", mirrorWikiPageLimit, offset)
		if err := client.GetJSON(ctx, path, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Cards...)
		if len(page.Cards) < mirrorWikiPageLimit || len(all) >= page.Total {
			return all, nil
		}
	}
}

// wikiRelativePath turns a card into a file path. The kind is already a path,
// so it becomes directories; the title becomes the filename because that is
// what a person scanning the tree reads. Two documents can share both, so a
// collision falls back to the id — silently overwriting one with the other
// would lose a document with no error anywhere.
func wikiRelativePath(card mirrorCard, used map[string]string) string {
	parts := []string{"wiki"}
	for _, segment := range strings.Split(card.Kind, "/") {
		if s := sanitizeMirrorSegment(segment); s != "" {
			parts = append(parts, s)
		}
	}
	name := sanitizeMirrorSegment(card.Title)
	if name == "" {
		name = card.ID
	}
	rel := filepath.Join(append(parts, name+".md")...)
	if owner, taken := used[rel]; taken && owner != card.ID {
		short := card.ID
		if len(short) > 8 {
			short = short[:8]
		}
		rel = filepath.Join(append(parts, name+"-"+short+".md")...)
	}
	used[rel] = card.ID
	return rel
}

// sanitizeMirrorSegment keeps the name readable while making it a legal path
// component. Chinese is left alone — these directories are read by people who
// named them in Chinese.
func sanitizeMirrorSegment(s string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-",
		"?", "-", "\"", "'", "<", "-", ">", "-", "|", "-", "\n", " ", "\r", " ")
	out := strings.TrimSpace(replacer.Replace(s))
	out = strings.Trim(out, ". ")
	// A long title makes a path some filesystems reject. Cut on runes, not
	// bytes, or a Chinese title splits mid-character.
	if runes := []rune(out); len(runes) > 80 {
		out = strings.TrimSpace(string(runes[:80]))
	}
	return out
}

// mirrorSkillBody is what SKILL.md holds: the database's content, byte for
// byte. `apply` writes this file straight back, so anything export adds here
// lands in the database on the next round trip — the mirror would be rewriting
// the rule it exists to record. That rules out the synthesized frontmatter the
// local mirror uses: a runtime needs one, a review does not.
func mirrorSkillBody(skill workspaceSkill) string {
	return skill.Content
}

func safeRelativePath(p string) (string, bool) {
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", false
	}
	return clean, true
}

// renderFrontmatter puts the metadata above the body. Empty fields are dropped
// rather than written blank: `issue:` with nothing after it reads as a card
// with no issue, which is a claim, not an absence of one.
func renderFrontmatter(fields map[string]string, body string) string {
	keys := make([]string, 0, len(fields))
	for k, v := range fields {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		value := strings.ReplaceAll(fields[k], "\n", " ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(quoteFrontmatterValue(value))
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")
	return b.String()
}

// quoteFrontmatterValue quotes only what YAML would otherwise misread. Titles
// here start with '[' often enough that leaving them bare would produce a
// flow sequence instead of a string.
func quoteFrontmatterValue(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, ":#[]{}&*!|>'\"%@`,") || strings.HasPrefix(v, " ") || strings.HasSuffix(v, " ") {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
	}
	return v
}

// splitFrontmatter returns the block and the body. A file with no frontmatter
// is all body — that is a hand-written file, not a broken one.
func splitFrontmatter(doc string) (front, body string) {
	trimmed := strings.TrimLeft(doc, " \t\r\n")
	if !strings.HasPrefix(trimmed, "---\n") {
		return "", doc
	}
	rest := trimmed[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", doc
	}
	after := rest[end+4:]
	return rest[:end], strings.TrimPrefix(strings.TrimLeft(after, "\r"), "\n")
}

func sidecarField(dir, key string) string {
	raw, err := os.ReadFile(filepath.Join(dir, mirrorSkillSidecar))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func readMirrorManifest(root string) mirrorManifest {
	var m mirrorManifest
	raw, err := os.ReadFile(filepath.Join(root, mirrorManifestFile))
	if err != nil {
		return mirrorManifest{Skills: map[string]string{}, Wiki: map[string]string{}}
	}
	if json.Unmarshal(raw, &m) != nil {
		return mirrorManifest{Skills: map[string]string{}, Wiki: map[string]string{}}
	}
	if m.Skills == nil {
		m.Skills = map[string]string{}
	}
	if m.Wiki == nil {
		m.Wiki = map[string]string{}
	}
	return m
}

func writeMirrorFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
