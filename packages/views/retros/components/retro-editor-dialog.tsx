"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Trash2 } from "lucide-react";
import type { Retro } from "@multica/core/types";
import {
  useCreateRetro,
  useDeleteRetro,
  useUpdateRetro,
} from "@multica/core/retros/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

/**
 * Write or edit one retro.
 *
 * A plain textarea, not the rich editor: a retro is written once, read
 * occasionally, and never rendered into an agent prompt — the editor's upload
 * pipeline, mention picker and Markdown round-trip would all be cost with no
 * reader. Markdown still displays as typed.
 */
export function RetroEditorDialog({
  retro,
  issueId,
  onClose,
}: {
  /** Null for a new retro. */
  retro: Retro | null;
  /** Pre-links a new retro to a requirement (used from the issue page). */
  issueId?: string;
  onClose: () => void;
}) {
  const { t } = useT("retros");
  const createRetro = useCreateRetro();
  const updateRetro = useUpdateRetro();
  const deleteRetro = useDeleteRetro();

  const [title, setTitle] = useState(retro?.title ?? "");
  const [content, setContent] = useState(retro?.content ?? "");
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const pending =
    createRetro.isPending || updateRetro.isPending || deleteRetro.isPending;
  const canSave = title.trim().length > 0 && !pending;

  const save = async () => {
    if (!canSave) return;
    try {
      if (retro) {
        await updateRetro.mutateAsync({ id: retro.id, title: title.trim(), content });
      } else {
        await createRetro.mutateAsync({
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
    if (!retro) return;
    try {
      await deleteRetro.mutateAsync(retro.id);
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
            {retro ? t(($) => $.editor.edit_title) : t(($) => $.editor.new_title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={t(($) => $.editor.title_placeholder)}
            autoFocus
          />
          <Textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            placeholder={t(($) => $.editor.content_placeholder)}
            className="min-h-64 font-mono text-caption"
          />
        </div>

        <DialogFooter className="justify-between sm:justify-between">
          <div>
            {retro &&
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
