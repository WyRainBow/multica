"use client";

import { Pencil } from "lucide-react";
import type { Issue, Retro } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../../i18n";

/**
 * One lesson, as a card.
 *
 * The requirement chip is the card's only navigation: clicking the card body
 * opens the editor, because a retro is text someone wrote and the common
 * action on it is "read the rest / fix a line", not "go somewhere else".
 */
export function RetroCard({
  retro,
  issue,
  onEdit,
  onOpenIssue,
}: {
  retro: Retro;
  /** Resolved requirement, when the retro points at one that still exists. */
  issue?: Issue;
  onEdit: () => void;
  onOpenIssue: (identifier: string) => void;
}) {
  const { t } = useT("retros");
  const timeAgo = useTimeAgo();

  return (
    <div className="group flex flex-col gap-2 rounded-lg border bg-card p-3 transition-colors hover:border-foreground/20">
      <div className="flex items-start gap-2">
        <button
          type="button"
          onClick={onEdit}
          className="min-w-0 flex-1 text-left text-body font-medium leading-snug hover:underline"
        >
          {retro.title}
        </button>
        <Button
          size="icon-xs"
          variant="ghost"
          onClick={onEdit}
          aria-label={t(($) => $.card.edit)}
          className="opacity-0 transition-opacity group-hover:opacity-100"
        >
          <Pencil className="size-3.5" />
        </Button>
      </div>

      {retro.content.trim() && (
        <p className="line-clamp-4 whitespace-pre-wrap text-caption text-muted-foreground">
          {retro.content}
        </p>
      )}

      <div className="mt-auto flex items-center gap-2 pt-1 text-caption text-muted-foreground">
        {issue ? (
          <button
            type="button"
            onClick={() => onOpenIssue(issue.identifier)}
            className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-medium tabular-nums transition-colors hover:text-foreground"
          >
            {issue.identifier}
          </button>
        ) : retro.issue_id ? (
          // The retro still names a requirement, but it is not in the loaded
          // set — deleted, or outside this workspace's window. Say so rather
          // than rendering a chip that goes nowhere.
          <span className="shrink-0 text-muted-foreground">
            {t(($) => $.card.issue_missing)}
          </span>
        ) : null}
        <span className="ml-auto shrink-0">{timeAgo(retro.created_at)}</span>
      </div>
    </div>
  );
}
