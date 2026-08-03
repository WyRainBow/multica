"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { retrosForIssueOptions } from "@multica/core/retros/queries";
import type { Retro } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../../i18n";
import { RetroEditorDialog } from "./retro-editor-dialog";

/**
 * "What did this teach us", on the requirement's own page.
 *
 * Oldest first, unlike the workspace list: read together, a requirement's
 * retros are a narrative of how the work went, and a narrative reads forwards.
 *
 * Rendered for every issue, not only parents. A sub-issue can be the one that
 * actually taught you something, and gating the section on having children
 * would hide it exactly there.
 */
export function IssueRetrosSection({ issueId }: { issueId: string }) {
  const { t } = useT("retros");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const [editing, setEditing] = useState<Retro | null>(null);
  const [creating, setCreating] = useState(false);

  const { data: retros = [] } = useQuery(retrosForIssueOptions(wsId, issueId));

  return (
    <section className="mt-6">
      <div className="flex items-center gap-2">
        <h2 className="text-body font-medium">{t(($) => $.issue_section.title)}</h2>
        {retros.length > 0 && (
          <span className="text-caption text-muted-foreground">{retros.length}</span>
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

      {retros.length === 0 ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.issue_section.empty)}
        </p>
      ) : (
        <ul className="mt-2 flex flex-col gap-2">
          {retros.map((retro) => (
            <li key={retro.id}>
              <button
                type="button"
                onClick={() => setEditing(retro)}
                className="flex w-full flex-col gap-1 rounded-lg border bg-card p-3 text-left transition-colors hover:border-foreground/20"
              >
                <div className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 truncate text-body font-medium">
                    {retro.title}
                  </span>
                  <span className="shrink-0 text-caption text-muted-foreground">
                    {timeAgo(retro.created_at)}
                  </span>
                </div>
                {retro.content.trim() && (
                  <p className="line-clamp-3 whitespace-pre-wrap text-caption text-muted-foreground">
                    {retro.content}
                  </p>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}

      {(creating || editing) && (
        <RetroEditorDialog
          retro={editing}
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
