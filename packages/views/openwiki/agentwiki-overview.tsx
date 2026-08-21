"use client";

import { useQuery } from "@tanstack/react-query";
import { GitBranch } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardListOptions } from "@multica/core/docs/queries";
import type { Card } from "@multica/core/types";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

/**
 * Agent wiki — the distilled-experience shelf and nothing else (user's
 * call: wiki docs and skills have their own tabs; this one is 经验沉淀复用).
 * The case library is the distillation loop's output: docs filed under
 * AgentWiki/cases/ carry lessons worth reusing, each pointing back at the
 * issue that produced it. Newest first, all of them — the shelf is capped
 * at the source (interview-retro keeps it ≤20), so the tab never paginates.
 */
export function AgentWikiOverview() {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const { data: cardsResponse } = useQuery(cardListOptions(wsId));
  const cards: Card[] = cardsResponse?.cards ?? [];
  const cases = cards
    .filter((c) => (c.kind ?? "").startsWith("AgentWiki/cases/"))
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at));

  return (
    <div className="p-4">
      <div className="flex items-center gap-2 text-muted-foreground">
        <GitBranch className="size-3.5" />
        {t(($) => $.agentwiki.section_cases)}
        <span className="tabular-nums">{cases.length}</span>
      </div>

      {cases.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          {t(($) => $.agentwiki.no_cases)}
        </p>
      ) : (
        <ul className="mt-3 space-y-1.5">
          {cases.map((c) => (
            <li key={c.id}>
              <button
                type="button"
                className="text-left text-sm hover:underline"
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
      )}
    </div>
  );
}
