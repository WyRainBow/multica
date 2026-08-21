"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { worktreeEntriesOptions } from "@multica/core/worktrees/queries";
import { useCreateWorktreeEntry } from "@multica/core/worktrees/mutations";
import type { WorktreeEntry, WorktreeEntryKind } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../i18n";

/** The kinds this build writes. Reads accept anything the server sends, so a
 *  kind added later shows up as itself instead of disappearing. */
const ENTRY_KINDS: WorktreeEntryKind[] = [
  "progress",
  "branch",
  "merge",
  "blocked",
  "handoff",
  "verify",
];

// Colour carries the same meaning as the word, for scanning: what moved
// (progress), what changed shape (branch/merge), what stopped (blocked).
const KIND_TONE: Record<string, string> = {
  progress: "text-muted-foreground",
  branch: "text-muted-foreground",
  merge: "text-foreground",
  blocked: "text-destructive",
  handoff: "text-foreground",
  verify: "text-muted-foreground",
};

/**
 * One tree's ledger, plus the box that appends to it.
 *
 * The box is the point of the whole feature: a commit is too expensive a unit
 * for "what did I change this round", so rounds that never became commits used
 * to leave no trace at all. There is no edit and no delete — a line written in
 * error is corrected by writing the correction.
 */
export function WorktreeEntryList({ treeRef }: { treeRef: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const timeAgo = useTimeAgo();
  const [body, setBody] = useState("");
  const [kind, setKind] = useState<WorktreeEntryKind>("progress");

  // Server-driven enum: a kind this build has not heard of renders raw rather
  // than blank, so a newer backend cannot make lines look empty.
  const kindLabel = (kind: string) =>
    ENTRY_KINDS.includes(kind as WorktreeEntryKind)
      ? t(($) => $[`entry_kind_${kind}` as "entry_kind_progress"])
      : kind;

  const { data: entries = [], isLoading } = useQuery(
    worktreeEntriesOptions(wsId, treeRef, 30),
  );
  const createEntry = useCreateWorktreeEntry();

  const submit = () => {
    const text = body.trim();
    if (text === "" || createEntry.isPending) return;
    createEntry.mutate(
      { ref: treeRef, body: text, kind },
      { onSuccess: () => setBody("") },
    );
  };

  return (
    <div className="mt-3">
      <div className="flex items-center gap-2">
        <NativeSelect
          size="sm"
          value={kind}
          onChange={(e) => setKind(e.target.value as WorktreeEntryKind)}
          aria-label={t(($) => $.entry_kind_label)}
        >
          {ENTRY_KINDS.map((k) => (
            <NativeSelectOption key={k} value={k}>
              {kindLabel(k)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Input
          className="h-7 flex-1"
          value={body}
          placeholder={t(($) => $.entry_placeholder)}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        <Button
          size="sm"
          variant="secondary"
          disabled={body.trim() === "" || createEntry.isPending}
          onClick={submit}
        >
          {t(($) => $.entry_add)}
        </Button>
      </div>

      {isLoading ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.worktree_loading)}
        </p>
      ) : entries.length === 0 ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.entries_empty)}
        </p>
      ) : (
        <ul className="mt-2 space-y-1">
          {entries.map((entry: WorktreeEntry) => (
            <li key={entry.id} className="flex items-baseline gap-2 text-caption">
              <span className="w-16 shrink-0 text-muted-foreground">
                {entry.created_at ? timeAgo(entry.created_at) : "—"}
              </span>
              <span
                className={cn(
                  "w-16 shrink-0 font-medium",
                  KIND_TONE[entry.kind] ?? "text-muted-foreground",
                )}
              >
                {kindLabel(entry.kind)}
              </span>
              <span className="min-w-0 flex-1 break-words">{entry.body}</span>
              {entry.sha !== "" && (
                <span className="shrink-0 font-mono text-muted-foreground">
                  {entry.sha.slice(0, 12)}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
