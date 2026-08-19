"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import type { DoneCommentReviewRequired } from "@multica/core/types";
import { parseDoneCommentReviewRequired } from "@multica/core/issues/done-comment-review";
import { useBatchUpdateIssues, useUpdateIssue } from "@multica/core/issues/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

export function DoneCommentReviewModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const [blocker, setBlocker] = useState(
    (data?.blocker as DoneCommentReviewRequired | undefined) ?? null,
  );
  const [kept, setKept] = useState<Set<string>>(new Set());
  const [summary, setSummary] = useState("");
  const updateIssue = useUpdateIssue();
  const batchUpdate = useBatchUpdateIssues();
  const requestedIssueIds = Array.isArray(data?.issueIds)
    ? data.issueIds.filter((id): id is string => typeof id === "string")
    : [];

  const threads = useMemo(
    () => blocker?.issues.flatMap((issue) =>
      issue.threads.map((thread) => ({ ...thread, issueId: issue.issue_id, identifier: issue.identifier })),
    ) ?? [],
    [blocker],
  );
  const submitting = updateIssue.isPending || batchUpdate.isPending;
  const ready = threads.length > 0 && threads.every((thread) => kept.has(thread.thread_root_id)) && summary.trim().length > 0;

  if (!blocker) return null;

  const submit = async () => {
    const commentReview = {
      summary: summary.trim(),
      dispositions: threads.map((thread) => ({
        issue_id: thread.issueId,
        thread_root_id: thread.thread_root_id,
        last_activity_at: thread.last_activity_at,
        action: "keep_unresolved" as const,
      })),
    };
    try {
      const issueIds = requestedIssueIds.length > 0
        ? requestedIssueIds
        : blocker.issues.map((issue) => issue.issue_id);
      if (issueIds.length === 1) {
        await updateIssue.mutateAsync({ id: issueIds[0]!, status: "done", comment_review: commentReview });
      } else {
        await batchUpdate.mutateAsync({ ids: issueIds, updates: { status: "done", comment_review: commentReview } });
      }
      onClose();
    } catch (error) {
      const refreshed = parseDoneCommentReviewRequired(error);
      if (refreshed) {
        setBlocker(refreshed);
        setKept(new Set());
        toast.error(t(($) => $.done_comment_review.changed));
        return;
      }
      toast.error(error instanceof Error ? error.message : t(($) => $.done_comment_review.failed));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !submitting) onClose(); }}>
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.done_comment_review.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.done_comment_review.description)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          {threads.map((thread) => (
            <label key={thread.thread_root_id} className="flex gap-3 rounded-lg border p-3">
              <Checkbox
                checked={kept.has(thread.thread_root_id)}
                onCheckedChange={(checked) => setKept((current) => {
                  const next = new Set(current);
                  if (checked === true) next.add(thread.thread_root_id); else next.delete(thread.thread_root_id);
                  return next;
                })}
              />
              <span className="min-w-0 space-y-1">
                <span className="block text-caption text-muted-foreground">{thread.identifier}{thread.pinned ? ` · ${t(($) => $.done_comment_review.pinned)}` : ""}</span>
                <span className="block text-body">{thread.content || t(($) => $.done_comment_review.empty)}</span>
                <span className="block text-caption text-muted-foreground">{t(($) => $.done_comment_review.keep)}</span>
              </span>
            </label>
          ))}
          <Textarea
            value={summary}
            onChange={(event) => setSummary(event.target.value)}
            placeholder={t(($) => $.done_comment_review.summary_placeholder)}
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={submitting}>{t(($) => $.done_comment_review.back)}</Button>
          <Button onClick={() => void submit()} disabled={!ready || submitting}>{t(($) => $.done_comment_review.confirm)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
