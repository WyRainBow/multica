"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/** How long the tick stays up before the button goes back to offering a copy. */
const CONFIRM_MS = 1500;

/**
 * Copy the whole description.
 *
 * Copies the MARKDOWN SOURCE, not the rendered text — the same string
 * `multica issue get` returns and an agent reads. Taking the rendered text
 * would paste a body whose headings, lists and code fences had become
 * indistinguishable from ordinary lines.
 *
 * Confirms in place with a tick rather than a toast. The comment menu's copy
 * needs a toast because the menu it lives in is gone by the time the copy
 * lands; this button is still on screen, and a toast for something you can
 * watch happen is noise.
 *
 * Rendered on finished issues too. A frozen body is the one people most often
 * want to lift somewhere else, and copying is not editing.
 */
export function CopyDescriptionButton({
  markdown,
  className,
}: {
  markdown: string;
  className?: string;
}) {
  const { t } = useT("issues");
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  // A click landing just before unmount would otherwise set state on a gone
  // component, and leaving the timer running keeps the page alive for it.
  useEffect(() => () => clearTimeout(timerRef.current), []);

  if (!markdown.trim()) return null;

  return (
    <Button
      size="sm"
      variant="ghost"
      className={cn(
        "h-7 gap-1.5 px-2 text-caption text-muted-foreground",
        className,
      )}
      aria-label={t(($) => $.detail.copy_description)}
      onClick={() => {
        void copyText(markdown).then((ok) => {
          if (!ok) return;
          setCopied(true);
          clearTimeout(timerRef.current);
          timerRef.current = setTimeout(() => setCopied(false), CONFIRM_MS);
        });
      }}
    >
      {copied ? (
        <>
          <Check className="size-3.5" />
          {t(($) => $.detail.copied)}
        </>
      ) : (
        <>
          <Copy className="size-3.5" />
          {t(($) => $.detail.copy_description)}
        </>
      )}
    </Button>
  );
}
