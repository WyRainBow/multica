import { describe, it, expect, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useDocumentTitle, configureDocumentTitle } from "./use-document-title";

describe("useDocumentTitle", () => {
  beforeEach(() => {
    configureDocumentTitle({ suffix: "" });
    document.title = "initial";
  });

  it("names the page", () => {
    renderHook(() => useDocumentTitle("COC-6: Workflow recovery"));
    expect(document.title).toBe("COC-6: Workflow recovery");
  });

  it("appends the configured suffix", () => {
    configureDocumentTitle({ suffix: " | Multica" });
    renderHook(() => useDocumentTitle("Issues"));
    expect(document.title).toBe("Issues | Multica");
  });

  it("leaves the previous title alone while the entity loads", () => {
    // Detail pages pass undefined until the query resolves. Writing a
    // placeholder here would flash "Issue" between two real titles every time
    // the user moves from one issue to the next.
    const { rerender } = renderHook(({ title }) => useDocumentTitle(title), {
      initialProps: { title: undefined as string | undefined },
    });
    expect(document.title).toBe("initial");

    rerender({ title: "COC-6: Workflow recovery" });
    expect(document.title).toBe("COC-6: Workflow recovery");
  });

  it("follows the entity when it is renamed", () => {
    const { rerender } = renderHook(({ title }) => useDocumentTitle(title), {
      initialProps: { title: "Old name" },
    });
    rerender({ title: "New name" });
    expect(document.title).toBe("New name");
  });

  it("keeps the last real title when the entity goes away", () => {
    const { rerender } = renderHook(({ title }) => useDocumentTitle(title), {
      initialProps: { title: "Real title" as string | null },
    });
    rerender({ title: null });
    expect(document.title).toBe("Real title");
  });
});
