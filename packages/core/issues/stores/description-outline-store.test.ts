import { describe, it, expect, beforeEach } from "vitest";
import { useDescriptionOutlineStore } from "./description-outline-store";

describe("description outline preference", () => {
  beforeEach(() => {
    useDescriptionOutlineStore.setState({ collapsed: false });
  });

  it("shows the outline by default", () => {
    // A reader who has never touched it should see the feature exists.
    expect(useDescriptionOutlineStore.getState().collapsed).toBe(false);
  });

  it("toggles both ways", () => {
    useDescriptionOutlineStore.getState().toggleCollapsed();
    expect(useDescriptionOutlineStore.getState().collapsed).toBe(true);
    useDescriptionOutlineStore.getState().toggleCollapsed();
    expect(useDescriptionOutlineStore.getState().collapsed).toBe(false);
  });
});
