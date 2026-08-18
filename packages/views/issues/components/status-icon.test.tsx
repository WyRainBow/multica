// @vitest-environment jsdom

import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";

describe("issue icons", () => {
  it("renders a muted fallback for unknown status values", () => {
    const { container } = render(<StatusIcon status="unexpected_status" />);

    const icon = container.querySelector("svg");
    expect(icon).toHaveClass("text-muted-foreground");
  });

  it("renders cancelled as a filled circle with a white X", () => {
    const { container } = render(<StatusIcon status="cancelled" />);

    // Terminal states are solid: like Done (filled circle + white check),
    // Cancelled is a filled circle + white X, not an X inside a thin ring.
    const icon = container.querySelector("svg");
    expect(icon).toHaveClass("text-muted-foreground");
    expect(icon!.innerHTML).toContain('fill="currentColor"');
    expect(icon!.innerHTML).toContain('stroke="white"');
  });

  it("renders a muted fallback for unknown priority values", () => {
    const { container } = render(<PriorityIcon priority="unexpected_priority" />);

    const icon = container.querySelector("svg");
    expect(icon).toHaveClass("text-muted-foreground");
  });
});
