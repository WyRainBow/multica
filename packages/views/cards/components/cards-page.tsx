"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lightbulb, Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardListOptions } from "@multica/core/cards/queries";
import { issueListOptions } from "@multica/core/issues/queries";
import type { Issue, Card } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { CardEditorDialog } from "./card-editor-dialog";
import { CardItem } from "./card-item";

/**
 * Everything a workspace has learned, newest first.
 *
 * A flat reverse-chronological list rather than a board or a tree: a card is
 * finished the moment it is written, so there is no state to move it through,
 * and grouping by the requirement it came from would bury the ones that came
 * from reading or from an incident.
 */
export function CardsPage() {
  const { t } = useT("cards");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<Card | null>(null);
  const [creating, setCreating] = useState(false);

  const { data, isLoading } = useQuery(cardListOptions(wsId));
  // The requirement a card points at is rendered as its identifier, so the
  // list needs the issues it references. One workspace query rather than one
  // per card.
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const issuesById = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

  const cards = data?.cards ?? [];
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return cards;
    return cards.filter(
      (card) =>
        card.title.toLowerCase().includes(query) ||
        card.content.toLowerCase().includes(query),
    );
  }, [cards, search]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <h1 className="text-title-sm font-medium">{t(($) => $.page.title)}</h1>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.page.count, { count: cards.length })}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t(($) => $.page.search_placeholder)}
            className="h-8 w-56 text-body"
          />
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" />
            {t(($) => $.page.new)}
          </Button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        {isLoading ? null : filtered.length === 0 ? (
          <EmptyState
            hasCards={cards.length > 0}
            onCreate={() => setCreating(true)}
          />
        ) : (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {filtered.map((card) => (
              <CardItem
                key={card.id}
                card={card}
                issue={card.issue_id ? issuesById.get(card.issue_id) : undefined}
                onEdit={() => setEditing(card)}
                onOpenIssue={(identifier) =>
                  navigation.push(paths.issueDetail(identifier))
                }
              />
            ))}
          </div>
        )}
      </div>

      {(creating || editing) && (
        <CardEditorDialog
          card={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

/**
 * Two different empty states. "Nothing written yet" invites the first card;
 * "nothing matches" must not, or the button silently discards the search the
 * user just typed.
 */
function EmptyState({
  hasCards,
  onCreate,
}: {
  hasCards: boolean;
  onCreate: () => void;
}) {
  const { t } = useT("cards");
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-24 text-center">
      <Lightbulb className="size-8 text-faint-foreground" />
      <p className="text-body text-muted-foreground">
        {hasCards ? t(($) => $.page.no_matches) : t(($) => $.page.empty)}
      </p>
      {!hasCards && (
        <Button size="sm" variant="outline" onClick={onCreate}>
          <Plus className="size-3.5" />
          {t(($) => $.page.new)}
        </Button>
      )}
    </div>
  );
}
