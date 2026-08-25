"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, GitBranch } from "lucide-react";
import { copyText } from "@multica/ui/lib/clipboard";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueSessionsOptions } from "@multica/core/worktrees/queries";
import type { IssueSession } from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

/**
 * The sessions that worked on this card, and where each one left off.
 *
 * This is the card's view of the code-progress ledger, not a copy of it. The
 * accounts are written by `multica worktree` and by the hooks that call it, and
 * read here — which is the whole difference from the pinned session comment it
 * replaces. That comment was a per-card copy of a pointer, so it went stale one
 * card at a time and nobody could tell which copy was current.
 *
 * A fixed slot: the heading is always there, empty or not. "No session yet"
 * and "this card has no such thing" are different facts, and hiding the whole
 * section conflated them — a reader could not tell an unclaimed card from one
 * whose section had simply never been built.
 */
export function IssueSessionSidebar({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);

  const { data: sessions = [] } = useQuery(issueSessionsOptions(wsId, issueId));

  return (
    <div>
      <button
        type="button"
        className={`mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.sessions.title)}
        {sessions.length > 0 && (
          <span className="rounded-full bg-muted px-1.5 text-caption tabular-nums text-muted-foreground">
            {sessions.length}
          </span>
        )}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="flex flex-col gap-3 pl-2">
          {sessions.length === 0 ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.sessions.empty)}
            </p>
          ) : (
            sessions.map((session: IssueSession) => (
              <SessionRow
                key={session.worktree_id + session.session_id}
                session={session}
                timeAgo={timeAgo}
              />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function SessionRow({
  session,
  timeAgo,
}: {
  session: IssueSession;
  timeAgo: (iso: string) => string;
}) {
  const { t } = useT("issues");
  const [copied, setCopied] = useState(false);

  const copy = () => {
    void copyText(session.resume).then((ok) => {
      if (!ok) return;
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-baseline gap-x-2 text-caption">
        <span className="font-medium">
          {session.agent === ""
            ? t(($) => $.sessions.no_agent)
            : session.agent}
        </span>
        {/* Waiting on a person is the one state worth interrupting the scan
          for: it is the only one where the card is stopped on the reader. */}
        {session.waiting_for_human === true && (
          <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-medium text-amber-700 dark:text-amber-400">
            {t(($) => $.sessions.waiting)}
          </span>
        )}
        {session.direct !== true && (
          <span className="text-muted-foreground">
            {t(($) => $.sessions.mentioned)}
          </span>
        )}
        {session.updated_at !== null && session.updated_at !== "" && (
          <span className="ml-auto text-muted-foreground">
            {timeAgo(session.updated_at)}
          </span>
        )}
      </div>

      {/* The branch the work was opened on, not the one the checkout happens to
        have now. After a merge the measured branch reads as the base, which
        loses the name a reader needs to find the diff. */}
      {(session.work_branch || session.branch) !== "" && (
        <div className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
          <GitBranch className="!size-3 shrink-0" />
          <span className="truncate">
            {session.work_branch === "" ? session.branch : session.work_branch}
          </span>
        </div>
      )}

      {session.next_action !== "" && (
        <p className="text-caption">→ {session.next_action}</p>
      )}

      {session.resume !== "" && (
        <button
          type="button"
          onClick={copy}
          title={session.resume}
          className="min-w-0 truncate rounded bg-muted px-1.5 py-0.5 text-left font-mono text-caption hover:bg-accent"
        >
          {copied ? t(($) => $.sessions.copied) : session.resume}
        </button>
      )}
    </div>
  );
}
