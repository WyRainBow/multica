"use client";

import { useCallback, useEffect, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import {
  isCommentAnchorVisible,
  scrollToCommentAnchor,
} from "../../editor/comment-anchor-navigation";

/**
 * The description span an inline comment was written against, shown above the
 * comment body.
 *
 * Clicking it jumps to the highlight in the description. When the anchor text
 * has been edited away there is nothing to jump to, so the quote renders as
 * plain, inert text — the comment still reads correctly, it just no longer
 * points anywhere. That is the whole reason the anchor lives on the comment
 * instead of in the document: the comment survives edits to the prose.
 */
export function CommentAnchorQuote({
  commentId,
  text,
  className,
}: {
  commentId: string;
  text: string;
  className?: string;
}) {
  const { t } = useT("issues");
  // Assume navigable on first paint. The description and the comment list
  // render together, and starting at `false` would flash every quote as
  // orphaned for one frame on every mount.
  const [navigable, setNavigable] = useState(true);

  useEffect(() => {
    setNavigable(isCommentAnchorVisible(commentId));
  }, [commentId, text]);

  const handleClick = useCallback(() => {
    // Re-check by acting: the highlight may have appeared or disappeared since
    // the last render, and the jump itself reports which.
    setNavigable(scrollToCommentAnchor(commentId));
  }, [commentId]);

  const quote = (
    <span className="line-clamp-2 whitespace-pre-wrap">{text}</span>
  );

  if (!navigable) {
    return (
      <div
        className={cn(
          "border-l-2 border-border pl-2 text-caption text-muted-foreground",
          className,
        )}
        title={t(($) => $.comment.anchor_missing)}
      >
        {quote}
      </div>
    );
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      title={t(($) => $.comment.anchor_jump)}
      className={cn(
        "block w-full cursor-pointer border-l-2 border-primary/40 pl-2 text-left",
        "text-caption text-muted-foreground transition-colors",
        "hover:border-primary hover:text-foreground",
        className,
      )}
    >
      {quote}
    </button>
  );
}
