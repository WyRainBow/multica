"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { growthCardsForIssueOptions } from "@multica/core/growth-cards/queries";
import type { GrowthCard } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../../i18n";
import { GROWTH_CARD_BODY_KEYS, filledBodyFields, filledCount } from "../fields";
import { GrowthCardEditorDialog } from "./growth-card-editor-dialog";

/**
 * "What did this teach me", on the requirement's own page.
 *
 * Oldest first, unlike the workspace list: read together, a requirement's
 * cards are a narrative of how the work went, and a narrative reads forwards.
 *
 * Rendered for every issue, not only parents. A sub-issue can be the one that
 * actually taught you something, and gating the section on having children
 * would hide it exactly there.
 */
export function IssueGrowthCardsSection({ issueId }: { issueId: string }) {
  const { t } = useT("growth-cards");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const [editing, setEditing] = useState<GrowthCard | null>(null);
  const [creating, setCreating] = useState(false);

  const { data: cards = [] } = useQuery(growthCardsForIssueOptions(wsId, issueId));

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
          {cards.map((card) => {
            const first = filledBodyFields(card)[0];
            return (
              <li key={card.id}>
                <button
                  type="button"
                  onClick={() => setEditing(card)}
                  className="flex w-full flex-col gap-1 rounded-lg border bg-card p-3 text-left transition-colors hover:border-foreground/20"
                >
                  <div className="flex items-baseline gap-2">
                    <span className="min-w-0 flex-1 truncate text-body font-medium">
                      {card.title.trim() || t(($) => $.card.untitled)}
                    </span>
                    <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
                      {t(($) => $.card.progress, {
                        filled: filledCount(card),
                        total: GROWTH_CARD_BODY_KEYS.length,
                      })}
                    </span>
                    <span className="shrink-0 text-caption text-muted-foreground">
                      {timeAgo(card.created_at)}
                    </span>
                  </div>
                  {first && (
                    <p className="line-clamp-2 whitespace-pre-wrap text-caption text-muted-foreground">
                      <span className="font-medium">
                        {t(($) => $.fields[first.key].label)}
                      </span>
                      {" · "}
                      {first.value}
                    </p>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {(creating || editing) && (
        <GrowthCardEditorDialog
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
