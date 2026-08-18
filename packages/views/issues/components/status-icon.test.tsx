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

  it("renders cancelled as the prohibition sign", () => {
    const { container } = render(<StatusIcon status="cancelled" />);
    const icon = container.querySelector("svg");

    expect(icon).toHaveClass("text-muted-foreground");
    // A ring, not a disc: the filled version made Cancelled read as Done's
    // twin, and the two are not the same kind of ending — Done produced
    // something, Cancelled called the work off.
    expect(icon!.innerHTML).not.toContain('stroke="white"');
    expect(icon!.querySelector("line")).toBeTruthy();
  });

  // Cancelled and Blocked share the ring-and-bar family, so the bar length is
  // load-bearing: Cancelled spans the ring (the standard sign), Blocked keeps
  // a short bar inside it. Same shape at the same length would leave colour as
  // the only difference between two different statuses.
  it("draws a longer bar than blocked", () => {
    const span = (status: "cancelled" | "blocked") => {
      const { container } = render(<StatusIcon status={status} />);
      const line = container.querySelector("svg line")!;
      const x1 = Number(line.getAttribute("x1"));
      const x2 = Number(line.getAttribute("x2"));
      return Math.abs(x2 - x1);
    };
    expect(span("cancelled")).toBeGreaterThan(span("blocked"));
  });

  it("renders a muted fallback for unknown priority values", () => {
    const { container } = render(
      <PriorityIcon priority="unexpected_priority" />,
    );

    const icon = container.querySelector("svg");
    expect(icon).toHaveClass("text-muted-foreground");
  });
});
