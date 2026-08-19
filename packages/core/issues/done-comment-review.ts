import { z } from "zod";
import { ApiError } from "../api/client";
import type { DoneCommentReviewRequired } from "../types";

const DoneCommentReviewRequiredSchema = z.object({
  code: z.literal("comment_review_required"),
  error: z.string(),
  issues: z.array(z.object({
    issue_id: z.string(),
    identifier: z.string(),
    threads: z.array(z.object({
      thread_root_id: z.string(),
      content: z.string().default(""),
      reply_count: z.number().int().nonnegative().default(0),
      last_activity_at: z.string(),
      pinned: z.boolean().default(false),
    }).loose()),
  }).loose()),
}).loose();

export function parseDoneCommentReviewRequired(error: unknown): DoneCommentReviewRequired | null {
  if (!(error instanceof ApiError) || error.status !== 409) return null;
  const parsed = DoneCommentReviewRequiredSchema.safeParse(error.body);
  return parsed.success ? parsed.data : null;
}
