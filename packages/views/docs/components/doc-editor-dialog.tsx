"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Loader2, Trash2 } from "lucide-react";
import type { Card } from "@multica/core/types";
import {
  useCreateCard,
  useDeleteCard,
  useUpdateCard,
} from "@multica/core/docs/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { ContentEditor } from "../../editor";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { useQuery } from "@tanstack/react-query";
import { cardListOptions } from "@multica/core/docs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { docKindTabs } from "../doc-kinds";

/**
 * Write or edit one card.
 *
 * Title plus a Markdown body, and nothing else. The title is optional: the
 * note is the point, naming it is not, and asking for a name first is the
 * friction that stops people writing anything down.
 */
export function DocEditorDialog({
  card,
  issueId,
  onClose,
}: {
  /** Null for a new card. */
  card: Card | null;
  /** Pre-links a new card to a requirement (used from the issue page). */
  issueId?: string;
  onClose: () => void;
}) {
  const { t } = useT("docs");
  const createCard = useCreateCard();
  const updateCard = useUpdateCard();
  const deleteCard = useDeleteCard();

  const [title, setTitle] = useState(card?.title ?? "");
  const [content, setContent] = useState(card?.content ?? "");
  const [kind, setKind] = useState(card?.kind ?? "");

  // Suggestions come from the list the page already has in cache, so opening
  // the dialog costs no request. Reusing an existing name is the whole point:
  // free text invites 文档 / 档案 / doc for one thing, and three tabs for one
  // category is worse than none.
  const wsId = useWorkspaceId();
  const { data: cardList } = useQuery(cardListOptions(wsId));
  const kindSuggestions = useMemo(
    () => docKindTabs(cardList?.cards ?? []).map((tab) => tab.kind),
    [cardList],
  );
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const pending =
    createCard.isPending || updateCard.isPending || deleteCard.isPending;
  // Either field alone is a card. Only a completely empty one is refused,
  // and that is a stray click rather than a note.
  const canSave = (title.trim().length > 0 || content.trim().length > 0) && !pending;

  const save = async () => {
    if (!canSave) return;
    try {
      if (card) {
        await updateCard.mutateAsync({
          id: card.id,
          title: title.trim(),
          content,
          kind: kind.trim(),
        });
      } else {
        await createCard.mutateAsync({
          title: title.trim(),
          content,
          kind: kind.trim(),
          ...(issueId ? { issue_id: issueId } : {}),
        });
      }
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.editor.save_failed),
      );
    }
  };

  const remove = async () => {
    if (!card) return;
    try {
      await deleteCard.mutateAsync(card.id);
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.editor.delete_failed),
      );
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      {/* sm:, not a bare max-w-*: DialogContent's own class list ends with
          sm:max-w-sm, and an unprefixed override loses to it above 640px. The
          box then sat at 384px while the editor inside kept its natural width,
          painting content outside the background — which read as the dialog
          being half transparent rather than half the width it should be. */}
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>
            {card ? t(($) => $.editor.edit_title) : t(($) => $.editor.new_title)}
          </DialogTitle>
        </DialogHeader>

        {/* min-w-0: a flex child sizes to its content by default, so one wide
            table or an unbreakable code span in the note would push this
            column past the dialog and paint outside its background again —
            the same symptom, from the other direction. */}
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex min-w-0 gap-2">
            <Input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder={t(($) => $.editor.title_placeholder)}
              autoFocus
              className="min-w-0 flex-1"
            />
            {/* A plain input with suggestions, not a fixed select: kinds are
                whatever has been written, so the control has to accept a name
                that does not exist yet. The list is there so a second card
                about the same thing reuses 文档 instead of inventing 档案. */}
            <Input
              value={kind}
              onChange={(event) => setKind(event.target.value)}
              placeholder={t(($) => $.editor.kind_placeholder)}
              list="card-kind-suggestions"
              className="w-32 shrink-0"
            />
            <datalist id="card-kind-suggestions">
              {kindSuggestions.map((suggestion) => (
                <option key={suggestion} value={suggestion} />
              ))}
            </datalist>
          </div>
          {/* The same Markdown editor an issue description uses, not a raw
              textarea: a card is read back as prose, and headings, lists and
              code blocks are most of what makes a note worth returning to.
              `defaultValue`, not `value` — the editor owns its document while
              open, and re-feeding it on every keystroke would fight the
              cursor. */}
          {/* overflow-auto, not overflow-y-auto: a note pasted from a doc
              carries tables and long code lines, and a horizontal overflow
              with nowhere to go escapes the box instead of scrolling in it. */}
          <div className="max-h-[50vh] min-h-48 overflow-auto rounded-lg border px-3 py-2">
            <ContentEditor
              key={card?.id ?? "new"}
              defaultValue={card?.content ?? ""}
              placeholder={t(($) => $.editor.content_placeholder)}
              onUpdate={setContent}
              // Nothing to mention in a personal note, and no slash menu:
              // both would put an agent-shaped affordance on a human's
              // scratchpad.
              disableMentions
            />
          </div>
        </div>

        <DialogFooter className="justify-between sm:justify-between">
          <div>
            {card &&
              (confirmingDelete ? (
                <div className="flex items-center gap-2">
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.editor.delete_confirm)}
                  </span>
                  <Button size="sm" variant="destructive" onClick={remove} disabled={pending}>
                    {t(($) => $.editor.delete_action)}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setConfirmingDelete(false)}>
                    {t(($) => $.editor.cancel)}
                  </Button>
                </div>
              ) : (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setConfirmingDelete(true)}
                  disabled={pending}
                >
                  <Trash2 className="size-3.5" />
                  {t(($) => $.editor.delete_action)}
                </Button>
              ))}
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="ghost" onClick={onClose} disabled={pending}>
              {t(($) => $.editor.cancel)}
            </Button>
            <Button size="sm" onClick={save} disabled={!canSave}>
              {pending && <Loader2 className="size-3.5 animate-spin" />}
              {t(($) => $.editor.save)}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
