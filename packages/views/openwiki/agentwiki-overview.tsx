"use client";

import { useQuery } from "@tanstack/react-query";
import { BookMarked } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardListOptions } from "@multica/core/docs/queries";
import type { Card } from "@multica/core/types";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { agentWikiShelves } from "./agentwiki-kinds";

/**
 * Agent wiki — the distilled experience, shelf by shelf.
 *
 * Every shelf is whatever segment follows `AgentWiki/`, read off the documents
 * themselves. It used to render one hardcoded shelf, cases, which left a
 * playbook written through the CLI on no page at all: the wiki tab excludes
 * the whole prefix and this tab never asked for it. Naming four shelves
 * instead of one would only have moved that to the fifth.
 */
export function AgentWikiOverview() {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const { data: cardsResponse } = useQuery(cardListOptions(wsId));
  const cards: Card[] = cardsResponse?.cards ?? [];
  const shelves = agentWikiShelves(cards);

  if (shelves.length === 0) {
    return (
      <div className="p-4">
        <p className="text-body text-muted-foreground">
          {t(($) => $.agentwiki.no_cases)}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-5 p-4">
      {shelves.map((shelf) => (
        <section key={shelf.slug}>
          <h3 className="flex items-baseline gap-2 border-b pb-1">
            <BookMarked className="size-3.5 self-center text-muted-foreground" />
            <span className="text-body font-medium">
              {shelfLabel(shelf.slug, t)}
            </span>
            <span className="text-caption text-muted-foreground tabular-nums">
              {shelf.cards.length}
            </span>
          </h3>
          <ul className="mt-2 space-y-1.5">
            {shelf.cards.map((c) => (
              <li key={c.id}>
                <button
                  type="button"
                  className="text-left text-body hover:underline"
                  onClick={() => nav.push(paths.docDetail(c.id))}
                >
                  <span className="font-medium">{c.title}</span>
                  {c.issue_id && (
                    <span className="ml-2 text-caption text-muted-foreground">
                      {t(($) => $.agentwiki.from_issue)}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

/**
 * A shelf nobody has translated is labelled by its own name rather than
 * dropped — the point of deriving shelves is that a new one shows up the day
 * it is created, and waiting on a translation would undo that.
 */
function shelfLabel(
  slug: string,
  t: ReturnType<typeof useT<"openwiki">>["t"],
): string {
  switch (slug) {
    case "cases":
      return t(($) => $.agentwiki.section_cases);
    case "patterns":
      return t(($) => $.agentwiki.section_patterns);
    case "playbooks":
      return t(($) => $.agentwiki.section_playbooks);
    case "assets":
      return t(($) => $.agentwiki.section_assets);
    default:
      return slug;
  }
}
