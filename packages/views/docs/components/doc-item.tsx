"use client";

import { useRef, useState } from "react";
import { Pencil, TextCursorInput, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { Issue, Card } from "@multica/core/types";
import {
  useDeleteCard,
  useUpdateCard,
} from "@multica/core/docs/mutations";
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
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { RichContent } from "../../rich-content";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT } from "../../i18n";
import { docLength } from "../doc-tree";

/**
 * One card, as a row in the timeline.
 *
 * Full width and roomy rather than a tile in a grid: a card is prose, and
 * prose in a narrow column is read a third of a line at a time. Six lines of
 * body before the clamp — enough to tell whether this is the note you were
 * looking for without opening it.
 *
 * No timestamp here: the timeline rail to the left already carries it, and a
 * second copy inside the card would be the same fact twice.
 *
 * Clicking anywhere but the requirement chip opens the editor — the common
 * action on a note is "read the rest / fix a line", not "go somewhere else".
 * Rename and delete live here too (hover actions): walking into the detail
 * page to reach them is the detour this row's own affordances remove. The
 * server still has the last word — a frozen round doc answers 409 and the
 * toast says so rather than pretending it worked.
 */
/**
 * How much of a document's body the list shows before cutting it off.
 *
 * A cap in pixels rather than lines because the body is rendered Markdown —
 * headings, lists and code blocks all have different heights, and a line count
 * would cut each row at a different physical depth.
 *
 * A whole multiple of the body line-height (20px), so a cut through plain
 * prose lands BETWEEN lines. It cannot always: a heading or a list above the
 * cut shifts everything below it off the grid, which is what the fade is for.
 */
const DOC_PREVIEW_MAX_HEIGHT = 160;

