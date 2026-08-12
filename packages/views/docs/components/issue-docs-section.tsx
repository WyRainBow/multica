"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileText, Plus, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { cardsForIssueOptions } from "@multica/core/docs/queries";
import { useUpdateCard } from "@multica/core/docs/mutations";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { DocPickerModal } from "./doc-picker-modal";
import { docLength } from "../doc-tree";

/**
 * The documents this issue needs, listed on the issue.
 *
 * The other half of a link that already existed in one direction: a document
 * has always been able to name its issue, and until now the issue could not
 * see back. A pointer that only works one way is a pointer you have to
 * remember, which is the thing having a document store was supposed to fix.
 *
 * TITLES ONLY, not bodies. An earlier version of this section rendered each
 * document in full, which turned the issue page into a place documents are
 * read — and then the same text lived in two screens with no way to tell which
 * one anyone had looked at. The document's own page is where it is read; this
 * says which ones exist and gets you there.
 *
 * Attaching works from BOTH ends — here and on the document — because which
 * end you are standing on is not something the app gets to decide. Reading an
 * issue and remembering the SOP that belongs to it is as common as writing the
 * SOP and knowing which issue it serves.
 *
 * Still no "new document" action. Documents are written from the Docs tab,
 * where the whole set is in view; offering it here invites a second copy of
 * something already written.
 *
 * It renders even with nothing in it, matching the resources section directly
 * above. Hiding it when empty was the original choice — a permanent "no
 * documents" line looked like noise — but an invisible section is
 * indistinguishable from a missing feature, and this one was invisible on every
 * issue for as long as the app had no way to create the link at all.
 */
export function IssueDocsSection({ issueId }: { issueId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: docs = [] } = useQuery(cardsForIssueOptions(wsId, issueId));
  const update = useUpdateCard();
  const [picking, setPicking] = useState(false);

  // issue_id null detaches; the document itself is untouched. Detaching is not
  // deleting, so there is no confirm — the row can be put back in two clicks.
  const link = (docId: string, next: string | null) =>
    update.mutate(
      { id: docId, issue_id: next },
      { onError: () => toast.error(t(($) => $.issue_section.link_failed)) },
    );

  return (
    <section className="mt-6">
      <div className="flex items-center gap-2">
        <h2 className="text-body font-medium">
          {t(($) => $.issue_section.title)}
        </h2>
        {docs.length > 0 && (
          <span className="text-caption text-muted-foreground">
            {docs.length}
          </span>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => setPicking(true)}
        >
          <Plus className="size-3.5" />
          {t(($) => $.issue_section.link)}
        </Button>
      </div>

      {docs.length === 0 ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.issue_section.empty)}
        </p>
      ) : (
        <ul className="mt-2 flex flex-col gap-1">
          {/* The row is the container and the link only wraps the text: an
              unlink button nested inside an anchor is invalid HTML, and
              clicking it would navigate as well as detach. */}
          {docs.map((doc) => (
            <li
              key={doc.id}
              className="group flex items-center gap-2 rounded-lg border bg-card px-3 py-2 transition-colors hover:border-foreground/20"
            >
              <FileText className="size-3.5 shrink-0 text-muted-foreground" />
              <AppLink
                href={wsPaths.docDetail(doc.id)}
                className="min-w-0 flex-1 truncate text-body group-hover:underline"
              >
                {doc.title.trim() || t(($) => $.doc.untitled)}
              </AppLink>
              {doc.kind && (
                <span className="shrink-0 text-caption text-muted-foreground">
                  {doc.kind}
                </span>
              )}
              {/* How much there is to read, the same number the document's own
                  row carries — it is what decides whether to open it now. */}
              <span className="shrink-0 text-caption tabular-nums text-faint-foreground">
                {t(($) => $.doc.length, { count: docLength(doc.content) })}
              </span>
              <Button
                size="icon-sm"
                variant="ghost"
                className="size-5 shrink-0 text-muted-foreground hover:text-destructive"
                onClick={() => link(doc.id, null)}
                aria-label={t(($) => $.issue_section.unlink)}
              >
                <X className="size-3" />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <DocPickerModal
        open={picking}
        onOpenChange={setPicking}
        title={t(($) => $.issue_section.link)}
        description={t(($) => $.issue_section.link_hint)}
        excludeIds={docs.map((d) => d.id)}
        onSelect={(doc) => link(doc.id, issueId)}
      />
    </section>
  );
}
