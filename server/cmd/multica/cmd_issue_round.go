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

// multica issue round close — end a review round in one action.
//
// A round used to end in four remembered steps: write the conclusion comment,
// resolve it, file the conclusion somewhere, update whatever section claims to
// say where the issue stands. Any one of them could be skipped, and the one
// skipped most was the last — which is how an issue ends up with a "current
// conclusions" section that stopped being true three rounds ago.
//
// So the four are one command, and the spec section is derived from the round
// documents rather than typed. Closing is the only way it is written; there is
// nothing to keep in sync by hand.

const (
	roundsFolder      = "rounds"
	specFolder        = "spec"
	latestConclusion  = "review.latest_conclusion"
	latestRoundDocKey = "review.latest_round_doc"
)

var issueRoundCmd = &cobra.Command{
	Use:   "round",
	Short: "Close a review round and refresh what the issue currently says",
	Long: `Close a review round and refresh what the issue currently says.

A round ends with a verdict, a conclusion that outlives the thread it was
argued in, and a spec that still describes where the issue stands. Closing does
all of it at once: the round document is written, the spec's round section is
rebuilt from every round document on the issue, the conclusion is posted to the
phase and resolved, and the pointers are updated.

The spec section is derived, never typed. A hand-kept summary of conclusions is
a copy, and a copy stops being updated the first time someone is in a hurry —
after which it is worse than missing, because it is still believed.`,
}

var issueRoundCloseCmd = &cobra.Command{
	Use:   "close <issue-id>",
	Short: "Record this round's verdict and rebuild the spec from it",
	Args:  exactArgs(1),
	RunE:  runIssueRoundClose,
}

func init() {
	issueCmd.AddCommand(issueRoundCmd)
	issueRoundCmd.AddCommand(issueRoundCloseCmd)

	issueRoundCloseCmd.Flags().String("phase", "", "Review station this round closes, e.g. 代码评审 (required)")
	issueRoundCloseCmd.Flags().String("verdict", "", "approve | request_changes | block (required)")
	issueRoundCloseCmd.Flags().String("summary", "", "One line: what was decided (required)")
	issueRoundCloseCmd.Flags().Int("round", 0,
		"Round number at this station. Defaults to one past the highest already recorded HERE. "+
			"Pass it when earlier rounds were argued somewhere that left no round document — a terminal, "+
			"a chat, a comment thread — so this one lands after them instead of overwriting the sequence.")
	issueRoundCloseCmd.Flags().String("body", "", "The round's full conclusion, in Markdown")
	issueRoundCloseCmd.Flags().Bool("body-stdin", false, "Read the full conclusion from stdin")
	issueRoundCloseCmd.Flags().String("sha", "", "Baseline the round reviewed, so the next one can diff from it")
	issueRoundCloseCmd.Flags().String("verified-sha", "", "The commit the verdict was actually checked against — not the merge SHA, which can differ")
	issueRoundCloseCmd.Flags().String("evidence", "", "What the checks said, e.g. 'views 4044/4044, typecheck 6/6'. A verdict without it is an opinion")
	issueRoundCloseCmd.Flags().String("output", "json", "Output format: table or json")
}

var roundVerdicts = map[string]bool{
	"approve": true, "request_changes": true, "block": true,
}

type roundCloseResult struct {
	Round     int    `json:"round"`
	Phase     string `json:"phase"`
	Verdict   string `json:"verdict"`
	RoundDoc  string `json:"round_doc"`
	SpecDoc   string `json:"spec_doc"`
	CommentID string `json:"comment_id"`
}

