"use client";

import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { copyText } from "@multica/ui/lib/clipboard";
import type { WorktreeSession } from "@multica/core/types";
import { useT, useTimeAgo } from "../i18n";

/**
 * The navigation account of a worktree, read: who is driving this checkout,
 * what they are waiting on, and the command that puts you back in their
 * session.
 *
 * Shared by the ledger and the issue page because it is the same question
 * asked from two ends — "who has this tree" and "who has my card" — and a
 * second copy would be the per-card duplication that made pinned session
 * comments go stale one card at a time.
 *
 * The date is part of it. Sync re-measures a merge claim every run, so the
 * facts account can be trusted without one; nothing re-checks a resume
 * pointer, and the session behind it may be long gone.
 */
export function SessionPointer({
  session,
  onEdit,
}: {
  session: WorktreeSession;
  onEdit?: () => void;
}) {
  const { t } = useT("openwiki");
  const timeAgo = useTimeAgo();
  const [copied, setCopied] = useState(false);

  const copy = () => {
    void copyText(session.resume).then((ok) => {
      if (!ok) return;
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    });
  };

  const summary = (
    <span className="flex flex-wrap items-baseline gap-x-2 text-caption">
      <span className="font-medium">
        {session.agent || t(($) => $.session_no_agent)}
      </span>
      {session.owner !== "" && (
        <span className="text-muted-foreground">{session.owner}</span>
      )}
      {session.next_action !== "" && <span>→ {session.next_action}</span>}
      {session.updated_at !== null && (
        <span className="ml-auto text-muted-foreground">
          {t(($) => $.session_stated, { when: timeAgo(session.updated_at) })}
        </span>
      )}
    </span>
  );

  return (
    <div className="flex flex-col gap-1">
      {onEdit ? (
        <button type="button" className="w-full text-left" onClick={onEdit}>
          {summary}
        </button>
      ) : (
        summary
      )}

      {session.resume !== "" && (
        <div className="flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate rounded bg-muted px-1.5 py-0.5 font-mono text-caption">
            {session.resume}
          </code>
          <Button variant="ghost" size="sm" onClick={copy}>
            {copied ? t(($) => $.session_copied) : t(($) => $.session_copy)}
          </Button>
        </div>
      )}
    </div>
  );
}
