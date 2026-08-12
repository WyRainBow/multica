"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, FileText, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardDetailOptions, cardListOptions } from "@multica/core/docs/queries";
import { useUpdateCard, useDeleteCard } from "@multica/core/docs/mutations";
import { issueListOptions } from "@multica/core/issues/queries";
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
import { useT, useExactTime } from "../../i18n";
import { allDocPaths } from "../doc-tree";

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

  const { data: doc, isPending, isError } = useQuery(cardDetailOptions(wsId, docId));
  const update = useUpdateCard();
  const remove = useDeleteCard();

  const editorRef = useRef<ContentEditorRef>(null);
  const [outline, setOutline] = useState<OutlineHeading[]>([]);
  const [scrollEl, setScrollEl] = useState<HTMLElement | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Folder suggestions come from what has already been written, the same source
  // the tree derives from — a fixed list would offer folders nobody files
  // anything under. Every level, not just the top: the deep paths are exactly
  // the ones nobody wants to retype.
  const { data: all } = useQuery(cardListOptions(wsId));
  const kindSuggestions = useMemo(
    () => allDocPaths(all?.cards ?? []),
    [all],
  );

  const { data: issues } = useQuery(issueListOptions(wsId));
  const linkedIssue = useMemo(
    () => (doc?.issue_id ? issues?.find((i) => i.id === doc.issue_id) : undefined),
    [doc?.issue_id, issues],
  );

  const jumpToHeading = useCallback((heading: OutlineHeading) => {
    editorRef.current?.scrollToPosition(heading.pos);
  }, []);

  const save = useCallback(
    (patch: { title?: string; content?: string; kind?: string }) => {
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
        <AppLink href={wsPaths.docs()} className="text-caption hover:text-foreground">
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

            {linkedIssue && (
              <AppLink
                href={wsPaths.issueDetail(linkedIssue.id)}
                className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground hover:text-foreground"
              >
                <span className="shrink-0 font-medium">{linkedIssue.identifier}</span>
                <span className="truncate">{linkedIssue.title}</span>
              </AppLink>
            )}
          </div>

          <div className="mt-4">
            <ContentEditor
              ref={editorRef}
              key={doc.id}
              value={doc.content}
              placeholder={t(($) => $.editor.content_placeholder)}
              onUpdate={(md) => save({ content: md })}
              debounceMs={1500}
              // Closing the tab must save what was last typed — without the
              // flush, a paste followed by a quick close loses it.
              flushPendingOnUnmount
              onOutlineChange={setOutline}
            />
          </div>
        </div>
      </div>

      {/* Same outline the issue description carries. A document long enough to
          need its own page is long enough to need a way around it. */}
      <DescriptionOutline
        headings={outline}
        scrollContainer={scrollEl}
        onJump={jumpToHeading}
        className="absolute bottom-0 left-3 top-24 hidden w-44 pb-4 @[81rem]:flex"
      />

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.editor.delete_action)}</AlertDialogTitle>
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
