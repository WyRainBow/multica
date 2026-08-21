"use client";

import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardDetailOptions, cardListOptions } from "@multica/core/docs/queries";
import { AppLink } from "../../navigation";

const BASE_CLASS =
  "doc-mention inline-flex items-baseline gap-1 rounded bg-muted px-1 align-baseline text-inherit";

/**
 * A wiki page referenced from prose, rendered as its title.
 *
 * A page carries a UUID and a free-text title and nothing else — there is no
 * `MUL-123` shorthand to type — so a reference to one was a bare UUID sitting
 * in the text, unreadable and unclickable. Same shape as a project mention,
 * and for the same reason.
 *
 * The list answers first and the detail query only runs when it does not: the
 * list is already loaded on any page that shows documents, and a fetch per
 * mention would make a page full of references a page full of requests.
 *
 * An id that resolves to nothing keeps the label it had. A reference to a
 * deleted page should read as a dead reference, not vanish.
 */
export function DocMentionChip({
  docId,
  fallbackLabel,
}: {
  docId: string;
  fallbackLabel?: string;
}) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();

  const { data: listResponse } = useQuery(cardListOptions(wsId));
  const listed = listResponse?.cards?.find((card) => card.id === docId);

  const { data: detail } = useQuery({
    ...cardDetailOptions(wsId, docId),
    enabled: Boolean(wsId) && !listed,
  });

  const doc = listed ?? detail;

  if (!doc) {
    return (
      <span className={BASE_CLASS}>
        <FileText className="size-3 self-center text-muted-foreground" />
        {fallbackLabel ?? docId}
      </span>
    );
  }

  return (
    <AppLink
      href={paths.docDetail(doc.id)}
      newTabTitle={doc.title}
      className={BASE_CLASS}
    >
      <FileText className="size-3 self-center text-muted-foreground" />
      {doc.title}
    </AppLink>
  );
}
