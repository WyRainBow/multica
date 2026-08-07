"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import type { IssueResource } from "@multica/core/types";
import {
  useCreateIssueResource,
  useUpdateIssueResource,
} from "@multica/core/issue-resources/mutations";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * Add a link, or rename one that is already attached.
 *
 * The title is typed, never fetched. Fetching one means handling auth walls,
 * timeouts and private pages — and the documents most worth attaching here
 * return a login page to an anonymous request, so the fetch would fail exactly
 * where it was supposed to help. Left blank, the row falls back to the host
 * and path, which is always available and never wrong.
 */
export function IssueResourceDialog({
  issueId,
  resource,
  onClose,
}: {
  issueId: string;
  /** Null when adding. */
  resource: IssueResource | null;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const create = useCreateIssueResource(issueId);
  const update = useUpdateIssueResource(issueId);

  const [url, setUrl] = useState(resource?.url ?? "");
  const [title, setTitle] = useState(resource?.title ?? "");

  const pending = create.isPending || update.isPending;
  const canSave = url.trim().length > 0 && !pending;

  const save = async () => {
    if (!canSave) return;
    try {
      if (resource) {
        await update.mutateAsync({ id: resource.id, url: url.trim(), title: title.trim() });
      } else {
        await create.mutateAsync({ url: url.trim(), title: title.trim() });
      }
      onClose();
    } catch (err) {
      // The server normalises and can reject the URL outright, so its message
      // is the useful one — a generic "save failed" would hide "must be an
      // http(s) URL", which is the whole answer.
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.resources.save_failed),
      );
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {resource ? t(($) => $.resources.edit_title) : t(($) => $.resources.add_title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex min-w-0 flex-col gap-3">
          <Input
            value={url}
            onChange={(event) => setUrl(event.target.value)}
            placeholder={t(($) => $.resources.url_placeholder)}
            autoFocus={!resource}
            // Enter saves from either field: this is a two-field form and
            // reaching for the mouse to attach one link is friction the
            // feature exists to remove.
            onKeyDown={(event) => {
              if (event.key === "Enter") void save();
            }}
          />
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={t(($) => $.resources.title_placeholder)}
            autoFocus={Boolean(resource)}
            onKeyDown={(event) => {
              if (event.key === "Enter") void save();
            }}
          />
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={pending}>
            {t(($) => $.resources.cancel)}
          </Button>
          <Button onClick={save} disabled={!canSave}>
            {pending && <Loader2 className="size-3.5 animate-spin" />}
            {t(($) => $.resources.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