func runIssueRoundClose(cmd *cobra.Command, args []string) error {
	phase := strings.TrimSpace(mustString(cmd, "phase"))
	verdict := strings.TrimSpace(mustString(cmd, "verdict"))
	summary := strings.TrimSpace(mustString(cmd, "summary"))
	if phase == "" || verdict == "" || summary == "" {
		return fmt.Errorf("--phase, --verdict and --summary are all required")
	}
	if !roundVerdicts[verdict] {
		return fmt.Errorf("--verdict must be approve, request_changes or block")
	}

	body, err := readRoundBody(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issue, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	key := strings.TrimSpace(issue.Display)
	if key == "" {
		key = issue.ID
	}

	docs, err := fetchIssueDocs(ctx, client, issue.ID)
	if err != nil {
		return fmt.Errorf("read the issue's documents: %w", err)
	}
	rounds := roundsFromDocs(key, docs)
	number := NextRoundNumber(rounds, phase)
	// A round argued in a terminal, a chat, or a comment thread leaves no
	// document, so the count above cannot see it. Closing such a station for
	// the first time writes R1 over a history that already ran further, and a
	// write-once document has no second chance to say so. --round places it.
	if explicit, _ := cmd.Flags().GetInt("round"); explicit > 0 {
		if explicit < number {
			return fmt.Errorf(
				"--round %d would collide with R%d, already recorded at %s; pass %d or higher",
				explicit, explicit, phase, number)
		}
		number = explicit
	} else if number == 1 {
		fmt.Fprintf(os.Stderr,
			"Note: recording this as %s R1. If that station already argued rounds that were never closed here, "+
				"pass --round to place this one after them — a round document cannot be edited later.\n", phase)
	}

	// 1. The round's own document, write-once. It is the conclusion's body,
	//    which is why the comment can be a summary and the thread can be
	//    resolved without losing anything.
	roundKind := fmt.Sprintf("%s/%s/R%d-%s", key, roundsFolder, number, phase)
	verifiedSHA := strings.TrimSpace(mustString(cmd, "verified-sha"))
	evidence := strings.TrimSpace(mustString(cmd, "evidence"))
	// An approval nobody checked is the failure this column exists to expose,
	// so it is said out loud rather than left to be noticed in the table.
	if verdict == "approve" && verifiedSHA == "" && evidence == "" {
		fmt.Fprintf(os.Stderr, "Note: approving with no --verified-sha and no --evidence; the spec will record this round as unverified.\n")
	}
	roundBody := renderRoundBody(number, phase, verdict, summary, mustString(cmd, "sha"), verifiedSHA, evidence, body)
	roundDoc, err := createDoc(ctx, client, docRequest{
		Title:   fmt.Sprintf("%s R%d %s：%s", key, number, phase, summary),
		Kind:    roundKind,
		Content: roundBody,
		IssueID: issue.ID,
	})
	if err != nil {
		return fmt.Errorf("write the round document: %w", err)
	}
	fmt.Fprintf(os.Stderr, "R%d written to %s.\n", number, roundKind)

	// 2. The spec, rebuilt from every round document including the one just
	//    written. Derived, so there is nothing to remember to update.
	rounds = append(rounds, RoundDoc{
		Number: number, Phase: phase, Verdict: verdict,
		Summary: summary, VerifiedSHA: verifiedSHA, Evidence: evidence,
		DocID: roundDoc.ID,
	})
	// Closing is the moment the conclusions become true, so it is also the
	// moment everything argued so far is accounted for. Recording it lets a
	// later reader tell "these conclusions are current" from "these
	// conclusions predate the last three comments" without reading any of them.
	watermark := time.Now().UTC().Format(time.RFC3339)
	specDoc, err := upsertSpec(ctx, client, issue.ID, key, docs, rounds, watermark)
	if err != nil {
		// The round is already recorded; failing here must not read as though
		// nothing happened.
		fmt.Fprintf(os.Stderr, "Note: R%d is recorded, but the spec could not be refreshed (%v).\n", number, err)
	} else {
		fmt.Fprintf(os.Stderr, "Spec %s/%s refreshed.\n", key, specFolder)
	}

	// 3. The conclusion on the thread, then resolved — the argument stays
	//    readable, the verdict stops being buried in it.
	commentID, err := postRoundConclusion(ctx, client, issue.ID, phase, number, verdict, summary, roundDoc.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Note: R%d is recorded, but the conclusion comment failed (%v).\n", number, err)
	}

	// 4. Snapshot every live document. They are edited freely between rounds,
	//    so without this a closed round records a verdict on a version nobody
	//    can retrieve — the wiki keeps no history of its own. Closing is the
	//    only moment that means "this is what we agreed on", which is why the
	//    version granularity is one per close rather than one per edit.
	//
	//    Best-effort: the round is already recorded, and failing the command
	//    here would read as "nothing happened" when the archive exists.
	if taken, err := snapshotLiveDocs(ctx, client, issue.ID, key, docs, number, phase); err != nil {
		fmt.Fprintf(os.Stderr, "Note: R%d is recorded, but a document snapshot failed (%v).\n", number, err)
	} else if taken > 0 {
		fmt.Fprintf(os.Stderr, "Snapshotted %d live document(s).\n", taken)
	}

	// 5. Pointers, so a reader lands on the latest without scanning.
	setIssueMetadata(ctx, client, issue.ID, latestRoundDocKey, roundDoc.ID)
	if commentID != "" {
		setIssueMetadata(ctx, client, issue.ID, latestConclusion, commentID)
	}

	// 6. Move the station itself. Archiving a round used to leave every phase
	//    untouched, so a card could close two rounds and still show five
	//    stations nobody had entered — and the done gate, which only checks
	//    stations that were entered or completed, stayed inert for exactly the
	//    flow that produces round documents.
	//
	//    Entering is unconditional: the argument happened there whatever the
	//    verdict. Completing is not — request_changes and block mean the
	//    station runs again, and marking it finished would say the opposite.
	advanceRoundPhase(ctx, client, issue.ID, phase, verdict == "approve")

	result := roundCloseResult{
		Round: number, Phase: phase, Verdict: verdict,
		RoundDoc: roundDoc.ID, SpecDoc: specDoc, CommentID: commentID,
	}
	if verdict == "request_changes" {
		fmt.Fprintf(os.Stderr, "request_changes: open the next round before work resumes.\n")
	}
	// A route that ran to its end is worth a nudge; one sent back for changes
	// is not, because it has not finished being learned yet.
	if verdict != "request_changes" && shouldPromptRetro("", phase) {
		writeRetroPrompt(os.Stderr, key, phase+" 已收口")
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// advanceRoundPhase enters the station this round closed, and completes it
// when the verdict was an approval.
//
// Best-effort by design. The round document, the spec and the conclusion are
// already written by the time this runs; a phase that will not move is worth
// reporting but is not worth failing a recorded archive over. `enter` is
// idempotent on the server (it keeps the first arrival time), and `complete`
// is refused with 409 unless the phase was entered, which is why the order
// here is fixed rather than conditional.
func advanceRoundPhase(
	ctx context.Context,
	client *cli.APIClient,
	issueID, phase string,
	complete bool,
) {
	resolved, err := resolveIssuePhase(ctx, client, issueID, phase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Note: the round is recorded, but phase %q could not be resolved (%v).\n", phase, err)
		return
	}
	base := "/api/issues/" + issueID + "/phases/" + resolved.ID + "/"
	var discard map[string]any
	if err := client.PostJSON(ctx, base+"enter", map[string]any{}, &discard); err != nil {
		fmt.Fprintf(os.Stderr, "Note: the round is recorded, but phase %q could not be entered (%v).\n", resolved.Name, err)
		return
	}
	if !complete {
		fmt.Fprintf(os.Stderr, "Phase %q entered; left open — the verdict was not an approval.\n", resolved.Name)
		return
	}
	if err := client.PostJSON(ctx, base+"complete", map[string]any{}, &discard); err != nil {
		fmt.Fprintf(os.Stderr, "Note: the round is recorded, but phase %q could not be completed (%v).\n", resolved.Name, err)
		return
	}
	fmt.Fprintf(os.Stderr, "Phase %q completed.\n", resolved.Name)
}

func readRoundBody(cmd *cobra.Command) (string, error) {
	fromStdin, _ := cmd.Flags().GetBool("body-stdin")
	inline := mustString(cmd, "body")
	if fromStdin && strings.TrimSpace(inline) != "" {
		return "", fmt.Errorf("pass --body or --body-stdin, not both")
	}
	if fromStdin {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(raw), nil
	}
	return inline, nil
}

type docRequest struct {
	Title   string `json:"title"`
	Kind    string `json:"kind,omitempty"`
	Content string `json:"content"`
	IssueID string `json:"issue_id,omitempty"`
}

type docRow struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

func fetchIssueDocs(ctx context.Context, client *cli.APIClient, issueID string) ([]docRow, error) {
	var result struct {
		Cards []docRow `json:"cards"`
	}
	path := "/api/issues/" + url.PathEscape(issueID) + "/cards"
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return nil, err
	}
	return result.Cards, nil
}

// roundsFromDocs reads the closed rounds off the documents themselves. The
// summary is recovered from the title, which is where the closer put it — a
// round document is write-once, so there is nothing newer to prefer.
func roundsFromDocs(key string, docs []docRow) []RoundDoc {
	prefix := key + "/" + roundsFolder + "/"
	var rounds []RoundDoc
	for _, doc := range docs {
		if !strings.HasPrefix(doc.Kind, prefix) {
			continue
		}
		number, phase, ok := ParseRoundKind(strings.TrimPrefix(doc.Kind, prefix))
		if !ok {
			continue
		}
		rounds = append(rounds, RoundDoc{
			Number:      number,
			Phase:       phase,
			Verdict:     bodyField(doc.Content, "结论"),
			Summary:     summaryFromTitle(doc.Title),
			VerifiedSHA: bodyField(doc.Content, "验收版本"),
			Evidence:    bodyField(doc.Content, "验证证据"),
			DocID:       doc.ID,
		})
	}
	return rounds
}

func summaryFromTitle(title string) string {
	if _, after, found := strings.Cut(title, "："); found {
		return after
	}
	return title
}

// bodyField reads one "- 名称：值" line out of a round document. The document
// is write-once, so this is the whole record — rebuilding the spec must not
// silently drop a column that only lives there.
func bodyField(body, name string) string {
	prefix := "- " + name + "："
	for _, line := range strings.Split(body, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), prefix); found {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func renderRoundBody(number int, phase, verdict, summary, sha, verifiedSHA, evidence, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# R%d %s\n\n", number, phase)
	fmt.Fprintf(&b, "- 结论：%s\n", verdict)
	fmt.Fprintf(&b, "- 要点：%s\n", summary)
	if strings.TrimSpace(sha) != "" {
		// The next round diffs from here rather than re-reading everything.
		fmt.Fprintf(&b, "- 评审基线：%s\n", strings.TrimSpace(sha))
	}
	if verifiedSHA != "" {
		fmt.Fprintf(&b, "- 验收版本：%s\n", verifiedSHA)
	}
	if evidence != "" {
		fmt.Fprintf(&b, "- 验证证据：%s\n", evidence)
	}
	b.WriteString("\n")
	if strings.TrimSpace(body) != "" {
		b.WriteString(strings.TrimRight(body, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// upsertSpec rewrites the round section of the issue's spec, creating the spec
// if this is the first round to close.
func upsertSpec(
	ctx context.Context,
	client *cli.APIClient,
	issueID, key string,
	docs []docRow,
	rounds []RoundDoc,
	watermark string,
) (string, error) {
	specKind := key + "/" + specFolder
	for _, doc := range docs {
		if doc.Kind != specKind {
			continue
		}
		updated := ApplyRoundSection(doc.Content, rounds, watermark)
		if updated == doc.Content {
			return doc.ID, nil
		}
		var out docRow
		path := "/api/cards/" + url.PathEscape(doc.ID)
		if err := client.PutJSON(ctx, path, map[string]any{"content": updated}, &out); err != nil {
			return "", err
		}
		return doc.ID, nil
	}

	created, err := createDoc(ctx, client, docRequest{
		Title:   key + " spec",
		Kind:    specKind,
		Content: ApplyRoundSection("", rounds, watermark),
		IssueID: issueID,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func createDoc(ctx context.Context, client *cli.APIClient, req docRequest) (docRow, error) {
	var out docRow
	if err := client.PostJSON(ctx, "/api/cards", req, &out); err != nil {
		return docRow{}, err
	}
	return out, nil
}

func postRoundConclusion(
	ctx context.Context,
	client *cli.APIClient,
	issueID, phase string,
	number int,
	verdict, summary, docID string,
) (string, error) {
	content := fmt.Sprintf("R%d %s：**%s**\n\n%s\n\n正身：[R%d 结论](mention://doc/%s)",
		number, phase, verdict, summary, number, docID)

	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{"content": content}
	// The server takes a phase UUID; the station name is what a person or an
	// agent actually has, so it is resolved here like `comment add` does.
	if phase != "" {
		resolved, err := resolveIssuePhase(ctx, client, issueID, phase)
		if err != nil {
			return "", err
		}
		body["phase_id"] = resolved.ID
	}
	path := "/api/issues/" + url.PathEscape(issueID) + "/comments"
	if err := client.PostJSON(ctx, path, body, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", nil
	}
	// Resolving marks it as the thread's conclusion, which is what collapses
	// the argument behind it on the next read.
	var ignored map[string]any
	resolvePath := "/api/comments/" + url.PathEscape(created.ID) + "/resolve"
	if err := client.PostJSON(ctx, resolvePath, map[string]any{}, &ignored); err != nil {
		fmt.Fprintf(os.Stderr, "Note: the conclusion was posted but not resolved (%v).\n", err)
	}
	return created.ID, nil
}

// setIssueMetadata is advisory: a pointer that fails to update must not lose
// the round that was already recorded.
func setIssueMetadata(ctx context.Context, client *cli.APIClient, issueID, key, value string) {
	var out map[string]any
	path := "/api/issues/" + url.PathEscape(issueID) + "/metadata/" + url.PathEscape(key)
	if err := client.PutJSON(ctx, path, map[string]any{"value": value}, &out); err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s was not updated (%v).\n", key, err)
	}
}

// liveDocSuffixes are the documents a close snapshots. Mirrors the server's
// liveDocKinds; duplicated rather than shared because the two live in
// different binaries and a shared constant would tie a server release to a
// CLI one.
var liveDocSuffixes = []string{"/requirements", "/design", "/spec"}

// snapshotLiveDocs freezes each live document as it stood when the round
// closed.
//
// The wiki keeps no version history of its own — there is no card version
// table — so a document edited between rounds leaves no trace of what an
// earlier verdict was actually given on. The snapshot is that trace.
//
// Write-once, like the round it belongs to: the snapshot's kind carries the
// round number, so re-closing cannot overwrite an earlier one and a gap stays
// visible.
func snapshotLiveDocs(
	ctx context.Context,
	client *cli.APIClient,
	issueID, key string,
	docs []docRow,
	round int,
	phase string,
) (int, error) {
	taken := 0
	var firstErr error
	for _, doc := range docs {
		suffix, ok := liveDocSuffix(doc.Kind)
		if !ok {
			continue
		}
		if strings.TrimSpace(doc.Content) == "" {
			// An empty document has nothing to preserve, and a snapshot of
			// nothing would suggest the round was given nothing.
			continue
		}
		kind := fmt.Sprintf("%s/snapshots/%s/R%d-%s", key, strings.TrimPrefix(suffix, "/"), round, phase)
		body := fmt.Sprintf(
			"<!-- 收口 R%d %s 时的冻结副本。正身是 `%s`，那份会继续被修改；本份不会。 -->\n\n%s",
			round, phase, doc.Kind, doc.Content)
		if _, err := createDoc(ctx, client, docRequest{
			Title:   fmt.Sprintf("%s %s @ R%d %s", key, doc.Title, round, phase),
			Kind:    kind,
			Content: body,
			IssueID: issueID,
		}); err != nil {
			// One unwritable snapshot must not cost the others: each live
			// document is a separate record and losing all of them because of
			// one is a worse outcome than a partial set that says so.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		taken++
	}
	return taken, firstErr
}

func liveDocSuffix(kind string) (string, bool) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(kind), "/")
	for _, suffix := range liveDocSuffixes {
		if strings.HasSuffix(trimmed, suffix) {
			return suffix, true
		}
	}
	return "", false
}
