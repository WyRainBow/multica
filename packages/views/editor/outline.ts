import type { Node as ProseMirrorNode } from "@tiptap/pm/model";

/**
 * One entry in a description's outline.
 *
 * `pos` is a ProseMirror document position rather than a DOM id: the outline
 * is recomputed from the live document on every edit, and a position is what
 * that document can hand back. Ids would have to be written INTO the document
 * to survive, and the description round-trips through Markdown, which has
 * nowhere to keep them.
 */
export interface OutlineHeading {
  /** Stable within one computation; used as a React key and for active state. */
  id: string;
  level: number;
  text: string;
  pos: number;
}

/**
 * Read the headings out of a document, in document order.
 *
 * Empty headings are skipped. A heading being typed passes through "#", "##"
 * with no text at all, and an outline that flickers a blank row on every new
 * section is worse than one that appears a keystroke late.
 */
export function extractOutline(doc: ProseMirrorNode): OutlineHeading[] {
  const headings: OutlineHeading[] = [];
  doc.descendants((node, pos) => {
    if (node.type.name !== "heading") return true;
    const text = node.textContent.trim();
    if (!text) return false;
    const level = Number(node.attrs.level) || 1;
    headings.push({
      // Position is unique per heading within a document, which is exactly
      // the identity the outline needs — two sections can share a title.
      id: `h${pos}`,
      level,
      text,
      pos,
    });
    return false;
  });
  return headings;
}

/**
 * Normalize heading levels to consecutive indent depths.
 *
 * A description that only uses `##` and `###` should not render indented by a
 * level that is not there. Depth follows the ORDER levels appear in, not their
 * absolute number, so `## / ### / ##` indents as 0 / 1 / 0 and a document that
 * skips from `#` to `###` does not open a phantom middle tier.
 */
export function outlineDepths(headings: readonly OutlineHeading[]): number[] {
  const depths: number[] = [];
  // Stack of the heading levels currently open above the cursor.
  const open: number[] = [];
  for (const heading of headings) {
    while (open.length > 0 && open[open.length - 1]! >= heading.level) {
      open.pop();
    }
    depths.push(open.length);
    open.push(heading.level);
  }
  return depths;
}

/**
 * The heading a reader is currently under, given which ones are above the
 * viewport's reading line.
 *
 * The LAST heading at or above the line wins, not the first one visible: with
 * a long section the heading itself scrolls away, and an outline that
 * de-highlights the section you are still reading is actively misleading.
 * Returns null while the reader is above the first heading.
 */
export function activeOutlineId(
  headings: readonly OutlineHeading[],
  offsetsById: ReadonlyMap<string, number>,
  readingLine: number,
): string | null {
  let active: string | null = null;
  for (const heading of headings) {
    const top = offsetsById.get(heading.id);
    if (top === undefined) continue;
    if (top <= readingLine) active = heading.id;
    else break;
  }
  return active;
}
