import { describe, it, expect, afterEach } from "vitest";
import { Editor } from "@tiptap/core";
import Document from "@tiptap/extension-document";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import Heading from "@tiptap/extension-heading";
import { HeadingAnchor, HEADING_ANCHOR_ATTRIBUTE } from "./heading-anchor";

let editor: Editor | null = null;

afterEach(() => {
  editor?.destroy();
  editor = null;
});

function mount(content: string) {
  const element = document.createElement("div");
  document.body.append(element);
  editor = new Editor({
    element,
    extensions: [Document, Paragraph, Text, Heading, HeadingAnchor],
    content,
  });
  return element;
}

/**
 * The outline both measures and jumps through this attribute. It is produced by
 * a decoration rather than a node attribute — the description round-trips
 * through Markdown, which has nowhere to keep an id — so the only thing that
 * proves it survives to the DOM is reading the DOM.
 */
describe("heading anchors in the rendered document", () => {
  it("stamps every heading with its document position", () => {
    const element = mount("<h2>工作目标</h2><p>正文</p><h2>验收条件</h2>");
    const stamped = element.querySelectorAll(`[${HEADING_ANCHOR_ATTRIBUTE}]`);
    expect(stamped).toHaveLength(2);
    expect(stamped[0]!.textContent).toBe("工作目标");
    expect(stamped[1]!.textContent).toBe("验收条件");
  });

  // The value has to be the position extractOutline reports, or the outline
  // would look up an element that is not there and the jump would do nothing.
  it("stamps the same position extractOutline reports", () => {
    const element = mount("<h2>工作目标</h2><p>正文</p><h2>验收条件</h2>");
    const positions: number[] = [];
    editor!.state.doc.descendants((node, pos) => {
      if (node.type.name === "heading") positions.push(pos);
      return node.type.name !== "heading";
    });
    const stamped = [...element.querySelectorAll(`[${HEADING_ANCHOR_ATTRIBUTE}]`)].map(
      (el) => Number(el.getAttribute(HEADING_ANCHOR_ATTRIBUTE)),
    );
    expect(stamped).toEqual(positions);
  });

  // A decoration is recomputed from the document, so editing above a heading
  // must move its stamp rather than leave a stale one behind.
  it("re-stamps after the text above a heading changes", () => {
    const element = mount("<p>短</p><h2>验收条件</h2>");
    const before = element
      .querySelector(`[${HEADING_ANCHOR_ATTRIBUTE}]`)!
      .getAttribute(HEADING_ANCHOR_ATTRIBUTE);

    editor!.commands.insertContentAt(1, "把上面这段写长一点");

    const after = element
      .querySelector(`[${HEADING_ANCHOR_ATTRIBUTE}]`)!
      .getAttribute(HEADING_ANCHOR_ATTRIBUTE);
    expect(Number(after)).toBeGreaterThan(Number(before));
  });
});
