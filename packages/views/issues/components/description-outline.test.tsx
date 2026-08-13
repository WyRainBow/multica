import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import type { OutlineHeading } from "../../editor/outline";
import { DescriptionOutline } from "./description-outline";

vi.mock("@multica/core/issues/stores", () => ({
  useDescriptionOutlineStore: (selector: (s: unknown) => unknown) =>
    selector({ collapsed: false, toggleCollapsed: vi.fn() }),
}));

const HEADINGS: OutlineHeading[] = [
  { id: "h1", level: 2, text: "工作目标", pos: 1 },
  { id: "h42", level: 2, text: "验收条件", pos: 42 },
];

/**
 * A scroll container holding the rendered headings, stamped the way the
 * editor's HeadingAnchor decoration stamps them. jsdom has no layout, so the
 * rects are supplied: the container sits at viewport 0 and the second heading
 * 600px below it, while the container is already scrolled 100px down.
 */
function makeContainer({ scrollTop = 100 } = {}) {
  const container = document.createElement("div");
  container.getBoundingClientRect = () => ({ top: 0 }) as DOMRect;
  Object.defineProperty(container, "scrollTop", {
    value: scrollTop,
    writable: true,
  });
  Object.defineProperty(container, "clientHeight", { value: 800 });
  container.scrollTo = vi.fn();

  for (const [pos, top] of [
    [1, -80],
    [42, 600],
  ] as const) {
    const el = document.createElement("h2");
    el.setAttribute("data-outline-pos", String(pos));
    el.getBoundingClientRect = () => ({ top }) as DOMRect;
    container.append(el);
  }
  document.body.append(container);
  return container;
}

function render(container: HTMLElement | null) {
  renderWithI18n(
    <DescriptionOutline headings={HEADINGS} scrollContainer={container} />,
  );
}

beforeEach(() => {
  document.body.innerHTML = "";
});

// Clicking an entry is the only thing this component exists to do, and the
// jump used to run through the editor ref and scrollIntoView — a path nothing
// else exercised, and one the page does not always have an editor for.
describe("jumping to a heading", () => {
  it("scrolls the container to the heading", async () => {
    const user = userEvent.setup();
    const container = makeContainer({ scrollTop: 100 });
    render(container);

    await user.click(screen.getByRole("button", { name: "验收条件" }));

    // 100 (current) + 600 (distance below the container top) - 24 (margin)
    expect(container.scrollTo).toHaveBeenCalledWith({
      top: 676,
      behavior: "smooth",
    });
  });

  // Landing flush at the top edge reads as "the section starts off-screen".
  it("leaves a margin above the heading", async () => {
    const user = userEvent.setup();
    const container = makeContainer({ scrollTop: 100 });
    render(container);

    await user.click(screen.getByRole("button", { name: "验收条件" }));
    const { top } = (container.scrollTo as ReturnType<typeof vi.fn>).mock
      .calls[0]![0] as { top: number };
    expect(top).toBeLessThan(700);
  });

  // A heading already above the fold resolves to a negative offset; scrolling
  // to a negative top would be ignored or clamped inconsistently.
  it("never scrolls to a negative offset", async () => {
    const user = userEvent.setup();
    const container = makeContainer({ scrollTop: 0 });
    render(container);

    await user.click(screen.getByRole("button", { name: "工作目标" }));
    expect(container.scrollTo).toHaveBeenCalledWith({
      top: 0,
      behavior: "smooth",
    });
  });

  // Smooth scrolling reaches the scroll-driven recompute several frames later,
  // and an outline that highlights a moment after the click reads as a click
  // that did not register.
  it("marks the clicked entry active immediately", async () => {
    const user = userEvent.setup();
    render(makeContainer());

    const target = screen.getByRole("button", { name: "验收条件" });
    await user.click(target);
    expect(target.className).toContain("font-medium");
  });

  // Nothing to measure against, and deliberately no second way to scroll: the
  // jump used to have one, and the path the tests did not cover was the one
  // the reader clicked.
  it("does nothing, and does not throw, with no container", async () => {
    const user = userEvent.setup();
    render(null);

    const target = screen.getByRole("button", { name: "验收条件" });
    await user.click(target);
    expect(target).toBeInTheDocument();
  });
});
