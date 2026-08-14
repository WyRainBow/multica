import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { RichContent } from "./rich-content";

// Density is CSS-only, so the thing worth asserting is that the class reaches
// the DOM — the stylesheet cannot apply to a hook that is not there.
describe("preview density", () => {
  function root(density: "document" | "preview") {
    const { container } = render(
      <RichContent
        // Braces: a quoted JSX attribute would pass a literal backslash-n and
        // the whole string would parse as one heading.
        content={"## 一、交付了什么\n\n正文"}
        density={density}
        phase="settled"
      />,
    );
    return container.querySelector("[data-rich-content]")!;
  }

  it("marks the wrapper so the preview stylesheet can reach it", () => {
    expect(root("preview").className).toContain("rich-content-preview");
    expect(root("preview").getAttribute("data-density")).toBe("preview");
  });

  it("leaves document density alone", () => {
    expect(root("document").className).not.toContain("rich-content-preview");
  });

  // CSS only: the same Markdown must produce the same blocks either way, or
  // "density" would have become a second renderer.
  it("renders the same semantic DOM as document density", () => {
    expect(root("preview").querySelector("h2")?.textContent).toBe(
      "一、交付了什么",
    );
    expect(root("document").querySelector("h2")?.textContent).toBe(
      "一、交付了什么",
    );
  });
});
