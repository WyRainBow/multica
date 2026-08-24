package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// issueIndexCommentType is the comment type the automatic index is posted as.
//
// Load-bearing, and not interchangeable with "comment". The done gate
// (ListDoneReviewThreadsForIssue) treats every unresolved type='comment' thread
// root as something that must be dispositioned before an issue can reach done,
// so posting the index as "comment" would give EVERY card in the system a
// permanent unaddressed thread. "system" — the type the platform's own
// narration uses — is not available either: POST /comments rejects it, because
// a client claiming that type would be forging system output.
const issueIndexCommentType = "progress_update"

// issueIndexCommentContent builds the pinned root index a new card is born
// with. It turns the team ledger rule ("每张卡建完发一条 root 评论并 comment
// pin，当本卡索引") into a property of the tool rather than something whoever
// filed the card has to remember.
//
// The opening line and the two section headings are byte-identical to the
// indexes already pinned by hand on existing cards, deliberately: a reader
// scanning a list should not be able to tell an automatic index from a manual
// one.
//
// The session line is a snapshot of which session filed the card, written by
// the machine at the moment of creation and never updated afterwards. That is
// what separates it from the code-progress pointer (`multica worktree show
// <name>`), which tracks a value that keeps moving and therefore belongs in the
// worktree, where `sync` measures it rather than anyone typing it. The author
// fields cannot carry this: every write here is signed by the same person, so
// the session id is the only thing that tells two concurrent sessions apart.
func issueIndexCommentContent(session string) string {
	recorded := "未记录"
	if session != "" {
		recorded = "`" + session + "`"
	}
	return "> 本卡索引，只列产物落点与当前状态，不含结论。\n" +
		"\n" +
		"## 产物落点\n" +
		"\n" +
		"- 待补\n" +
		"- 建卡会话（建卡当刻快照，不随会话变动）：" + recorded + "\n" +
		"- 调研：见调研记录阶段评论\n" +
		"\n" +
		"## 当前状态\n" +
		"\n" +
		"- 刚建卡，尚未开始"
}

// resolveIssueIndexSession picks the session id to stamp into the index.
//
// An explicit --session always wins; otherwise the id comes from the agent
// runtime this command is running inside, read exactly the way `worktree
// session --auto` reads it. Finding neither is not an error — the card is still
// filed, and the line says the session was not recorded rather than going
// missing, so a reader can tell "filed outside a session" from "we forgot".
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

// postIssueIndexComment posts the ledger index on the freshly created issue and
// pins it.
//
// Best effort, on the same reasoning as the attachment upload it follows: the
// issue already exists by the time this runs, so returning an error would
// invite the caller — an agent, usually — to retry the whole `issue create` and
// end up with a duplicate card. Both steps warn on stderr instead, and the
// index can be posted by hand.
func postIssueIndexComment(ctx context.Context, client *cli.APIClient, issueID, identifier, session string) {
	var comment map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", map[string]any{
		"content": issueIndexCommentContent(session),
		"type":    issueIndexCommentType,
	}, &comment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: post index comment failed (issue already created, %s): %v\n",
			identifier, err)
		return
	}

	commentID := strVal(comment, "id")
	if commentID == "" {
		fmt.Fprintf(os.Stderr, "warning: index comment on %s came back without an id, so it could not be pinned\n",
			identifier)
		return
	}
	if err := client.PostJSON(ctx, "/api/comments/"+url.PathEscape(commentID)+"/pin", nil, nil); err != nil {
		fmt.Fprintf(os.Stderr, "warning: pin index comment %s failed (issue already created, %s): %v\n",
			commentID, identifier, err)
	}
}
