"use client";

import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { cardsForIssueOptions } from "@multica/core/docs/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

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
 * No "new document" action either. Documents are written from the Docs tab,
 * where the whole set is in view; offering it here invites a second copy of
 * something already written.
 *
 * Empty renders nothing at all. Most issues need no document, and a permanent
 * "no documents" heading would be noise on all of them.
 */
export function IssueDocsSection({ issueId }: { issueId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: docs = [] } = useQuery(cardsForIssueOptions(wsId, issueId));

  if (docs.length === 0) return null;

  return (
    <section className="mt-6">
      <div className="flex items-center gap-2">
        <h2 className="text-body font-medium">{t(($) => $.issue_section.title)}</h2>
        <span className="text-caption text-muted-foreground">{docs.length}</span>
      </div>

      <ul className="mt-2 flex flex-col gap-1">
        {docs.map((doc) => (
          <li key={doc.id}>
            <AppLink
              href={wsPaths.docDetail(doc.id)}
              className="group flex items-center gap-2 rounded-lg border bg-card px-3 py-2 transition-colors hover:border-foreground/20"
            >
              <FileText className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate text-body group-hover:underline">
                {doc.title.trim() || t(($) => $.doc.untitled)}
              </span>
              {doc.kind && (
                <span className="shrink-0 text-caption text-muted-foreground">{doc.kind}</span>
              )}
              {/* How much there is to read, the same number the document's own
                  row carries — it is what decides whether to open it now. */}
              <span className="shrink-0 text-caption tabular-nums text-faint-foreground">
                {t(($) => $.doc.length, { count: doc.content.length })}
              </span>
            </AppLink>
          </li>
        ))}
      </ul>
    </section>
  );
}
