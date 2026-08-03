import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { resolveCommentAnchors, type CommentAnchor } from "../comment-anchors";

export const commentHighlightPluginKey = new PluginKey("commentHighlight");

/** Marks the highlighted span so a click handler can find its comment. */
export const COMMENT_HIGHLIGHT_ATTRIBUTE = "data-comment-id";

export interface CommentHighlightOptions {
  /**
   * Read on every decoration pass rather than captured once, so the host can
   * update anchors without remounting the editor — the same getter contract
   * the Placeholder extension uses. Dispatch an empty transaction after
   * changing the source to force a recompute.
   */
  anchors: () => readonly CommentAnchor[];
  /**
   * Called when the reader clicks a highlighted span. A getter for the same
   * reason `anchors` is: the extension array is built once at mount, so a
   * captured callback would go stale.
   *
   * Receives the DOM node so the host can position a popover against the exact
   * span rather than the whole editor.
   */
  onAnchorClick?: () => ((commentId: string, element: HTMLElement) => void) | undefined;
}

/**
 * A flat rendering of the document's text, plus the ProseMirror position of
 * each character in it.
 *
 * Anchors are stored as plain-text offsets because that is the only coordinate
 * system that survives Markdown serialization — ProseMirror positions count
 * node boundaries, which shift the moment the document is re-parsed even if
 * not a character of prose changed.
 */
interface FlatText {
  text: string;
  /** `positions[i]` is the document position of `text[i]`. */
  positions: number[];
}

export function flattenDoc(doc: ProseMirrorNode): FlatText {
  let text = "";
  const positions: number[] = [];
  let lastBlockEnd = -1;

  doc.descendants((node, pos) => {
    if (node.isText && node.text) {
      for (let i = 0; i < node.text.length; i++) {
        text += node.text[i];
        positions.push(pos + i);
      }
      return true;
    }
    // Block boundaries become a newline so an anchor cannot silently match
    // across two paragraphs that only look adjacent once flattened. The
    // separator maps to no real position, which is fine: an anchor is trimmed
    // before matching, so a span can never begin or end on one.
    if (node.isBlock && pos > lastBlockEnd && text.length > 0) {
      text += "\n";
      positions.push(-1);
      lastBlockEnd = pos;
    }
    return true;
  });

  return { text, positions };
}

/**
 * Map a plain-text range back to a ProseMirror range.
 *
 * Returns null when either end lands on a synthetic block separator, which
 * means the stored anchor no longer corresponds to a contiguous run of text.
 */
function toDocumentRange(
  flat: FlatText,
  from: number,
  to: number,
): { from: number; to: number } | null {
  const start = flat.positions[from];
  const end = flat.positions[to - 1];
  if (start === undefined || end === undefined) return null;
  if (start < 0 || end < 0) return null;
  return { from: start, to: end + 1 };
}

function buildDecorations(
  doc: ProseMirrorNode,
  anchors: readonly CommentAnchor[],
): DecorationSet {
  if (anchors.length === 0) return DecorationSet.empty;

  const flat = flattenDoc(doc);
  const decorations: Decoration[] = [];
  for (const resolved of resolveCommentAnchors(flat.text, anchors)) {
    const range = toDocumentRange(flat, resolved.from, resolved.to);
    if (!range) continue;
    decorations.push(
      Decoration.inline(range.from, range.to, {
        class: "comment-highlight",
        [COMMENT_HIGHLIGHT_ATTRIBUTE]: resolved.commentId,
      }),
    );
  }
  return DecorationSet.create(doc, decorations);
}

/**
 * Paints inline-comment anchors as highlights.
 *
 * Decorations, deliberately, not marks: the description round-trips through
 * Markdown, which has no syntax for "this span has a comment on it", so a mark
 * would be erased on the next save. Decorations live outside the document, so
 * highlighting costs the stored content nothing and an anchor that no longer
 * matches simply stops painting.
 */
export const CommentHighlight = Extension.create<CommentHighlightOptions>({
  name: "commentHighlight",

  addOptions() {
    return { anchors: () => [] };
  },

  addProseMirrorPlugins() {
    const getAnchors = () => this.options.anchors();
    const getAnchorClick = this.options.onAnchorClick;

    return [
      new Plugin({
        key: commentHighlightPluginKey,
        state: {
          init: (_config, state) => buildDecorations(state.doc, getAnchors()),
          // Recomputed on every transaction rather than mapped forward: the
          // anchor source can change without the document changing at all
          // (a new comment arrives over the websocket), and an empty
          // transaction is exactly how the host asks for a repaint.
          apply: (tr) => buildDecorations(tr.doc, getAnchors()),
        },
        props: {
          decorations(state) {
            return commentHighlightPluginKey.getState(state) as DecorationSet;
          },
          handleClick(_view, _pos, event) {
            const handler = getAnchorClick?.();
            if (!handler) return false;
            const target = (event.target as HTMLElement | null)?.closest<HTMLElement>(
              `[${COMMENT_HIGHLIGHT_ATTRIBUTE}]`,
            );
            const commentId = target?.getAttribute(COMMENT_HIGHLIGHT_ATTRIBUTE);
            if (!target || !commentId) return false;
            handler(commentId, target);
            // Not consumed: the click still moves the caret, because the
            // description stays editable and swallowing it would make a
            // highlighted word the one place you cannot click to type.
            return false;
          },
        },
      }),
    ];
  },
});

/**
 * Snapshot the current selection as a storable anchor.
 *
 * The offset is measured in the SAME flattened coordinates the decoration pass
 * uses. Capturing it any other way — a ProseMirror position, a DOM range —
 * would produce a number that cannot be compared against the document later,
 * because re-parsing the Markdown renumbers PM positions even when the prose is
 * identical.
 *
 * Returns null for a selection with no usable text; the caller must not offer
 * to comment on it.
 */
export function captureSelectionAnchor(
  editor: { state: { doc: ProseMirrorNode; selection: { from: number; to: number } } },
): { text: string; offset: number } | null {
  const { doc, selection } = editor.state;
  const text = doc.textBetween(selection.from, selection.to).trim();
  if (!text) return null;

  const flat = flattenDoc(doc);
  // `positions` maps flattened index -> document position, so the offset is
  // the index whose position is the selection start.
  const offset = flat.positions.indexOf(selection.from);
  return { text, offset: offset >= 0 ? offset : 0 };
}
