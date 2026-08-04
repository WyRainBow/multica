import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";

export const headingAnchorPluginKey = new PluginKey("headingAnchor");

/** Attribute the outline measures heading positions by. */
export const HEADING_ANCHOR_ATTRIBUTE = "data-outline-pos";

/**
 * Stamps each heading with its document position.
 *
 * A decoration rather than a node attribute: the description round-trips
 * through Markdown, which has nowhere to keep an id, so anything written into
 * the document would be erased on the next save. A decoration lives outside
 * the document and is recomputed from it, which also means the attribute can
 * never drift from the position it claims to be.
 *
 * The outline needs this because a ProseMirror position says nothing about
 * where a heading ended up on screen — images, code blocks and wrapping all
 * move it. Measuring the rendered element is the only way to know which
 * section the reader is actually inside.
 */
export const HeadingAnchor = Extension.create({
  name: "headingAnchor",

  addProseMirrorPlugins() {
    return [
      new Plugin({
        key: headingAnchorPluginKey,
        props: {
          decorations(state) {
            const decorations: Decoration[] = [];
            state.doc.descendants((node, pos) => {
              if (node.type.name !== "heading") return true;
              decorations.push(
                Decoration.node(pos, pos + node.nodeSize, {
                  [HEADING_ANCHOR_ATTRIBUTE]: String(pos),
                }),
              );
              return false;
            });
            return DecorationSet.create(state.doc, decorations);
          },
        },
      }),
    ];
  },
});
