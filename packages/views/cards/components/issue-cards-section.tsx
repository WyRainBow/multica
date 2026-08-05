"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { cardsForIssueOptions } from "@multica/core/cards/queries";
import type { Card } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { ReadonlyContent } from "../../editor";
import { useT, useTimeAgo } from "../../i18n";
import { CardEditorDialog } from "./card-editor-dialog";

/**
 * "What did this teach us", on the requirement's own page.
 *
 * Oldest first, unlike the workspace list: read together, a requirement's
 * cards are a narrative of how the work went, and a narrative reads forwards.
 *
 * Rendered for every issue, not only parents. A sub-issue can be the one that
 * actually taught you something, and gating the section on having children
 * would hide it exactly there.
 */
export function IssueCardsSection({ issueId }: { issueId: string }) {
  const { t } = useT("cards");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const [editing, setEditing] = useState<Card | null>(null);
  const [creating, setCreating] = useState(false);

  const { data: cards = [] } = useQuery(cardsForIssueOptions(wsId, issueId));

  return (
    <section className="mt-6">
      <div className="flex items-center gap-2">
        <h2 className="text-body font-medium">{t(($) => $.issue_section.title)}</h2>
        {cards.length > 0 && (
          <span className="text-caption text-muted-foreground">{cards.length}</span>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => setCreating(true)}
        >
          <Plus className="size-3.5" />
          {t(($) => $.issue_section.add)}
        </Button>
      </div>

      {cards.length === 0 ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.issue_section.empty)}
        </p>
      ) : (
        <ul className="mt-2 flex flex-col gap-2">
          {cards.map((card) => (
            <li key={card.id}>
              <button
                type="button"
                onClick={() => setEditing(card)}
                className="flex w-full flex-col gap-1 rounded-lg border bg-card p-3 text-left transition-colors hover:border-foreground/20"
              >
                <div className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 truncate text-body font-medium">
                    {card.title}
                  </span>
                  <span className="shrink-0 text-caption text-muted-foreground">
                    {timeAgo(card.created_at)}
                  </span>
                </div>
                {card.content.trim() && (
                  // Rendered Markdown, same as the cards page — showing the
                  // source would put `**bold**` and link syntax on screen.
                  // Shallower cap here: this is a secondary section on someone
                  // else's page, so it teases rather than shows.
                  <div className="relative max-h-20 overflow-hidden">
                    <ReadonlyContent
                      content={card.content}
                      className="text-caption"
                    />
                    <span
                      aria-hidden
                      className="pointer-events-none absolute inset-x-0 bottom-0 h-6 bg-gradient-to-t from-card to-transparent"
                    />
                  </div>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}

      {(creating || editing) && (
        <CardEditorDialog
          card={editing}
          issueId={creating ? issueId : undefined}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
    </section>
  );
}
