package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/issuenamespace"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetIssueNamespace handles GET /api/issues/{id}/namespace.
//
// The one read that is allowed to see the empty slots. Every other card read
// filters placeholders out in SQL, which is what keeps them out of lists,
// search, agent briefs, and the round / decision numbering; this endpoint is
// how a reader finds out that a slot is waiting to be written rather than
// missing.
func (h *Handler) GetIssueNamespace(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	cards, err := h.Queries.ListIssueNamespaceCards(r.Context(), db.ListIssueNamespaceCardsParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list issue namespace cards failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load issue namespace")
		return
	}
	key := issuenamespace.Key(h.getIssuePrefix(r.Context(), issue.WorkspaceID), issue.Number)
	writeJSON(w, http.StatusOK, issuenamespace.View(uuidToString(issue.ID), key, cards))
}

// applyNamespaceStatusLifecycle keeps an issue's document directory in step
// with whether the issue is still being worked on.
//
// Entering a terminal status drops the slots still standing empty: a finished
// issue with six "待补" entries reads as unfinished work, and the honest record
// is the documents that were actually written. Leaving one restores the slots
// that are missing, because the questions are askable again.
//
// Runs on the caller's transaction, never after it. Cleanup that is a separate
// round trip is cleanup somebody has to remember to run, and the whole reason
// this exists is that remembering is not a control.
func applyNamespaceStatusLifecycle(
	ctx context.Context,
	qtx *db.Queries,
	prevStatus string,
	issue db.Issue,
) error {
	wasTerminal := isTerminalIssueStatus(prevStatus)
	nowTerminal := isTerminalIssueStatus(issue.Status)
	if wasTerminal == nowTerminal {
		// Only the CROSSING owes anything. done → cancelled has already been
		// pruned, and todo → in_progress has nothing to restore.
		return nil
	}
	if nowTerminal {
		return issuenamespace.Prune(ctx, qtx, issue)
	}
	ws, err := qtx.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load workspace for issue namespace reseed: %w", err)
	}
	return issuenamespace.Reseed(ctx, qtx, ws, issue)
}

// crossesTerminalBoundary reports whether an update is a status change that
// moves the issue into or out of a terminal status — the only case that needs
// the update wrapped in a transaction with the namespace work.
func crossesTerminalBoundary(prevStatus string, params db.UpdateIssueParams) bool {
	return params.Status.Valid &&
		isTerminalIssueStatus(prevStatus) != isTerminalIssueStatus(params.Status.String)
}

// updateIssueWithNamespaceLifecycle is UpdateIssue plus the directory work the
// transition owes, committed together.
//
// Falls through to the bare query when there is nothing to do, so an ordinary
// field edit does not pay for a transaction it has no use for.
func (h *Handler) updateIssueWithNamespaceLifecycle(
	ctx context.Context,
	params db.UpdateIssueParams,
	prevStatus string,
) (db.Issue, error) {
	if h.TxStarter == nil || !crossesTerminalBoundary(prevStatus, params) {
		return h.Queries.UpdateIssue(ctx, params)
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, fmt.Errorf("begin issue status update: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	issue, err := qtx.UpdateIssue(ctx, params)
	if err != nil {
		return db.Issue{}, err
	}
	if err := applyNamespaceStatusLifecycle(ctx, qtx, prevStatus, issue); err != nil {
		return db.Issue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, fmt.Errorf("commit issue status update: %w", err)
	}
	return issue, nil
}
