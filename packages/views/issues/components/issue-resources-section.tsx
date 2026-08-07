"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Copy, ExternalLink, Link2, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueResourcesOptions } from "@multica/core/issue-resources/queries";
import { useDeleteIssueResource } from "@multica/core/issue-resources/mutations";
import type { IssueResource } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT, useTimeAgo } from "../../i18n";
import { IssueResourceDialog } from "./issue-resource-dialog";
import { resourceHost, resourceLabel } from "./resource-label";
import feishuLogo from "./feishu-logo.png";

/**
 * Pages that live outside Multica but belong next to this issue: a design doc,
 * a meeting note, a vendor page.
 *
 * A section rather than links in the description. A link written into the body
 * cannot be listed, counted or removed without editing prose — and once the
 * issue is finished the body is frozen, so it could not be added at all.
 *
 * Its own section rather than folded into the cards section below it: a card
 * row is a preview card with a body teaser, and a resource row has no body.
 * Merging them would either put two row shapes in one list or flatten cards
 * into bare titles, which is a loss to a feature that already works.
 */
export function IssueResourcesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const [editing, setEditing] = useState<IssueResource | null>(null);
  const [adding, setAdding] = useState(false);

  const { data: resources = [] } = useQuery(issueResourcesOptions(wsId, issueId));
  const remove = useDeleteIssueResource(issueId);

  return (
    <section className="mt-6">
      <div className="flex items-center gap-2">
        <h2 className="text-body font-medium">{t(($) => $.resources.title)}</h2>
        {resources.length > 0 && (
          <span className="text-caption text-muted-foreground">{resources.length}</span>
        )}
        <Button size="sm" variant="ghost" className="ml-auto" onClick={() => setAdding(true)}>
          <Plus className="size-3.5" />
          {t(($) => $.resources.add)}
        </Button>
      </div>

      {resources.length === 0 ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.resources.empty)}
        </p>
      ) : (
        <ul className="mt-2 flex flex-col gap-1">
          {resources.map((resource) => (
            <ResourceRow
              key={resource.id}
              resource={resource}
              timeAgo={timeAgo}
              onRename={() => setEditing(resource)}
              onRemove={() => remove.mutate(resource.id)}
            />
          ))}
        </ul>
      )}

      {(adding || editing) && (
        <IssueResourceDialog
          issueId={issueId}
          resource={editing}
          onClose={() => {
            setAdding(false);
            setEditing(null);
          }}
        />
      )}
    </section>
  );
}

// Feishu — official mark, shipped as a PNG next to this file. Bundlers hand an
// import back as a string or as { src }, depending on the app; same shim
// provider-logo.tsx uses.
function staticAssetSrc(asset: string | { src: string }): string {
  return typeof asset === "string" ? asset : asset.src;
}
const feishuLogoSrc = staticAssetSrc(feishuLogo);

/**
 * A brand mark where we have one, the site's own favicon otherwise, and the
 * generic link glyph behind both.
 *
 * The bundled mark wins for Feishu because a self-hosted tenant
 * (hellotalk.feishu.cn) serves its own favicon, which may be a company logo or
 * nothing at all — the product is still Feishu, and the row should say so.
 * Matched by suffix for exactly that reason: the host is a tenant subdomain,
 * not feishu.cn itself.
 *
 * Fetched straight from the host rather than through a favicon service: the
 * browser is asking the same site the row points at, so nothing about which
 * documents someone attaches leaks to a third party — and on an internal host
 * the request rides the session that is already there.
 *
 * A favicon needs no authorisation even when the page behind it does, which is
 * why an icon is fetchable where a title is not: a Feishu doc returns a login
 * page to an anonymous reader, but /favicon.ico is served to anyone.
 *
 * onError, not a probe: there is no way to ask whether a host has a favicon
 * without requesting it, so the fallback is the failure path. The generic icon
 * sits underneath the whole time, so a slow or missing favicon shows a link
 * glyph rather than an empty gap that reflows the row.
 */
function ResourceIcon({ host }: { host: string }) {
  const [failed, setFailed] = useState(false);
  if (host.endsWith("feishu.cn") || host.endsWith("larksuite.com")) {
    return (
      <img
        src={feishuLogoSrc}
        alt=""
        aria-hidden
        className="size-3.5 shrink-0 rounded-[2px] object-contain"
      />
    );
  }
  if (!host || failed) {
    return <Link2 className="size-3.5 shrink-0 text-muted-foreground" />;
  }
  return (
    <span className="relative size-3.5 shrink-0">
      <Link2 className="absolute inset-0 size-3.5 text-muted-foreground" />
      <img
        src={`https://${host}/favicon.ico`}
        alt=""
        aria-hidden
        loading="lazy"
        onError={() => setFailed(true)}
        className="absolute inset-0 size-3.5 rounded-[2px] object-contain"
      />
    </span>
  );
}

function ResourceRow({
  resource,
  timeAgo,
  onRename,
  onRemove,
}: {
  resource: IssueResource;
  timeAgo: (value: string) => string;
  onRename: () => void;
  onRemove: () => void;
}) {
  const { t } = useT("issues");
  const host = resourceHost(resource.url);

  return (
    <li className="group flex items-center gap-2 rounded-lg border bg-card px-3 py-2 transition-colors hover:border-foreground/20">
      <ResourceIcon host={host} />
      {/* rel: a resource is a URL someone pasted, so the target page is not
          trusted with a handle on this tab. */}
      <a
        href={resource.url}
        target="_blank"
        rel="noopener noreferrer"
        className="min-w-0 flex-1 truncate text-body hover:underline"
        title={resource.url}
      >
        {resourceLabel(resource)}
      </a>
      {host && (
        <span className="hidden shrink-0 text-caption text-muted-foreground sm:inline">
          {host}
        </span>
      )}
      <span className="shrink-0 text-caption text-muted-foreground">
        {timeAgo(resource.created_at)}
      </span>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              size="icon"
              variant="ghost"
              // Revealed on hover, but never hidden from the keyboard: an
              // opacity-only reveal leaves it focusable, which is what a
              // display:none reveal would break.
              className="size-6 shrink-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
            >
              <MoreHorizontal className="size-3.5" />
            </Button>
          }
        />
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={() => {
              void copyText(resource.url).then((ok) => {
                if (ok) toast.success(t(($) => $.resources.copied));
              });
            }}
          >
            <Copy className="size-3.5" />
            {t(($) => $.resources.copy)}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => window.open(resource.url, "_blank", "noopener,noreferrer")}
          >
            <ExternalLink className="size-3.5" />
            {t(($) => $.resources.open)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onRename}>
            <Pencil className="size-3.5" />
            {t(($) => $.resources.rename)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onRemove} variant="destructive">
            <Trash2 className="size-3.5" />
            {t(($) => $.resources.remove)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  );
}
