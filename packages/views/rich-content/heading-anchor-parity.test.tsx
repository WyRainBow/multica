import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { RichContent } from "./rich-content";
import { extractOutlineFromMarkdown } from "../editor/outline";
import { HEADING_ANCHOR_ATTRIBUTE } from "../editor/extensions/heading-anchor";

// The outline looks a heading up by this attribute. If the number the renderer
// stamps is not the number the extractor reports, the entry is there and the
// jump silently does nothing — which is the failure this pair exists to stop.
describe("rendered headings carry the outline's anchor", () => {
  const md = "intro\n\n## 工作目标\n正文\n\n### 验收条件\n\n#### 更深一层\n";

  it("stamps every heading", () => {
    const { container } = render(<RichContent content={md} />);
    expect(
      container.querySelectorAll(`[${HEADING_ANCHOR_ATTRIBUTE}]`),
    ).toHaveLength(3);
  });

  it("stamps exactly the positions the extractor reports", () => {
    const { container } = render(<RichContent content={md} />);
    const stamped = [
      ...container.querySelectorAll(`[${HEADING_ANCHOR_ATTRIBUTE}]`),
    ].map((el) => Number(el.getAttribute(HEADING_ANCHOR_ATTRIBUTE)));
    expect(stamped).toEqual(extractOutlineFromMarkdown(md).map((h) => h.pos));
  });

  it("keeps the heading text and level", () => {
    const { container } = render(<RichContent content={md} />);
    const first = container.querySelector(`[${HEADING_ANCHOR_ATTRIBUTE}]`)!;
    expect(first.tagName).toBe("H2");
    expect(first.textContent).toBe("工作目标");
  });
});
