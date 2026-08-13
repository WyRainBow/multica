"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, FileText, Link2, Loader2, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardDetailOptions, cardListOptions } from "@multica/core/docs/queries";
import { useUpdateCard, useDeleteCard } from "@multica/core/docs/mutations";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import type { OutlineHeading } from "../../editor/outline";
import { DescriptionOutline } from "../../issues/components/description-outline";
import { AppLink, useNavigation } from "../../navigation";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useT, useExactTime } from "../../i18n";
import { allDocPaths, docLength } from "../doc-tree";

/**
 * One document, on a page of its own.
 *
 * Until now a document could only be opened in a modal, which is what kept the
 * long ones in Feishu: the whole point of this store is the writing an issue
 * needs while it is being worked, and the longest of those runs past eleven
 * thousand characters. A dialog is a promise that what you are editing is
 * small.
 *
 * Saves the way the issue description does — on a debounce, with no save
 * button. A document that has to be explicitly saved is a document someone
 * eventually loses.
 */
export function DocDetail({ docId }: { docId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();
  const exactTime = useExactTime();

  const {
    data: doc,
    isPending,
    isError,
  } = useQuery(cardDetailOptions(wsId, docId));
  const update = useUpdateCard();
  const remove = useDeleteCard();

  const editorRef = useRef<ContentEditorRef>(null);
  const [outline, setOutline] = useState<OutlineHeading[]>([]);
  const [scrollEl, setScrollEl] = useState<HTMLElement | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [pickingIssue, setPickingIssue] = useState(false);
  // Seeded from what was saved and refreshed on the editor's debounced update,
  // so it is right on open and settles a moment after typing stops. Counting
  // per keystroke would mean serializing the whole document on every key.
  const [length, setLength] = useState(0);

  // Folder suggestions come from what has already been written, the same source
  // the tree derives from — a fixed list would offer folders nobody files
  // anything under. Every level, not just the top: the deep paths are exactly
  // the ones nobody wants to retype.
  const { data: all } = useQuery(cardListOptions(wsId));
  const kindSuggestions = useMemo(() => allDocPaths(all?.cards ?? []), [all]);

  // Fetched by id, not looked up in the issue list. The list is paginated and
  // excludes archived issues, so a link to either would have silently rendered
  // nothing — and a link you cannot see is a link you will not trust.
  const { data: linkedIssue } = useQuery({
    ...issueDetailOptions(wsId, doc?.issue_id ?? ""),
    enabled: Boolean(doc?.issue_id),
  });

  useEffect(() => {
    setLength(docLength(doc?.content ?? ""));
  }, [doc?.content]);

  const save = useCallback(
    // issue_id: an explicit null detaches; omitting it leaves the link alone.
    (patch: {
      title?: string;
      content?: string;
      kind?: string;
      issue_id?: string | null;
    }) => {
      if (!doc) return;
      update.mutate(
        { id: doc.id, ...patch },
        {
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.editor.save_failed),
            ),
        },
      );
    },
    [doc, update, t],
  );

  if (isPending) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (isError || !doc) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground">
        <FileText className="size-10 text-faint-foreground" />
        <p className="text-body">{t(($) => $.detail.not_found)}</p>
        <AppLink
          href={wsPaths.docs()}
          className="text-caption hover:text-foreground"
        >
          {t(($) => $.detail.back)}
        </AppLink>
      </div>
    );
  }

  return (
    <div className="relative flex flex-1 flex-col overflow-hidden">
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <Button
          size="icon-sm"
          variant="ghost"
          onClick={() => navigation.push(wsPaths.docs())}
          aria-label={t(($) => $.detail.back)}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <FileText className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate text-body font-medium">
          {doc.title.trim() || t(($) => $.doc.untitled)}
        </span>
        <span className="ml-auto shrink-0 text-caption text-muted-foreground">
          {t(($) => $.detail.updated_at, { time: exactTime(doc.updated_at) })}
        </span>
        <Button
          size="icon-sm"
          variant="ghost"
          className="text-muted-foreground hover:text-destructive"
          onClick={() => setConfirmDelete(true)}
          aria-label={t(($) => $.editor.delete_action)}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      <div ref={setScrollEl} className="flex-1 overflow-y-auto">
        {/* Centred, unlike the list. The two are different acts: the list is
            scanned, so it belongs where the eye already is — hard left, under
            the header. This page is READ start to finish, and a column pinned
            to one edge of a wide window leaves the text hanging off the side
            with nothing balancing it. */}
        <div className="mx-auto w-full max-w-3xl px-6 py-6">
          <Input
            defaultValue={doc.title}
            placeholder={t(($) => $.editor.title_placeholder)}
            onBlur={(e) => {
              const next = e.target.value.trim();
              if (next !== doc.title) save({ title: next });
            }}
            className="!border-0 !bg-transparent px-0 !text-display-sm font-bold shadow-none focus-visible:ring-0"
          />

          <div className="mt-2 flex items-center gap-3">
            <Input
              defaultValue={doc.kind}
              placeholder={t(($) => $.editor.kind_placeholder)}
              list="doc-kind-suggestions"
              onBlur={(e) => {
                const next = e.target.value.trim();
                if (next !== doc.kind) save({ kind: next });
              }}
              className="h-7 w-40 text-caption"
            />
            <datalist id="doc-kind-suggestions">
              {kindSuggestions.map((k) => (
                <option key={k} value={k} />
              ))}
            </datalist>

            {/* The link was display-only until now: a document could name its
                issue, but only through the CLI. So the issue page's document
                section had nothing to list for anyone working in the app, and
                it renders nothing when empty — the feature read as missing
                rather than unreachable. */}
            {doc.issue_id ? (
              <span className="flex min-w-0 items-center gap-1">
                <AppLink
                  href={wsPaths.issueDetail(doc.issue_id)}
                  className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground hover:text-foreground"
                >
                  <Link2 className="size-3.5 shrink-0" />
                  {linkedIssue ? (
                    <>
                      <span className="shrink-0 font-medium">
                        {linkedIssue.identifier}
                      </span>
                      <span className="truncate">{linkedIssue.title}</span>
                    </>
                  ) : (
                    // The id is stored; the issue row may still be loading.
                    // Showing nothing here would look like no link at all.
                    <span className="truncate">
                      {t(($) => $.detail.issue_loading)}
                    </span>
                  )}
                </AppLink>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  className="size-5 shrink-0 text-muted-foreground hover:text-destructive"
                  onClick={() => save({ issue_id: null })}
                  aria-label={t(($) => $.detail.unlink_issue)}
                >
                  <X className="size-3" />
                </Button>
              </span>
            ) : (
              <Button
                size="sm"
                variant="ghost"
                className="h-7 gap-1.5 px-2 text-caption text-muted-foreground"
                onClick={() => setPickingIssue(true)}
              >
                <Link2 className="size-3.5" />
                {t(($) => $.detail.link_issue)}
              </Button>
            )}
          </div>

          <div className="mt-4">
            <ContentEditor
              ref={editorRef}
              key={doc.id}
              value={doc.content}
              placeholder={t(($) => $.editor.content_placeholder)}
              onUpdate={(md) => {
                setLength(docLength(md));
                save({ content: md });
              }}
              debounceMs={1500}
              // Closing the tab must save what was last typed — without the
              // flush, a paste followed by a quick close loses it.
              flushPendingOnUnmount
              onOutlineChange={setOutline}
            />
          </div>

          {/* How long this document is, the same count the list rows and the
              issue page show — and the same one an agent reads, so it answers
              "how much am I about to hand over". Below the body rather than in
              the header: it is a fact about the text, and it settles a moment
              after typing stops rather than on every keystroke. */}
          {length > 0 && (
            <div className="mt-2 flex justify-end text-caption tabular-nums text-faint-foreground">
              {t(($) => $.doc.length, { count: length })}
            </div>
          )}
        </div>
      </div>

      {/* Same outline the issue description carries. A document long enough to
          need its own page is long enough to need a way around it. */}
      <DescriptionOutline
        headings={outline}
        scrollContainer={scrollEl}
        className="absolute bottom-0 left-3 top-24 hidden w-44 pb-4 @[81rem]:flex"
      />

      {/* The same picker that sets a parent issue and adds a child — it
          searches server-side, so it finds issues this page never loaded. */}
      <IssuePickerModal
        open={pickingIssue}
        onOpenChange={setPickingIssue}
        title={t(($) => $.detail.link_issue)}
        description={t(($) => $.detail.link_issue_hint)}
        excludeIds={[]}
        onSelect={(issue) => {
          save({ issue_id: issue.id });
          setPickingIssue(false);
        }}
      />

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.editor.delete_action)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.editor.delete_confirm)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.editor.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() =>
                remove.mutate(doc.id, {
                  onSuccess: () => navigation.push(wsPaths.docs()),
                  onError: () => toast.error(t(($) => $.editor.delete_failed)),
                })
              }
            >
              {t(($) => $.editor.delete_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
