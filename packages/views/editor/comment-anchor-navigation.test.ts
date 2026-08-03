import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  scrollToCommentAnchor,
  isCommentAnchorVisible,
  COMMENT_ANCHOR_FLASH_CLASS,
} from "./comment-anchor-navigation";
import { COMMENT_HIGHLIGHT_ATTRIBUTE } from "./extensions/comment-highlight";

function paintAnchor(commentId: string): HTMLElement {
  const span = document.createElement("span");
  span.setAttribute(COMMENT_HIGHLIGHT_ATTRIBUTE, commentId);
  span.textContent = "V1 结论";
  span.scrollIntoView = vi.fn();
  document.body.appendChild(span);
  return span;
}

describe("scrollToCommentAnchor", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
  });

  it("scrolls the anchored span into view and flashes it", () => {
    const span = paintAnchor("c1");

    expect(scrollToCommentAnchor("c1")).toBe(true);
    expect(span.scrollIntoView).toHaveBeenCalled();
    expect(span.classList.contains(COMMENT_ANCHOR_FLASH_CLASS)).toBe(true);
  });

  it("reports failure when the anchor text has been edited away", () => {
    // Nothing painted: the comment survives, but there is nowhere to jump to.
    expect(scrollToCommentAnchor("gone")).toBe(false);
  });

  it("removes the flash class once the animation is over", () => {
    vi.useFakeTimers();
    const span = paintAnchor("c1");

    scrollToCommentAnchor("c1", { flashMs: 500 });
    expect(span.classList.contains(COMMENT_ANCHOR_FLASH_CLASS)).toBe(true);

    vi.advanceTimersByTime(500);
    expect(span.classList.contains(COMMENT_ANCHOR_FLASH_CLASS)).toBe(false);
  });

  it("replays the flash on a second click of the same quote", () => {
    // Without the remove/reflow/add dance the class is already present and the
    // animation never restarts, so the second click looks broken.
    const span = paintAnchor("c1");
    scrollToCommentAnchor("c1");
    scrollToCommentAnchor("c1");
    expect(span.classList.contains(COMMENT_ANCHOR_FLASH_CLASS)).toBe(true);
    expect(span.scrollIntoView).toHaveBeenCalledTimes(2);
  });

  it("does not confuse two comments anchored in the same document", () => {
    paintAnchor("c1");
    const second = paintAnchor("c2");

    scrollToCommentAnchor("c2");
    expect(second.classList.contains(COMMENT_ANCHOR_FLASH_CLASS)).toBe(true);
    expect(
      document
        .querySelector(`[${COMMENT_HIGHLIGHT_ATTRIBUTE}="c1"]`)
        ?.classList.contains(COMMENT_ANCHOR_FLASH_CLASS),
    ).toBe(false);
  });

  it("survives a comment id that would otherwise break the selector", () => {
    // Ids come from the server, but a selector built by string concatenation
    // is one malformed value away from throwing inside a click handler.
    const weird = 'c"1';
    const span = paintAnchor(weird);
    expect(scrollToCommentAnchor(weird)).toBe(true);
    expect(span.classList.contains(COMMENT_ANCHOR_FLASH_CLASS)).toBe(true);
  });
});

describe("isCommentAnchorVisible", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  it("is true while the span is painted", () => {
    paintAnchor("c1");
    expect(isCommentAnchorVisible("c1")).toBe(true);
  });

  it("is false once the anchor no longer resolves", () => {
    expect(isCommentAnchorVisible("c1")).toBe(false);
  });
});
