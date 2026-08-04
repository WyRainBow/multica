"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Trash2, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import {
  useCreateGrowthCard,
  useDeleteGrowthCard,
  useUpdateGrowthCard,
} from "@multica/core/growth-cards/mutations";
import type { GrowthCard, GrowthCardFields, Issue } from "@multica/core/types";
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
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import {
  GROWTH_CARD_BODY_KEYS,
  draftFromCard,
  emptyDraft,
} from "../fields";

/**
 * Write or edit one growth card.
 *
 * Plain textareas, not the rich editor: a card is written once, read
 * occasionally, and never rendered into an agent prompt — the editor's upload
 * pipeline, mention picker and Markdown round-trip would all be cost with no
 * reader.
 *
 * Nothing is required, not even the title. The blanks are the signal: a card
 * with an empty 我亲自验证了什么 is the record of a delivery that was never
 * verified, and refusing to save it would only teach the writer to fill the
 * box with something.
 */
export function GrowthCardEditorDialog({
  card,
  issueId,
  onClose,
}: {
  /** Null for a new card. */
  card: GrowthCard | null;
  /** Pre-links a new card to a requirement (used from the issue page). */
  issueId?: string;
  onClose: () => void;
}) {
  const { t } = useT("growth-cards");
  const wsId = useWorkspaceId();
  const createCard = useCreateGrowthCard();
  const updateCard = useUpdateGrowthCard();
  const deleteCard = useDeleteGrowthCard();

  const [draft, setDraft] = useState<Required<GrowthCardFields>>(() =>
    card ? draftFromCard(card) : emptyDraft(),
  );
  const [linkedIssueId, setLinkedIssueId] = useState<string | null>(
    card?.issue_id ?? issueId ?? null,
  );
  const [picking, setPicking] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // The linked requirement renders as its identifier, so the dialog needs
  // the workspace issues to resolve the stored id.
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const linkedIssue = useMemo(
    () => issues.find((issue) => issue.id === linkedIssueId),
    [issues, linkedIssueId],
  );

  const pending =
    createCard.isPending || updateCard.isPending || deleteCard.isPending;

  const setField = (key: keyof GrowthCardFields, value: string) =>
    setDraft((prev) => ({ ...prev, [key]: value }));

  const save = async () => {
    if (pending) return;
    try {
      if (card) {
        await updateCard.mutateAsync({
          id: card.id,
          ...draft,
          // Explicit null detaches; the server tells absent from null.
          issue_id: linkedIssueId,
        });
      } else {
        await createCard.mutateAsync({
          ...draft,
          ...(linkedIssueId ? { issue_id: linkedIssueId } : {}),
        });
      }
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.editor.save_failed),
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
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.editor.delete_failed),
      );
    }
  };

  return (
    <>
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent className="flex max-h-[85vh] max-w-2xl flex-col">
          <DialogHeader>
            <DialogTitle>
              {card ? t(($) => $.editor.edit_title) : t(($) => $.editor.new_title)}
            </DialogTitle>
          </DialogHeader>

          <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
            <Field
              label={t(($) => $.fields.title.label)}
              hint={t(($) => $.fields.title.hint)}
            >
              <Input
                value={draft.title}
                onChange={(event) => setField("title", event.target.value)}
                placeholder={t(($) => $.fields.title.placeholder)}
                autoFocus
              />
            </Field>

            <IssueLink
              issue={linkedIssue}
              linkedIssueId={linkedIssueId}
              onPick={() => setPicking(true)}
              onClear={() => setLinkedIssueId(null)}
            />

            {GROWTH_CARD_BODY_KEYS.map((key) => (
              <Field
                key={key}
                label={t(($) => $.fields[key].label)}
                hint={t(($) => $.fields[key].hint)}
              >
                <Textarea
                  value={draft[key]}
                  onChange={(event) => setField(key, event.target.value)}
                  placeholder={t(($) => $.fields[key].placeholder)}
                  className="min-h-20 text-body"
                />
              </Field>
            ))}
          </div>

          <DialogFooter className="justify-between sm:justify-between">
            <div>
              {card &&
                (confirmingDelete ? (
                  <div className="flex items-center gap-2">
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.editor.delete_confirm)}
                    </span>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={remove}
                      disabled={pending}
                    >
                      {t(($) => $.editor.delete_action)}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setConfirmingDelete(false)}
                    >
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
              <Button size="sm" onClick={save} disabled={pending}>
                {pending && <Loader2 className="size-3.5 animate-spin" />}
                {t(($) => $.editor.save)}
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {picking && (
        <IssuePickerModal
          open
          onOpenChange={(open) => !open && setPicking(false)}
          title={t(($) => $.editor.link_issue_title)}
          description={t(($) => $.editor.link_issue_description)}
          excludeIds={[]}
          // Top-level requirements only. A card is written about a delivery,
          // and a delivery is the requirement — its sub-issues are the steps.
          filter={(issue) => !issue.parent_issue_id}
          onSelect={(issue) => {
            setLinkedIssueId(issue.id);
            setPicking(false);
          }}
        />
      )}
    </>
  );
}

/** One labelled field with its prompting question. */
function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline gap-2">
        <span className="text-body font-medium">{label}</span>
        <span className="text-caption text-muted-foreground">{hint}</span>
      </div>
      {children}
    </div>
  );
}

/** The optional link to the requirement this card is about. */
function IssueLink({
  issue,
  linkedIssueId,
  onPick,
  onClear,
}: {
  issue?: Issue;
  linkedIssueId: string | null;
  onPick: () => void;
  onClear: () => void;
}) {
  const { t } = useT("growth-cards");
  return (
    <div className="flex items-center gap-2">
      <span className="text-body font-medium">
        {t(($) => $.editor.linked_issue)}
      </span>
      {linkedIssueId ? (
        <div className="flex items-center gap-1">
          <Button size="sm" variant="outline" onClick={onPick}>
            {/* An id with no loaded issue is a requirement that was deleted or
                sits outside the loaded window — still linked, still clearable. */}
            {issue
              ? `${issue.identifier} ${issue.title}`
              : t(($) => $.card.issue_missing)}
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            onClick={onClear}
            aria-label={t(($) => $.editor.unlink_issue)}
          >
            <X className="size-3.5" />
          </Button>
        </div>
      ) : (
        <Button size="sm" variant="outline" onClick={onPick}>
          {t(($) => $.editor.link_issue)}
        </Button>
      )}
    </div>
  );
}
