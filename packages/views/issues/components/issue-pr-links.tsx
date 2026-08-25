"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useWorkspaceId } from "@multica/core/hooks";
import { issuePRLinksOptions } from "@multica/core/worktrees/queries";
import {
  useCreateIssuePRLink,
  useDeleteIssuePRLink,
} from "@multica/core/worktrees/mutations";
import type { IssuePRLink } from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Review requests recorded by hand.
 *
 * This workspace talks to GitHub and GitLab and integrates with neither, so
 * nothing here is fetched: no state badge, no title from the page, no check
 * results. A status this build cannot verify would be a claim nobody could
 * check, sitting next to a list where the states are real.
 *
 * What each row does carry is who recorded it. A bare URL says a review exists
 * somewhere; a URL with a name says who to ask about it.
 */
export function IssuePRLinks({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const [draft, setDraft] = useState("");

  const { data: links = [] } = useQuery(issuePRLinksOptions(wsId, issueId));
  const create = useCreateIssuePRLink(issueId);
  const remove = useDeleteIssuePRLink(issueId);

  const submit = () => {
    const url = draft.trim();
    if (url === "" || create.isPending) return;
    create.mutate({ url }, { onSuccess: () => setDraft("") });
  };

  return (
    <div className="flex flex-col gap-2">
      {links.map((link: IssuePRLink) => (
        <div key={link.id} className="group flex items-start gap-1.5 text-caption">
          <ExternalLink className="mt-0.5 !size-3 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <a
              href={link.url}
              target="_blank"
              rel="noreferrer noopener"
              className="block truncate underline-offset-2 hover:underline"
              title={link.url}
            >
              {link.title === "" ? link.url : link.title}
            </a>
            {/* Who and when, on its own line: it is the part that makes the
              link actionable, and it must not push the URL out of view. */}
            <p className="text-caption text-muted-foreground">
              {link.added_by === ""
                ? t(($) => $.pr_links.added_unknown)
                : t(($) => $.pr_links.added_by, { who: link.added_by })}
              {link.added_at !== "" && <> · {timeAgo(link.added_at)}</>}
            </p>
          </div>
          <button
            type="button"
            onClick={() => remove.mutate(link.id)}
            aria-label={t(($) => $.pr_links.remove)}
            className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover:opacity-100"
          >
            <X className="!size-3" />
          </button>
        </div>
      ))}

      <div className="flex items-center gap-1.5">
        <Input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submit();
            }
          }}
          placeholder={t(($) => $.pr_links.placeholder)}
          className="h-7 text-caption"
        />
        <Button
          size="sm"
          variant="secondary"
          className="h-7 shrink-0"
          disabled={draft.trim() === "" || create.isPending}
          onClick={submit}
        >
          {t(($) => $.pr_links.add)}
        </Button>
      </div>

      {create.isError && (
        <p className="text-caption text-destructive">
          {t(($) => $.pr_links.add_failed)}
        </p>
      )}
      {links.length === 0 && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.pr_links.empty)}
        </p>
      )}
    </div>
  );
}
