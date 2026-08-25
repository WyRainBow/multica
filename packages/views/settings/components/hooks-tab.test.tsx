import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import type { ListWorkspaceHooksResponse } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { HooksTab } from "./hooks-tab";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

const queryResult: { data: ListWorkspaceHooksResponse; isPending: boolean } = {
  data: { runtimes: [] },
  isPending: false,
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => queryResult,
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeHookListOptions: (wsId: string) => ({
    queryKey: ["runtime-hooks", wsId],
  }),
  providerDisplayName: (provider: string) =>
    provider.charAt(0).toUpperCase() + provider.slice(1),
}));

function hook(overrides: Record<string, unknown> = {}) {
  return {
    id: "h-1",
    hook_name: "multica-session-start",
    event: "SessionStart",
    trigger_spec: "*",
    command_path: "~/.claude/hooks/multica-session-start.sh",
    enabled: true,
    telemetry: "unobserved",
    last_fired_at: null,
    observed_at: "2026-08-24T12:00:00Z",
    ...overrides,
  };
}

function group(overrides: Record<string, unknown> = {}) {
  return {
    runtime_id: "r-1",
    name: "Claude (mac)",
    provider: "claude",
    host: "mac",
    status: "online",
    last_seen_at: "2026-08-24T12:00:00Z",
    observed_at: "2026-08-24T12:00:00Z",
    supported: true,
    hooks: [],
    ...overrides,
  };
}

function renderWith(runtimes: unknown[], isPending = false) {
  queryResult.data = {
    runtimes: runtimes as ListWorkspaceHooksResponse["runtimes"],
  };
  queryResult.isPending = isPending;
  return renderWithI18n(<HooksTab />);
}

describe("HooksTab telemetry states", () => {
  afterEach(cleanup);

  // The reason this card exists. A hook installed before telemetry shipped is
  // `unobserved`; a hook telemetry watched and never saw run is `never_fired`.
  // Rendering the two with the same words wastes the whole distinction.
  it("gives each of the four telemetry states its own wording", () => {
    renderWith([
      group({
        hooks: [
          hook({ id: "h-1", hook_name: "a", telemetry: "unobserved" }),
          hook({ id: "h-2", hook_name: "b", telemetry: "never_fired" }),
          hook({
            id: "h-3",
            hook_name: "c",
            telemetry: "fired",
            last_fired_at: "2026-08-24T09:00:00Z",
          }),
          hook({ id: "h-4", hook_name: "d", telemetry: "uncollectable" }),
        ],
      }),
    ]);

    const texts = [
      screen.getByText(/Not yet observed/),
      screen.getByText(/^Never fired$/),
      screen.getByText(/^Fired · /),
      screen.getByText(/Can't be collected/),
    ].map((node) => node.textContent);

    expect(new Set(texts).size).toBe(4);
  });

  it("keeps a telemetry state it has never heard of instead of folding it in", () => {
    renderWith([
      group({ hooks: [hook({ telemetry: "throttled" })] }),
    ]);

    expect(screen.getByText("throttled")).toBeInTheDocument();
    expect(screen.queryByText(/Never fired/)).toBeNull();
    expect(screen.queryByText(/Not yet observed/)).toBeNull();
  });
});

describe("HooksTab runtime groups", () => {
  afterEach(cleanup);

  // An empty list reads as "supported, and you installed none", which sends
  // someone off to debug an installation that was never possible.
  it("says a provider without a hook mechanism is unsupported, not empty", () => {
    renderWith([
      group({ provider: "cursor", supported: false, hooks: [] }),
    ]);

    expect(screen.getByText(/Hooks aren't supported here/)).toBeInTheDocument();
    expect(screen.queryByText(/No Multica hooks installed/)).toBeNull();
  });

  it("separates a scanned-but-empty runtime from one never scanned", () => {
    renderWith([
      group({ runtime_id: "r-1", name: "A", hooks: [] }),
      group({
        runtime_id: "r-2",
        name: "B",
        observed_at: null,
        hooks: [],
      }),
    ]);

    expect(screen.getByText(/No Multica hooks installed/)).toBeInTheDocument();
    expect(
      screen.getByText(/has not reported a hook inventory yet/),
    ).toBeInTheDocument();
  });

  it("marks an offline runtime as stale while still showing its inventory", () => {
    renderWith([
      group({
        status: "offline",
        hooks: [hook({ telemetry: "fired", last_fired_at: "2026-08-24T09:00:00Z" })],
      }),
    ]);

    expect(screen.getByText(/Offline · last seen/)).toBeInTheDocument();
    expect(screen.getByText("multica-session-start")).toBeInTheDocument();
  });

  it("renders the hook's event, trigger and command path", () => {
    renderWith([group({ hooks: [hook()] })]);

    expect(screen.getByText("SessionStart")).toBeInTheDocument();
    expect(screen.getByText("*")).toBeInTheDocument();
    expect(
      screen.getByText("~/.claude/hooks/multica-session-start.sh"),
    ).toBeInTheDocument();
  });

  it("flags a disabled hook", () => {
    renderWith([group({ hooks: [hook({ enabled: false })] })]);

    expect(screen.getByText("Disabled")).toBeInTheDocument();
  });
});

describe("HooksTab empty and loading states", () => {
  afterEach(cleanup);

  it("shows the empty state when the workspace has no runtimes", () => {
    renderWith([]);

    expect(screen.getByText(/No runtimes yet/)).toBeInTheDocument();
  });

  it("shows a loading state while the inventory is pending", () => {
    renderWith([], true);

    expect(screen.getByText(/Loading hook inventory/)).toBeInTheDocument();
  });
});
