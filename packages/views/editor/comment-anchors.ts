/**
 * Re-locating inline-comment anchors inside a document.
 *
 * The issue description is stored as Markdown, which has no representation for
 * a highlight, so a comment cannot keep a mark in the document. It keeps the
 * text it was written against instead, and the span is found again here every
 * time the description renders.
 *
 * That makes "not found" a normal outcome, not an error: the user edited the
 * sentence away. Such a comment stops highlighting and reads as an ordinary
 * comment — it is never dropped.
 */

/** The stored side of an anchor: what a comment remembers about its span. */
export interface CommentAnchor {
  commentId: string;
  text: string;
  /**
   * Character offset the anchor had when it was created. Only a hint: it
   * drifts as the document is edited, and is used solely to choose between
   * repeated occurrences of the same text.
   */
  offset?: number | null;
}

/** A resolved anchor, in plain-text character offsets. */
export interface ResolvedAnchor {
  commentId: string;
  from: number;
  to: number;
}

/**
 * Pick the occurrence of `text` that best matches `offset`.
 *
 * Exact-offset match wins outright. Otherwise the nearest occurrence wins,
 * which keeps a highlight on the right sentence after edits elsewhere in the
 * document shift it. With no hint at all the first occurrence wins, so a
 * highlight lands somewhere sensible rather than nowhere.
 */
function findBestOccurrence(
  haystack: string,
  needle: string,
  offset: number | null | undefined,
): number {
  if (!needle) return -1;

  const first = haystack.indexOf(needle);
  if (first === -1) return -1;
  if (offset == null) return first;

  let best = first;
  let bestDistance = Math.abs(first - offset);
  let cursor = haystack.indexOf(needle, first + 1);
  while (cursor !== -1) {
    const distance = Math.abs(cursor - offset);
    // `<` not `<=`: on a tie the earlier occurrence keeps the highlight, so
    // the result does not depend on scan order.
    if (distance < bestDistance) {
      best = cursor;
      bestDistance = distance;
    }
    if (distance === 0) break;
    cursor = haystack.indexOf(needle, cursor + 1);
  }
  return best;
}

/**
 * Resolve every anchor against `text`, dropping the ones that no longer match.
 *
 * Two anchors may legitimately resolve to the same span (two people commenting
 * on the same sentence); overlapping spans are the caller's problem to render,
 * not something to de-duplicate here. Results come back sorted by position so
 * a renderer can walk them in document order.
 */
export function resolveCommentAnchors(
  text: string,
  anchors: readonly CommentAnchor[],
): ResolvedAnchor[] {
  const resolved: ResolvedAnchor[] = [];
  for (const anchor of anchors) {
    const needle = anchor.text?.trim();
    if (!needle) continue;
    const at = findBestOccurrence(text, needle, anchor.offset);
    if (at === -1) continue;
    resolved.push({ commentId: anchor.commentId, from: at, to: at + needle.length });
  }
  return resolved.sort((a, b) => a.from - b.from || a.to - b.to);
}

/** Anchors whose text is gone from the document — rendered without a jump target. */
export function unresolvedAnchorIds(
  text: string,
  anchors: readonly CommentAnchor[],
): string[] {
  const resolvedIds = new Set(
    resolveCommentAnchors(text, anchors).map((a) => a.commentId),
  );
  return anchors
    .filter((anchor) => !resolvedIds.has(anchor.commentId))
    .map((anchor) => anchor.commentId);
}
