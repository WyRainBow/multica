"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Trash2 } from "lucide-react";
import type { Card } from "@multica/core/types";
import {
  useCreateCard,
  useDeleteCard,
  useUpdateCard,
} from "@multica/core/cards/mutations";
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

/**
 * Write or edit one card.
 *
 * Title plus a Markdown body, and nothing else. The title is optional: the
 * note is the point, naming it is not, and asking for a name first is the
 * friction that stops people writing anything down.
 */
export function CardEditorDialog({
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
  const { t } = useT("cards");
  const createCard = useCreateCard();
  const updateCard = useUpdateCard();
  const deleteCard = useDeleteCard();

  const [title, setTitle] = useState(card?.title ?? "");
  const [content, setContent] = useState(card?.content ?? "");
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
        await updateCard.mutateAsync({ id: card.id, title: title.trim(), content });
      } else {
        await createCard.mutateAsync({
          title: title.trim(),
          content,
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
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {card ? t(($) => $.editor.edit_title) : t(($) => $.editor.new_title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={t(($) => $.editor.title_placeholder)}
            autoFocus
          />
          {/* The same Markdown editor an issue description uses, not a raw
              textarea: a card is read back as prose, and headings, lists and
              code blocks are most of what makes a note worth returning to.
              `defaultValue`, not `value` — the editor owns its document while
              open, and re-feeding it on every keystroke would fight the
              cursor. */}
          <div className="max-h-[50vh] min-h-48 overflow-y-auto rounded-lg border px-3 py-2">
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