export function DocItem({
  card,
  issue,
  issueGone = false,
  onEdit,
  onOpenIssue,
}: {
  card: Card;
  /** Resolved requirement, when the card points at one that still exists. */
  issue?: Issue;
  /** The linked issue was looked up and is genuinely gone — not merely absent
   *  from the page of issues this view happened to load. */
  issueGone?: boolean;
  onEdit: () => void;
  onOpenIssue: (identifier: string) => void;
}) {
  const { t } = useT("docs");
  const title = card.title.trim();
  const body = card.content.trim();

  // Inline rename: the title swaps to an input seeded with the current
  // value. Enter or blur commits, Escape drops the edit. Unmounting on a
  // successful update is what ends the edit — the row re-renders with the
  // new title from the invalidated query.
  const [renaming, setRenaming] = useState(false);
  const [draftTitle, setDraftTitle] = useState(title);
  const renameInputRef = useRef<HTMLInputElement>(null);
  const update = useUpdateCard();

  const [confirmDelete, setConfirmDelete] = useState(false);
  const remove = useDeleteCard();

  const commitRename = () => {
    const next = draftTitle.trim();
    setRenaming(false);
    if (next === title || next === "") return;
    update.mutate(
      { id: card.id, title: next },
      {
        onError: (err) =>
          toast.error(t(($) => $.doc.rename_failed), {
            description: err instanceof Error ? err.message : undefined,
          }),
      },
    );
  };

  return (
    <div className="group rounded-lg border bg-card px-5 py-4 transition-colors hover:border-foreground/20">
      <div className="flex items-start gap-3">
        {renaming ? (
          <Input
            ref={renameInputRef}
            autoFocus
            value={draftTitle}
            onChange={(e) => setDraftTitle(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename();
              if (e.key === "Escape") setRenaming(false);
            }}
            className="h-7 flex-1 text-title-sm font-semibold"
            aria-label={t(($) => $.doc.rename_input)}
          />
        ) : (
          <button
            type="button"
            onClick={onEdit}
            className="min-w-0 flex-1 text-left"
          >
            <h3 className="text-title-sm font-semibold leading-snug group-hover:underline">
              {title || t(($) => $.doc.untitled)}
            </h3>
          </button>
        )}
        {!renaming && (
          <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
            <Button
              size="icon-xs"
              variant="ghost"
              onClick={() => {
                setDraftTitle(title);
                setRenaming(true);
              }}
              aria-label={t(($) => $.doc.rename)}
            >
              <TextCursorInput className="size-3.5" />
            </Button>
            <Button
              size="icon-xs"
              variant="ghost"
              onClick={() => setConfirmDelete(true)}
              aria-label={t(($) => $.editor.delete_action)}
            >
              <Trash2 className="size-3.5" />
            </Button>
            <Button
              size="icon-xs"
              variant="ghost"
              onClick={onEdit}
              aria-label={t(($) => $.doc.edit)}
            >
              <Pencil className="size-3.5" />
            </Button>
          </div>
        )}
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.editor.delete_action)} ·{" "}
              {title || t(($) => $.doc.untitled)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.editor.delete_confirm)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.editor.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                remove.mutate(card.id, {
                  onError: (err) =>
                    toast.error(t(($) => $.editor.delete_failed), {
                      description:
                        err instanceof Error ? err.message : undefined,
                    }),
                })
              }
            >
              {t(($) => $.editor.delete_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {body && (
        // Rendered Markdown, not the raw source. The editor writes Markdown, so
        // showing the source here means every bold run and every link reads as
        // punctuation — `**待确认**` and `[url](url)` on screen.
        //
        // Preview density, not document density. At document density an h2 in
        // the body renders at 18px while this row's own title is 16px, so any
        // document whose body has a heading showed that heading LARGER than the
        // document's name — the hierarchy inverted on the row. Preview caps
        // every heading at body size and separates them by weight instead.
        //
        // Height-capped rather than `line-clamp`: clamping counts lines inside
        // ONE block, so a row whose body is several paragraphs would show six
        // lines of the first one and then the rest in full. A max-height plus a
        // fade cuts the whole thing at the same place regardless of structure.
        <button
          type="button"
          onClick={onEdit}
          className="relative mt-2 block w-full overflow-hidden text-left"
          style={{ maxHeight: DOC_PREVIEW_MAX_HEIGHT }}
        >
          <RichContent content={body} density="preview" phase="settled" />
          {/* Tall enough to swallow a whole line. At 8px it only covered the
              descenders, so a cut through the middle of a line left the tops of
              the characters legible — which reads as a rendering fault rather
              than as "there is more below". */}
          <span
            aria-hidden
            className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-card via-card/80 to-transparent"
          />
        </button>
      )}

      {/* Length, next to the requirement chip. The body above is cut at a fixed
          height, so this is the only thing that says how much is BELOW the cut
          — a 300-character note and an 11674-character SOP are the same six
          lines on screen otherwise. Same count the issue page's document list
          shows, and the same one an agent reads. */}
      <div className="mt-3 flex items-center gap-2 text-caption text-muted-foreground">
        {issue ? (
          <button
            type="button"
            onClick={() => onOpenIssue(issue.identifier)}
            className="flex shrink-0 items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-medium tabular-nums transition-colors hover:text-foreground"
          >
            {/* The issue's state, not just its key. A document is usually
                written while the work is live and read long after it finished;
                whether that has happened is the first thing you want to know,
                and it used to be invisible here. */}
            <StatusIcon status={issue.status} className="size-3 shrink-0" />
            {issue.identifier}
          </button>
        ) : (
          // Only once the lookup has actually failed. This used to fire for any
          // issue absent from the loaded page — which is every finished issue
          // past the first pages, the ones documents are most often about.
          issueGone && (
            <span className="shrink-0">{t(($) => $.doc.issue_missing)}</span>
          )
        )}
        {docLength(card.content) > 0 && (
          <span className="ml-auto shrink-0 tabular-nums text-faint-foreground">
            {t(($) => $.doc.length, { count: docLength(card.content) })}
          </span>
        )}
      </div>
    </div>
  );
}
