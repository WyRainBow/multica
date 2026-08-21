"use client";

import { useQuery } from "@tanstack/react-query";
import { BookMarked, GitBranch, ScrollText, Sparkles } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { skillListOptions } from "@multica/core/workspace/queries";
import { cardListOptions } from "@multica/core/docs/queries";
import type { Card } from "@multica/core/types";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

/**
 * Agent wiki — the front door of the workspace's shared assets (the concept
 * the whole page is named after). One screen answering "what do the agents
 * know": which skills they run with, which rules govern them, and what the
 * distillation loop has produced lately. Every section is a pointer, not a
 * copy — the assets live in their own tabs and repos; this view just makes
 * the three classes visible together with their counts and freshest cases.
 */
export function AgentWikiOverview() {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const { data: skills = [] } = useQuery(skillListOptions(wsId));
  const { data: cardsResponse } = useQuery(cardListOptions(wsId));
  const cards: Card[] = cardsResponse?.cards ?? [];

  // The case library is the distillation loop's output: docs filed under
  // AgentWiki/cases/ carry lessons a human (or agent) can act on, each
  // pointing back at the issue that produced it.
  const cases = cards
    .filter((c) => (c.kind ?? "").startsWith("AgentWiki/cases/"))
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at));

  return (
    <div className="space-y-6 p-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-lg border bg-card p-4">
          <div className="flex items-center gap-2 text-muted-foreground">
            <Sparkles className="size-3.5" />
            {t(($) => $.agentwiki.section_skills)}
          </div>
          <p className="mt-1 text-title-sm font-semibold tabular-nums">
            {skills.length}
          </p>
        </div>
        <div className="rounded-lg border bg-card p-4">
          <div className="flex items-center gap-2 text-muted-foreground">
            <GitBranch className="size-3.5" />
            {t(($) => $.agentwiki.section_cases)}
          </div>
          <p className="mt-1 text-title-sm font-semibold tabular-nums">
            {cases.length}
          </p>
        </div>
        <div className="rounded-lg border bg-card p-4">
          <div className="flex items-center gap-2 text-muted-foreground">
            <BookMarked className="size-3.5" />
            {t(($) => $.agentwiki.section_docs)}
          </div>
          <p className="mt-1 text-title-sm font-semibold tabular-nums">
            {cards.length}
          </p>
        </div>
      </div>

      <div>
        <h3 className="text-sm font-semibold">
          {t(($) => $.agentwiki.latest_cases)}
        </h3>
        {cases.length === 0 ? (
          <p className="mt-1 text-sm text-muted-foreground">
            {t(($) => $.agentwiki.no_cases)}
          </p>
        ) : (
          <ul className="mt-2 space-y-1.5">
            {cases.slice(0, 8).map((c) => (
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

      <div>
        <h3 className="text-sm font-semibold">
          {t(($) => $.agentwiki.rules)}
        </h3>
        <ul className="mt-2 space-y-1 text-sm text-muted-foreground">
          <li>
            <ScrollText className="mr-1.5 inline size-3.5" />
            {t(($) => $.agentwiki.rules_manual)}
          </li>
          <li>
            <ScrollText className="mr-1.5 inline size-3.5" />
            {t(($) => $.agentwiki.rules_repo)}
          </li>
        </ul>
      </div>
    </div>
  );
}
