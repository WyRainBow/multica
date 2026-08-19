package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const doneCommentReviewAction = "comments_reviewed_before_done"

type DoneCommentDisposition struct {
	IssueID             string `json:"issue_id,omitempty"`
	ThreadRootID        string `json:"thread_root_id"`
	LastActivityAt      string `json:"last_activity_at"`
	Action              string `json:"action"`
	ResolutionCommentID string `json:"resolution_comment_id,omitempty"`
}

type DoneCommentReviewRequest struct {
	Summary      string                   `json:"summary"`
	Dispositions []DoneCommentDisposition `json:"dispositions"`
}

type doneReviewThreadBlocker struct {
	ThreadRootID   string `json:"thread_root_id"`
	Content        string `json:"content"`
	ReplyCount     int32  `json:"reply_count"`
	LastActivityAt string `json:"last_activity_at"`
	Pinned         bool   `json:"pinned"`
}

type doneReviewIssueBlocker struct {
	IssueID    string                    `json:"issue_id"`
	Identifier string                    `json:"identifier"`
	Threads    []doneReviewThreadBlocker `json:"threads"`
}

type doneReviewActivityDetails struct {
	Summary      string                   `json:"summary"`
	Dispositions []DoneCommentDisposition `json:"dispositions"`
}

type doneReviewEvaluation struct {
	Blocker     *doneReviewIssueBlocker
	Accepted    []DoneCommentDisposition
	Resolutions []DoneCommentDisposition
}

func doneThreadSnapshot(at pgtype.Timestamptz) string {
	return at.Time.UTC().Format(time.RFC3339Nano)
}

func (h *Handler) priorDoneReviewKeeps(ctx context.Context, issue db.Issue) (map[string]string, error) {
	receipts, err := h.Queries.ListDoneCommentReviewReceipts(ctx, db.ListDoneCommentReviewReceiptsParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	kept := make(map[string]string)
	for _, raw := range receipts {
		var details doneReviewActivityDetails
		if json.Unmarshal(raw, &details) != nil {
			continue
		}
		for _, disposition := range details.Dispositions {
			if disposition.Action == "keep_unresolved" {
				if _, exists := kept[disposition.ThreadRootID]; !exists {
					kept[disposition.ThreadRootID] = disposition.LastActivityAt
				}
			}
		}
	}
	return kept, nil
}

func reviewDispositionsForIssue(review *DoneCommentReviewRequest, issueID string, rows []db.ListDoneReviewThreadsForIssueRow) (map[string]DoneCommentDisposition, error) {
	dispositions := make(map[string]DoneCommentDisposition)
	if review == nil {
		return dispositions, nil
	}
	knownRoots := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		knownRoots[uuidToString(row.ThreadRootID)] = struct{}{}
	}
	for _, disposition := range review.Dispositions {
		// Batch callers may send a single review payload for several issues;
		// explicit dispositions for another issue belong to that issue's pass.
		if disposition.IssueID != "" && disposition.IssueID != issueID {
			continue
		}
		if _, exists := knownRoots[disposition.ThreadRootID]; !exists {
			return nil, fmt.Errorf("comment review thread %s does not belong to issue %s", disposition.ThreadRootID, issueID)
		}
		if _, exists := dispositions[disposition.ThreadRootID]; exists {
			return nil, fmt.Errorf("duplicate comment review disposition for thread %s", disposition.ThreadRootID)
		}
		dispositions[disposition.ThreadRootID] = disposition
	}
	return dispositions, nil
}

func (h *Handler) evaluateDoneCommentReview(ctx context.Context, issue db.Issue, identifier string, review *DoneCommentReviewRequest) (doneReviewEvaluation, error) {
	rows, err := h.Queries.ListDoneReviewThreadsForIssue(ctx, db.ListDoneReviewThreadsForIssueParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return doneReviewEvaluation{}, fmt.Errorf("list comment threads: %w", err)
	}
	dispositions, err := reviewDispositionsForIssue(review, uuidToString(issue.ID), rows)
	if err != nil {
		return doneReviewEvaluation{}, err
	}
	priorKeeps, err := h.priorDoneReviewKeeps(ctx, issue)
	if err != nil {
		return doneReviewEvaluation{}, fmt.Errorf("list review receipts: %w", err)
	}

	issueID := uuidToString(issue.ID)
	evaluation := doneReviewEvaluation{}
	blocker := doneReviewIssueBlocker{IssueID: issueID, Identifier: identifier}
	for _, row := range rows {
		rootID := uuidToString(row.ThreadRootID)
		snapshot := doneThreadSnapshot(row.LastActivityAt)
		var disposition *DoneCommentDisposition
		if value, exists := dispositions[rootID]; exists {
			disposition = &value
		}

		// A thread is resolved only while its latest activity predates the
		// resolution. A reply created after that resolution reopens the review
		// obligation even though an older comment still carries resolved_at.
		threadResolved := row.ResolutionCommentID.Valid && row.ResolutionAt.Valid &&
			!row.ResolutionAt.Time.Before(row.LastActivityAt.Time)
		if threadResolved {
			if disposition != nil {
				if disposition.Action != "resolve" || disposition.LastActivityAt != snapshot ||
					disposition.ResolutionCommentID != uuidToString(row.ResolutionCommentID) {
					return doneReviewEvaluation{}, fmt.Errorf("invalid disposition for resolved thread %s", rootID)
				}
				evaluation.Accepted = append(evaluation.Accepted, *disposition)
			}
			continue
		}

		if priorKeeps[rootID] == snapshot {
			continue
		}
		if disposition != nil && disposition.LastActivityAt == snapshot {
			switch disposition.Action {
			case "keep_unresolved":
				evaluation.Accepted = append(evaluation.Accepted, *disposition)
				continue
			case "resolve":
				if disposition.ResolutionCommentID == "" {
					return doneReviewEvaluation{}, fmt.Errorf("resolution_comment_id is required for thread %s", rootID)
				}
				evaluation.Accepted = append(evaluation.Accepted, *disposition)
				evaluation.Resolutions = append(evaluation.Resolutions, *disposition)
				continue
			}
		}
		content := strings.TrimSpace(row.Content)
		if len([]rune(content)) > 240 {
			content = string([]rune(content)[:240]) + "…"
		}
		blocker.Threads = append(blocker.Threads, doneReviewThreadBlocker{
			ThreadRootID: rootID, Content: content, ReplyCount: row.ReplyCount,
			LastActivityAt: snapshot, Pinned: row.PinnedAt.Valid,
		})
	}
	if len(blocker.Threads) > 0 {
		evaluation.Blocker = &blocker
		return evaluation, nil
	}
	if len(evaluation.Accepted) > 0 && strings.TrimSpace(review.Summary) == "" {
		return doneReviewEvaluation{}, errors.New("comment review summary is required")
	}
	return evaluation, nil
}

