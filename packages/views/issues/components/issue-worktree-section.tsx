"use client";

import { useQuery } from "@tanstack/react-query";
import { GitBranch } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { worktreeListOptions } from "@multica/core/worktrees/queries";
import type { Issue, Worktree } from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";
import { SessionPointer } from "../../worktrees/session-pointer";

/** Written by `multica worktree log --issue`, or by hand. Holds a tree name. */
const WORKTREE_KEY = "git.worktree";

/**
 * Where this card's code is being written, and who to talk to about it.
 *
 * This is the half of the ledger that used to be a pinned comment on every
 * card: a resume pointer and a next action, retyped per card and going stale
 * per card. One tree can carry many cards, so the pointer lives on the tree and
 * the card names the tree — one place to update, and it is the place the person
 * driving the tree already updates.
 *
 * Nothing is rendered for a card with no tree. Most cards never get one, and a
 * heading over an empty state on all of them would be noise on the page that
 * matters most.
 */
export function IssueWorktreeSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();

  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: trees = [] } = useQuery(worktreeListOptions(wsId));

  const meta = ((issue as Issue | undefined)?.metadata ?? {}) as Record<string, unknown>;
  const raw = meta[WORKTREE_KEY];
  const name = typeof raw === "string" ? raw.trim() : "";
  if (name === "") return null;

  const tree = trees.find((candidate: Worktree) => candidate.name === name);
  // The card names a tree the ledger does not have: say so rather than render
  // nothing. A dangling pointer is worth seeing — it usually means the tree was
  // removed while the card still pointed at it.
  if (!tree) {
    return (
      <section className="mt-6">
        <Heading />
        <p className="mt-1 text-caption text-muted-foreground">
          {t(($) => $.worktree.missing, { name })}
        </p>
      </section>
    );
  }

  return (
    <section className="mt-6">
      <Heading />
      <div className="mt-1 rounded-lg border">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-2">
          <span className="font-medium text-body">{tree.name}</span>
          <span className="text-caption text-muted-foreground">{tree.status}</span>
          <span className="truncate font-mono text-caption text-muted-foreground">
            {tree.branch || "—"}
            {tree.base_ref !== "" && ` ← ${tree.base_ref}`}
          </span>
          {tree.merged_sha !== "" && (
            <span className="font-mono text-caption text-muted-foreground">
              {tree.merged_sha.slice(0, 12)}
              {tree.merged_into !== "" && ` → ${tree.merged_into}`}
            </span>
          )}
          <span className="ml-auto text-caption text-muted-foreground">
            {tree.verified_at
              ? t(($) => $.worktree.measured, { when: timeAgo(tree.verified_at as string) })
              : t(($) => $.worktree.never_measured)}
          </span>
        </div>
        {(tree.session.agent !== "" ||
          tree.session.resume !== "" ||
          tree.session.next_action !== "") && (
          <div className="border-t px-3 py-2">
            <SessionPointer session={tree.session} />
          </div>
        )}
      </div>
    </section>
  );
}

function Heading() {
  const { t } = useT("issues");
  return (
    <h3 className="flex items-center gap-1.5 text-caption font-medium">
      <GitBranch className="size-3.5 text-muted-foreground" />
      {t(($) => $.worktree.title)}
    </h3>
  );
}
