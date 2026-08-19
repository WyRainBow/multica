import { parseDoneCommentReviewRequired } from "@multica/core/issues/done-comment-review";
import { useModalStore } from "@multica/core/modals";

export function openDoneCommentReview(error: unknown, issueIds?: string[]): boolean {
  const blocker = parseDoneCommentReviewRequired(error);
  if (!blocker) return false;
  useModalStore.getState().open("issue-done-comment-review", { blocker, issueIds });
  return true;
}
