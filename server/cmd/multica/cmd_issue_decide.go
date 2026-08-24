package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
)

// multica issue decide — record a decision as an artefact instead of a comment.
//
// Decisions used to be a side effect of closing a review round, and most
// decisions are not review rounds: someone weighs options in a thread, picks
// one, and the choice survives only as prose in that thread. Every later run
// then rebuilds it by re-reading the argument, and nothing can say who decided
// or what the alternatives cost.
//
// The card is write-once. Status is never stored: a decision is superseded
// because a later card says it supersedes it, and an open question is open
// because no later card has closed it. There is exactly one way to change
// anything — write a new card referencing the old one — so there is no stored
// state that can disagree with the text.
//
// Recording is not deciding. The CLI signs whoever ran it, which is usually an
// agent writing down a choice a person made. Conflating the two would attribute
// every human decision to whichever agent typed it up, and half the value of a
// decision record is answering "who decided this".

const decisionsFolder = "decisions"

var issueDecideCmd = &cobra.Command{
	Use:   "decide <issue-id>",
	Short: "Record a decision as a write-once card on the issue",
	Long: `Records a decision where the next run will find it.

Most decisions are not review rounds: someone weighs options, picks one, and
the choice survives only in the thread it was argued in. This files it as an
artefact — the question, what was chosen, what the rejected options cost, and
who decided — so a later run reads the answer instead of the argument.

The card cannot be edited. To change a decision, record a new one that
supersedes it; to settle a question this one left open, record one that closes
it. Status follows from those references and is never stored, so nothing can
drift out of agreement with the text.

  multica issue decide COC-311 \
    --question "决策记录用什么载体" \
    --summary "独立决策卡，状态全派生" \
    --decided-by "用户" \
    --open "命令具体名称未定" \
    --supersedes D1 \
    --body-file ./decision.md`,
	Args: exactArgs(1),
	RunE: runIssueDecide,
}

func init() {
	issueCmd.AddCommand(issueDecideCmd)

	issueDecideCmd.Flags().String("question", "", "The question this decision answers, in one line (required)")
	issueDecideCmd.Flags().String("summary", "", "What was chosen, in one line (required)")
	issueDecideCmd.Flags().String("body", "", "The full record: alternatives, their costs, and the reasoning")
	issueDecideCmd.Flags().Bool("body-stdin", false, "Read the full record from stdin")
	issueDecideCmd.Flags().String("body-file", "", "Read the full record from a UTF-8 file")
	issueDecideCmd.Flags().String("decided-by", "",
		"Who made the call. Defaults to whoever runs this command; set it when recording someone else's decision, "+
			"because the signature identifies the recorder and attributing a person's decision to an agent loses "+
			"half of what a decision record is for.")
	issueDecideCmd.Flags().StringArray("open", nil,
		"A question this decision deliberately leaves open (repeatable). It stays open until a later decision closes it.")
	issueDecideCmd.Flags().StringArray("closes", nil,
		"An open question this decision settles, as D<n>#<i> — the card that raised it and its position in that card's list (repeatable).")
	issueDecideCmd.Flags().StringArray("supersedes", nil,
		"A decision this one replaces, as D<n> (repeatable). The superseded card is left untouched; it reads as superseded because this one says so.")
	issueDecideCmd.Flags().StringArray("affects", nil,
		"A live document this decision changed, e.g. requirements / design / spec (repeatable). "+
			"The decision itself stays here; the document keeps only the result.")
	issueDecideCmd.Flags().String("sha", "", "Baseline the decision was taken against")
	issueDecideCmd.Flags().Int("number", 0,
		"Decision number on this issue. Defaults to one past the highest already recorded. "+
			"Pass it when earlier decisions were taken somewhere that left no card — a comment thread, a terminal — "+
			"so this one lands after them instead of overwriting the sequence.")
	issueDecideCmd.Flags().String("output", "json", "Output format: table or json")
}

type decideResult struct {
	Number     int      `json:"number"`
	DocID      string   `json:"doc_id"`
	CommentID  string   `json:"comment_id,omitempty"`
	DecidedBy  string   `json:"decided_by"`
	RecordedBy string   `json:"recorded_by"`
	Supersedes []string `json:"supersedes,omitempty"`
	Closes     []string `json:"closes,omitempty"`
	Open       []string `json:"open,omitempty"`
}

