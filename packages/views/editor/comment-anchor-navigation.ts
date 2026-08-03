import { COMMENT_HIGHLIGHT_ATTRIBUTE } from "./extensions/comment-highlight";

/** Class applied briefly so the reader's eye lands on the right span. */
export const COMMENT_ANCHOR_FLASH_CLASS = "comment-highlight-flash";

/** How long the flash lasts. Long enough to notice, short enough not to nag. */
export const COMMENT_ANCHOR_FLASH_MS = 1200;

/**
 * Scroll the description span a comment is anchored to into view and flash it.
 *
 * Returns false when the span is not on screen — the anchor text was edited
 * away, so there is nothing to jump to. Callers use that to render the quote
 * as inert rather than as a link that does nothing when clicked.
 *
 * Queries the live DOM rather than going through the editor instance because
 * the highlight is a decoration: it exists only as rendered output, and the
 * comment list has no editor reference to ask.
 */
export function scrollToCommentAnchor(
  commentId: string,
  options: { root?: ParentNode; flashMs?: number } = {},
): boolean {
  const root = options.root ?? document;
  const target = root.querySelector<HTMLElement>(
    `[${COMMENT_HIGHLIGHT_ATTRIBUTE}="${CSS.escape(commentId)}"]`,
  );
  if (!target) return false;

  target.scrollIntoView({ behavior: "smooth", block: "center" });

  // Restart the flash if one is already running, so a second click on the same
  // quote is not a no-op.
  target.classList.remove(COMMENT_ANCHOR_FLASH_CLASS);
  // Force a reflow so removing and re-adding the class actually replays the
  // animation instead of collapsing into no change at all.
  void target.offsetWidth;
  target.classList.add(COMMENT_ANCHOR_FLASH_CLASS);

  window.setTimeout(() => {
    target.classList.remove(COMMENT_ANCHOR_FLASH_CLASS);
  }, options.flashMs ?? COMMENT_ANCHOR_FLASH_MS);

  return true;
}

/** Whether a comment's anchor is currently painted in the document. */
export function isCommentAnchorVisible(
  commentId: string,
  root: ParentNode = document,
): boolean {
  return (
    root.querySelector(
      `[${COMMENT_HIGHLIGHT_ATTRIBUTE}="${CSS.escape(commentId)}"]`,
    ) !== null
  );
}
