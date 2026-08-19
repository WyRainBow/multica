import { afterEach, describe, expect, it } from "vitest";
import { ApiError } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { openDoneCommentReview } from "./open-done-comment-review";

describe("openDoneCommentReview", () => {
  afterEach(() => useModalStore.getState().close());

  it("opens the shared modal for the structured done blocker", () => {
    const blocker = {
      code: "comment_review_required",
      error: "review comments",
      issues: [{
        issue_id: "issue-1",
        identifier: "MUL-1",
        threads: [{
          thread_root_id: "root-1",
          content: "Question",
          reply_count: 1,
          last_activity_at: "2026-08-19T12:00:00Z",
          pinned: false,
        }],
      }],
    };
    expect(openDoneCommentReview(new ApiError("blocked", 409, "Conflict", blocker), ["issue-1", "issue-2"])).toBe(true);
    expect(useModalStore.getState()).toMatchObject({
      modal: "issue-done-comment-review",
      data: { blocker, issueIds: ["issue-1", "issue-2"] },
    });
  });

  it("leaves unrelated errors to the caller", () => {
    expect(openDoneCommentReview(new Error("network"))).toBe(false);
    expect(useModalStore.getState().modal).toBeNull();
  });
});
