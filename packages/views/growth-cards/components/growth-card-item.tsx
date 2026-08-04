"use client";

import { Pencil } from "lucide-react";
import type { GrowthCard, Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../../i18n";
import {
  GROWTH_CARD_BODY_KEYS,
  filledBodyFields,
  filledCount,
} from "../fields";

/**
 * One growth card, collapsed to what fits.
 *
 * Shows the first two filled fields plus a count of the rest, rather than a
 * truncated dump of all seven: the value of the list is scanning for which
 * delivery a card is about, and the fields themselves are read in the editor.
 *
 * The requirement chip is the card's only navigation. Clicking anywhere else
 * opens the editor, because the common action on a half-written card is
 * "finish it", not "go somewhere else".
 */
export function GrowthCardItem({
  card,
  issue,
  onEdit,
  onOpenIssue,
}: {
  card: GrowthCard;
  /** Resolved requirement, when the card points at one that still exists. */
  issue?: Issue;
  onEdit: () => void;
  onOpenIssue: (identifier: string) => void;
}) {
  const { t } = useT("growth-cards");
  const timeAgo = useTimeAgo();
  const filled = filledBodyFields(card);
  const preview = filled.slice(0, 2);
  const hidden = filled.length - preview.length;

  return (
    <div className="group flex flex-col gap-2 rounded-lg border bg-card p-3 transition-colors hover:border-foreground/20">
      <div className="flex items-start gap-2">
        <button
          type="button"
          onClick={onEdit}
          className="min-w-0 flex-1 text-left text-body font-medium leading-snug hover:underline"
        >
          {card.title.trim() || t(($) => $.card.untitled)}
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

      {preview.map(({ key, value }) => (
        <div key={key} className="flex flex-col gap-0.5">
          <span className="text-caption font-medium text-muted-foreground">
            {t(($) => $.fields[key].label)}
          </span>
          <p className="line-clamp-2 whitespace-pre-wrap text-caption text-muted-foreground">
            {value}
          </p>
        </div>
      ))}

      <div className="mt-auto flex items-center gap-2 pt-1 text-caption text-muted-foreground">
        {issue ? (
          <button
            type="button"
            onClick={() => onOpenIssue(issue.identifier)}
            className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-medium tabular-nums transition-colors hover:text-foreground"
          >
            {issue.identifier}
          </button>
        ) : card.issue_id ? (
          // The card still names a requirement, but it is not in the loaded
          // set — deleted, or outside this workspace's window. Say so rather
          // than rendering a chip that goes nowhere.
          <span className="shrink-0">{t(($) => $.card.issue_missing)}</span>
        ) : null}
        {hidden > 0 && (
          <span className="shrink-0">
            {t(($) => $.card.more_fields, { count: hidden })}
          </span>
        )}
        <span className="ml-auto shrink-0 tabular-nums">
          {t(($) => $.card.progress, {
            filled: filledCount(card),
            total: GROWTH_CARD_BODY_KEYS.length,
          })}
        </span>
        <span className="shrink-0">{timeAgo(card.created_at)}</span>
      </div>
    </div>
  );
}
