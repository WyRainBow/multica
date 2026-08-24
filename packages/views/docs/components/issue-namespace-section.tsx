"use client";

import { useQuery } from "@tanstack/react-query";
import { FileText, Folder } from "lucide-react";
import type { IssueNamespaceSlot } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueNamespaceOptions } from "@multica/core/docs/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * The issue's fixed document directory: six named slots, always in the same
 * order, each either written or still waiting.
 *
 * WHY IT IS NOT THE DOCUMENTS LIST BELOW IT. A document appears in that list
 * only once somebody writes it, which makes "there is no design doc" and
 * "nobody has looked at the design yet" the same observation. The directory is
 * created with the issue and every slot is held open by a placeholder card, so
 * the unanswered ones are visible as unanswered instead of absent.
 *
 * Placeholders never reach the list below — the card list, search and the brief
 * all filter them out in SQL — and they must not leak in here either. A slot
 * still on its placeholder renders as 待补 and is deliberately NOT a link: there
 * is nothing written at the other end, and offering a way in would send a
 * reader to read a stub.
 *
 * The slot list, its order and its labels all come off the wire. A second copy
 * of the six names in this file is exactly how the client and the server drift
 * apart, and the labels are the same Chinese the placeholder titles use.
 */
export function IssueNamespaceSection({ issueId }: { issueId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: namespace } = useQuery(issueNamespaceOptions(wsId, issueId));

  // Nothing to draw before the first read lands, and nothing to draw if the
  // response drifted far enough to fail its schema: six invented rows would
  // claim a directory the issue may not have.
  const slots = namespace?.slots ?? [];
  if (slots.length === 0) return null;

  return (
    <section className="mt-6">
      <div className="flex items-center gap-2">
        <h2 className="text-body font-medium">
          {t(($) => $.namespace.title)}
        </h2>
        {namespace?.root && (
          <span className="text-caption text-muted-foreground">
            {namespace.root}
          </span>
        )}
      </div>
      <ul className="mt-2 flex flex-col gap-1">
        {slots.map((slot) => (
          <SlotRow
            key={slot.name}
            slot={slot}
            href={
              // Only a written slot with a card behind it is reachable. A
              // folder's own card is the placeholder, so folders link only
              // through the documents section.
              slot.placeholder !== true && slot.card_id
                ? wsPaths.docDetail(slot.card_id)
                : null
            }
          />
        ))}
      </ul>
    </section>
  );
}

function SlotRow({
  slot,
  href,
}: {
  slot: IssueNamespaceSlot;
  href: string | null;
}) {
  const Icon = slotIcon(slot.type);
  const label = slot.label.trim() || slot.name;

  return (
    <li className="group flex items-center gap-2 rounded-lg border bg-card px-3 py-2 transition-colors hover:border-foreground/20">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      {href ? (
        <AppLink
          href={href}
          className="min-w-0 flex-1 truncate text-body group-hover:underline"
        >
          {label}
        </AppLink>
      ) : (
        /* Not a link on purpose: an unwritten slot has nothing at the other
           end, and a stub is worse than an obvious gap. */
        <span className="min-w-0 flex-1 truncate text-body text-muted-foreground">
          {label}
        </span>
      )}
      {/* The full path, the same way a document row carries it: a row gets
          quoted and screenshotted on its own, and `requirements` without its
          root names nothing. */}
      {slot.kind && (
        <span className="hidden shrink-0 text-caption text-muted-foreground sm:inline">
          {slot.kind}
        </span>
      )}
      <SlotState slot={slot} />
    </li>
  );
}

/**
 * What this slot is, in one badge.
 *
 * Three states, checked in the order a reader asks them: does the slot exist
 * at all, is anything written in it, and how much. `exists === false` happens
 * only on issues created before the directory did — those have no placeholder
 * to be 待补 against, so calling them 待补 would report an omission the writer
 * never had a chance to make.
 */
function SlotState({ slot }: { slot: IssueNamespaceSlot }) {
  const { t } = useT("docs");

  if (slot.exists !== true) {
    return (
      <Badge variant="ghost" className="shrink-0 text-faint-foreground">
        {t(($) => $.namespace.absent)}
      </Badge>
    );
  }
  if (slot.placeholder === true) {
    return (
      <Badge variant="outline" className="shrink-0 text-muted-foreground">
        {t(($) => $.namespace.pending)}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="shrink-0 tabular-nums">
      {t(($) => $.namespace.count, { count: slot.count })}
    </Badge>
  );
}

/**
 * `type` is a server-driven enum, so the switch has a `default`: a shape this
 * build has not heard of still gets a row, drawn as the commoner of the two.
 */
function slotIcon(type: string) {
  switch (type) {
    case "folder":
      return Folder;
    case "document":
      return FileText;
    default:
      return FileText;
  }
}
