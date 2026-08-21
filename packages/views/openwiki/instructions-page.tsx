"use client";

import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ScrollText } from "lucide-react";
import { api } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

/**
 * Instructions — what every agent in this workspace obeys, on every task.
 *
 * The layer already existed and nobody could see it: `workspace.context` is
 * injected into each task's brief as "Workspace Context", editable only from a
 * field buried in settings, and empty in practice. An instruction nobody can
 * find is one nobody writes, which is why team rules kept living in each
 * person's own CLAUDE.md instead.
 *
 * Beside Wiki, Skills and the worktree ledger rather than in settings: it is an
 * asset the team writes, not a preference someone configures. Wiki explains
 * why, Skills say when, this says always.
 *
 * Saved on demand, not on a timer. An instruction takes effect for every agent
 * on the next task, so half a sentence must never ship because a debounce
 * fired mid-thought.
 */
export function InstructionsPage() {
  const { t } = useT("openwiki");
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();

  const saved = workspace?.context ?? "";
  const [draft, setDraft] = useState(saved);
  const [saving, setSaving] = useState(false);

  // Seed once the workspace resolves, but never over an edit in progress —
  // that would delete what is being typed.
  useEffect(() => {
    setDraft((current) => (current === "" ? saved : current));
  }, [saved]);

  const dirty = draft !== saved;

  const save = async () => {
    if (!workspace || saving) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { context: draft });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      await qc.invalidateQueries({ queryKey: workspaceKeys.all(workspace.id) });
      toast.success(t(($) => $.instructions_saved));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.instructions_save_failed),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Header on its own rule, like the wiki pages next door — the tab bar
        above is chrome, and a heading is what tells you where you landed. */}
      <div className="flex shrink-0 items-center gap-2 border-b px-4 py-3">
        <ScrollText className="size-4 text-muted-foreground" />
        <h1 className="text-title-sm font-medium">{t(($) => $.tab_instructions)}</h1>
        <span className="text-caption text-muted-foreground">
          {dirty
            ? t(($) => $.instructions_unsaved)
            : t(($) => $.instructions_effect)}
        </span>
        <Button
          size="sm"
          className="ml-auto"
          onClick={save}
          disabled={!dirty || saving}
        >
          {saving ? t(($) => $.instructions_saving) : t(($) => $.instructions_save)}
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        {/* Capped like the wiki's prose column: this is read as writing, and a
          line running the full width of a wide window is hard to scan. */}
        <div className="mx-auto flex h-full w-full max-w-3xl flex-col">
          <p className="mb-3 text-caption text-muted-foreground">
            {t(($) => $.instructions_hint)}
          </p>
          <Textarea
            aria-label={t(($) => $.tab_instructions)}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={t(($) => $.instructions_placeholder)}
            className="min-h-96 flex-1 resize-none font-mono text-body leading-relaxed"
          />
        </div>
      </div>
    </div>
  );
}
