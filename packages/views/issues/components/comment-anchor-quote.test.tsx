import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { CommentAnchorQuote } from "./comment-anchor-quote";
import { COMMENT_HIGHLIGHT_ATTRIBUTE } from "../../editor/extensions/comment-highlight";

vi.mock("../../i18n", () => ({
  useT: () => ({ t: (pick: (d: Record<string, unknown>) => string) => pick({
    comment: { anchor_jump: "jump", anchor_missing: "missing" },
  } as never) }),
}));

function paintAnchor(commentId: string): HTMLElement {
  const span = document.createElement("span");
  span.setAttribute(COMMENT_HIGHLIGHT_ATTRIBUTE, commentId);
  span.scrollIntoView = vi.fn();
  document.body.appendChild(span);
  return span;
}

describe("CommentAnchorQuote", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  it("is clickable while the highlight is on screen", () => {
    const span = paintAnchor("c1");
    render(<CommentAnchorQuote commentId="c1" text="V1 结论" />);

    fireEvent.click(screen.getByRole("button"));
    expect(span.scrollIntoView).toHaveBeenCalled();
  });

  it("still shows the quoted text when the anchor is gone", () => {
    // The comment survives an edit to the prose it was written against; only
    // the jump is lost. Hiding the quote would erase what was commented on.
    render(<CommentAnchorQuote commentId="orphan" text="被删掉的原文" />);

    expect(screen.getByText("被删掉的原文")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("stops offering the jump once the anchor disappears under it", () => {
    // The description is editable: the highlight can vanish while the comment
    // list is still on screen. Clicking must degrade, not silently do nothing
    // forever.
    const span = paintAnchor("c1");
    render(<CommentAnchorQuote commentId="c1" text="V1 结论" />);
    expect(screen.getByRole("button")).toBeInTheDocument();

    // Remove only the highlight — wiping document.body would take the
    // component under test with it.
    span.remove();
    fireEvent.click(screen.getByRole("button"));

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText("V1 结论")).toBeInTheDocument();
  });
});
