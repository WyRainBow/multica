"use client";

import { Pencil } from "lucide-react";
import type { Issue, Card } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * One card, as a row in the timeline.
 *
 * Full width and roomy rather than a tile in a grid: a card is prose, and
 * prose in a narrow column is read a third of a line at a time. Six lines of
 * body before the clamp — enough to tell whether this is the note you were
 * looking for without opening it.
 *
 * No timestamp here: the timeline rail to the left already carries it, and a
 * second copy inside the card would be the same fact twice.
 *
 * Clicking anywhere but the requirement chip opens the editor — the common
 * action on a note is "read the rest / fix a line", not "go somewhere else".
 */
export function CardItem({
  card,
  issue,
  onEdit,
  onOpenIssue,
}: {
  card: Card;
  /** Resolved requirement, when the card points at one that still exists. */
  issue?: Issue;
  onEdit: () => void;
  onOpenIssue: (identifier: string) => void;
}) {
  const { t } = useT("cards");
  const title = card.title.trim();
  const body = card.content.trim();

  return (
    <div className="group rounded-lg border bg-card px-5 py-4 transition-colors hover:border-foreground/20">
      <div className="flex items-start gap-3">
        <button
          type="button"
          onClick={onEdit}
          className="min-w-0 flex-1 text-left"
        >
          <h3 className="text-title-sm font-semibold leading-snug group-hover:underline">
            {title || t(($) => $.card.untitled)}
          </h3>
        </button>
        <Button
          size="icon-xs"
          variant="ghost"
          onClick={onEdit}
          aria-label={t(($) => $.card.edit)}
          className="shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
        >
          <Pencil className="size-3.5" />
        </Button>
      </div>

      {body && (
        <button
          type="button"
          onClick={onEdit}
          className="mt-2 block w-full text-left"
        >
          <p className="line-clamp-6 whitespace-pre-wrap text-body leading-relaxed text-muted-foreground">
            {body}
          </p>
        </button>
      )}

      {(issue || card.issue_id) && (
        <div className="mt-3 flex items-center gap-2 text-caption text-muted-foreground">
          {issue ? (
            <button
              type="button"
              onClick={() => onOpenIssue(issue.identifier)}
              className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-medium tabular-nums transition-colors hover:text-foreground"
            >
              {issue.identifier}
            </button>
          ) : (
            // The card still names a requirement, but it is not in the loaded
            // set — deleted, or outside this workspace's window. Say so rather
            // than rendering a chip that goes nowhere.
            <span className="shrink-0">{t(($) => $.card.issue_missing)}</span>
          )}
        </div>
      )}
    </div>
  );
}
