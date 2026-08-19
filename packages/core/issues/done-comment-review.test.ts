import { describe, expect, it } from "vitest";
import { ApiError } from "../api/client";
import { parseDoneCommentReviewRequired } from "./done-comment-review";

describe("parseDoneCommentReviewRequired", () => {
  it("accepts the structured done blocker and defaults additive fields", () => {
    const error = new ApiError("blocked", 409, "Conflict", {
      code: "comment_review_required",
      error: "review comments",
      issues: [{
        issue_id: "issue-1",
        identifier: "MUL-1",
        threads: [{ thread_root_id: "root-1", last_activity_at: "2026-08-19T12:00:00Z" }],
      }],
    });
    expect(parseDoneCommentReviewRequired(error)?.issues[0]?.threads[0]).toMatchObject({
      content: "",
      reply_count: 0,
      pinned: false,
    });
  });

  it("fails closed for malformed or unrelated errors", () => {
    expect(parseDoneCommentReviewRequired(new ApiError("bad", 409, "Conflict", { code: "other" }))).toBeNull();
    expect(parseDoneCommentReviewRequired(new Error("network"))).toBeNull();
  });
});