func runIssueDecide(cmd *cobra.Command, args []string) error {
	question := strings.TrimSpace(mustString(cmd, "question"))
	summary := strings.TrimSpace(mustString(cmd, "summary"))
	if question == "" || summary == "" {
		return fmt.Errorf("--question and --summary are both required: a decision with neither is a note")
	}

	body, err := readDecisionBody(cmd)
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
	number := NextDecisionNumber(key, docs)
	// A decision taken in a comment thread leaves no card, so the count above
	// cannot see it. Recording such an issue for the first time writes D1 over
	// a history that already ran further, and a write-once card has no second
	// chance to say so.
	if explicit, _ := cmd.Flags().GetInt("number"); explicit > 0 {
		if explicit < number {
			return fmt.Errorf("--number %d would collide with D%d, already recorded; pass %d or higher",
				explicit, explicit, number)
		}
		number = explicit
	} else if number == 1 {
		fmt.Fprintf(os.Stderr,
			"Note: recording this as D1. If decisions were already taken on this issue without being carded, "+
				"pass --number to place this one after them — a decision card cannot be edited later.\n")
	}

	recordedBy := resolveDecisionRecorder(ctx, client)
	decidedBy := strings.TrimSpace(mustString(cmd, "decided-by"))
	if decidedBy == "" {
		// A dispatched agent may not let this default. The recorder comes from
		// /api/me, which names the human whose token signed the call and never
		// the agent, so defaulting puts that person's name on a decision they
		// may never have made — and the "on behalf of" notice further down
		// stays silent exactly because the two values match. That is the
		// misattribution --decided-by exists to prevent, running backwards.
		//
		// Gated on MULTICA_AGENT_ID rather than DetectHarness: harness
		// detection is documented as display-only and must never gate
		// anything. So this catches agents the daemon dispatched; one driven
		// by hand in a terminal is still on its operator to pass the flag.
		if os.Getenv("MULTICA_AGENT_ID") != "" {
			return fmt.Errorf("--decided-by is required when a dispatched agent records a decision: " +
				"the recorder is the human whose token signed this call, so leaving it to default would " +
				"file the decision as theirs. Name whoever actually made the call")
		}
		decidedBy = recordedBy
	}

	open, _ := cmd.Flags().GetStringArray("open")
	closes, _ := cmd.Flags().GetStringArray("closes")
	supersedes, _ := cmd.Flags().GetStringArray("supersedes")
	affects, _ := cmd.Flags().GetStringArray("affects")

	meta := DecisionMeta{
		ID:         fmt.Sprintf("D%d", number),
		Question:   question,
		Summary:    summary,
		DecidedBy:  decidedBy,
		RecordedBy: recordedBy,
		DecidedAt:  time.Now().UTC().Format(time.RFC3339),
		SHA:        strings.TrimSpace(mustString(cmd, "sha")),
		Open:       trimAll(open),
		Closes:     trimAll(closes),
		Supersedes: trimAll(supersedes),
		Affects:    trimAll(affects),
	}

	// Writing the card is the whole point; a failure here is fatal. Everything
	// after it is a pointer to a card that already exists, so failing the
	// command would read as "nothing happened" when something did.
	doc, err := createDoc(ctx, client, docRequest{
		Title:   fmt.Sprintf("%s %s：%s", key, meta.ID, summary),
		Kind:    fmt.Sprintf("%s/%s/%s", key, decisionsFolder, meta.ID),
		Content: RenderDecisionCard(meta, body),
		IssueID: issue.ID,
	})
	if err != nil {
		return fmt.Errorf("write the decision card: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s written to %s/%s/%s.\n", meta.ID, key, decisionsFolder, meta.ID)

	commentID, err := postDecisionPointer(ctx, client, issue.ID, meta, doc.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s is recorded, but the pointer comment failed (%v).\n", meta.ID, err)
	}

	if decidedBy != recordedBy {
		fmt.Fprintf(os.Stderr, "Recorded by %s on behalf of %s.\n", recordedBy, decidedBy)
	}
	if len(meta.Open) > 0 {
		fmt.Fprintf(os.Stderr, "%d question(s) left open; they stay open until a later decision closes them.\n", len(meta.Open))
	}

	result := decideResult{
		Number: number, DocID: doc.ID, CommentID: commentID,
		DecidedBy: decidedBy, RecordedBy: recordedBy,
		Supersedes: meta.Supersedes, Closes: meta.Closes, Open: meta.Open,
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// resolveDecisionRecorder names whoever is running this command. It is read
// from the server rather than taken from a flag: the recorder is a fact about
// the call, and a fact a caller can type is not one.
func resolveDecisionRecorder(ctx context.Context, client *cli.APIClient) string {
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err == nil {
		if name := strings.TrimSpace(me.Name); name != "" {
			return name
		}
		if email := strings.TrimSpace(me.Email); email != "" {
			return email
		}
	}
	// An unnamed recorder is still better than a wrong one: the card says the
	// identity could not be resolved rather than claiming somebody.
	return "unknown"
}

func postDecisionPointer(
	ctx context.Context,
	client *cli.APIClient,
	issueID string,
	meta DecisionMeta,
	docID string,
) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "> **%s · %s**\n\n", meta.ID, meta.Summary)
	fmt.Fprintf(&b, "问题：%s\n\n", meta.Question)
	if meta.DecidedBy != meta.RecordedBy {
		fmt.Fprintf(&b, "拍板者 %s，记录者 %s。\n\n", meta.DecidedBy, meta.RecordedBy)
	} else {
		fmt.Fprintf(&b, "拍板者 %s。\n\n", meta.DecidedBy)
	}
	if len(meta.Supersedes) > 0 {
		fmt.Fprintf(&b, "取代 %s。\n\n", strings.Join(meta.Supersedes, "、"))
	}
	fmt.Fprintf(&b, "正身：`%s`", docID)

	var out map[string]any
	err := client.PostJSON(ctx, "/api/issues/"+issueID+"/comments",
		map[string]any{"content": b.String()}, &out)
	if err != nil {
		return "", err
	}
	return strVal(out, "id"), nil
}

func readDecisionBody(cmd *cobra.Command) (string, error) {
	fromStdin, _ := cmd.Flags().GetBool("body-stdin")
	file := strings.TrimSpace(mustString(cmd, "body-file"))
	inline := mustString(cmd, "body")

	given := 0
	for _, used := range []bool{fromStdin, file != "", strings.TrimSpace(inline) != ""} {
		if used {
			given++
		}
	}
	if given > 1 {
		return "", fmt.Errorf("pass only one of --body, --body-file or --body-stdin")
	}
	switch {
	case fromStdin:
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(raw), nil
	case file != "":
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file, err)
		}
		return string(raw), nil
	default:
		return util.UnescapeBackslashEscapes(inline), nil
	}
}

func trimAll(in []string) []string {
	var out []string
	for _, s := range in {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