// recheckDoneCommentReview repeats the live thread query immediately before a
// status write. It deliberately uses the same evaluator as the initial gate:
// a reply that arrives after the first pass changes its snapshot and therefore
// becomes a fresh blocker instead of silently inheriting an older decision.
func (h *Handler) recheckDoneCommentReview(ctx context.Context, issue db.Issue, identifier string, review *DoneCommentReviewRequest) (doneReviewEvaluation, error) {
	return h.evaluateDoneCommentReview(ctx, issue, identifier, review)
}

func writeDoneReviewRequired(w http.ResponseWriter, blockers []doneReviewIssueBlocker) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":   "comment_review_required",
		"error":  "请先处理评论：逐条选择解决或明确保留，然后附上聚合收尾说明",
		"issues": blockers,
	})
}

func (h *Handler) commentBelongsToDoneReviewThread(ctx context.Context, issue db.Issue, rootID, commentID pgtype.UUID) (bool, error) {
	current := commentID
	for depth := 0; depth < 1000; depth++ {
		comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID: current, WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || comment.IssueID != issue.ID {
			return false, err
		}
		if !comment.ParentID.Valid {
			return comment.ID == rootID, nil
		}
		current = comment.ParentID
	}
	return false, errors.New("comment thread exceeds maximum depth")
}

func (h *Handler) applyDoneReviewResolutions(r *http.Request, issue db.Issue, dispositions []DoneCommentDisposition, actorType, actorID string) error {
	if len(dispositions) == 0 {
		return nil
	}
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		return err
	}
	for _, disposition := range dispositions {
		rootID, err := util.ParseUUID(disposition.ThreadRootID)
		if err != nil {
			return fmt.Errorf("invalid thread_root_id: %w", err)
		}
		commentID, err := util.ParseUUID(disposition.ResolutionCommentID)
		if err != nil {
			return fmt.Errorf("invalid resolution_comment_id: %w", err)
		}
		belongs, err := h.commentBelongsToDoneReviewThread(r.Context(), issue, rootID, commentID)
		if err != nil || !belongs {
			return fmt.Errorf("resolution comment does not belong to thread %s", disposition.ThreadRootID)
		}

		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			return err
		}
		qtx := h.Queries.WithTx(tx)
		cleared, err := qtx.ClearOtherThreadResolutions(r.Context(), db.ClearOtherThreadResolutionsParams{
			TargetID: commentID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		})
		if err == nil {
			_, err = qtx.ResolveComment(r.Context(), db.ResolveCommentParams{
				ID: commentID, ResolvedByType: pgtype.Text{String: actorType, Valid: true}, ResolvedByID: actorUUID,
			})
		}
		if err != nil {
			tx.Rollback(r.Context())
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		for _, comment := range cleared {
			h.publish(protocol.EventCommentUnresolved, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
				"comment": commentToResponse(comment, nil, nil),
			})
		}
		updated, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{ID: commentID, WorkspaceID: issue.WorkspaceID})
		if err == nil {
			h.publish(protocol.EventCommentResolved, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
				"comment": commentToResponse(updated, nil, nil),
			})
		}
	}
	return nil
}

func (h *Handler) recordDoneCommentReview(ctx context.Context, issue db.Issue, review *DoneCommentReviewRequest, accepted []DoneCommentDisposition, actorType, actorID string) error {
	if review == nil || len(accepted) == 0 {
		return nil
	}
	details, err := json.Marshal(doneReviewActivityDetails{Summary: strings.TrimSpace(review.Summary), Dispositions: accepted})
	if err != nil {
		return err
	}
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		return err
	}
	_, err = h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		ActorType: pgtype.Text{String: actorType, Valid: true}, ActorID: actorUUID,
		Action: doneCommentReviewAction, Details: details,
	})
	return err
}
