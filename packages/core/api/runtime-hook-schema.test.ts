import { describe, it, expect } from "vitest";
import {
  RuntimeHookSchema,
  RuntimeHookGroupSchema,
  ListWorkspaceHooksResponseSchema,
} from "./schemas";

// The hook inventory answers "is the Multica hook actually installed on that
// machine, and has anyone seen it run". A drifting backend must degrade a row,
// never empty the page — and must never turn "nobody was watching" into "this
// hook is dead". Those are the cases worth pinning.

describe("RuntimeHookSchema", () => {
  it("parses a well-formed hook", () => {
    const parsed = RuntimeHookSchema.parse({
      id: "h-1",
      hook_name: "multica-session-start",
      event: "SessionStart",
      trigger_spec: "*",
      command_path: "~/.claude/hooks/multica-session-start.sh",
      enabled: true,
      telemetry: "fired",
      last_fired_at: "2026-08-24T12:00:00Z",
      observed_at: "2026-08-24T12:05:00Z",
    });
    expect(parsed.hook_name).toBe("multica-session-start");
    expect(parsed.telemetry).toBe("fired");
    expect(parsed.last_fired_at).toBe("2026-08-24T12:00:00Z");
  });

  // The whole point of the card: an omitted telemetry field means nobody
  // said, not that the hook stayed quiet. Defaulting to `never_fired` would
  // accuse a working hook of being dead.
  it("defaults a missing telemetry state to unobserved, never never_fired", () => {
    const parsed = RuntimeHookSchema.parse({ id: "h-1", hook_name: "x" });
    expect(parsed.telemetry).toBe("unobserved");
    expect(parsed.last_fired_at).toBeNull();
    expect(parsed.enabled).toBe(true);
    expect(parsed.event).toBe("");
  });

  // A telemetry state this build has not heard of renders as itself. Dropping
  // the row would hide a hook that exists on the machine.
  it("keeps a telemetry state it does not recognise", () => {
    const parsed = RuntimeHookSchema.parse({
      id: "h-1",
      hook_name: "x",
      telemetry: "throttled",
    });
    expect(parsed.telemetry).toBe("throttled");
  });

  // Identity has no sensible default: a hook with no name cannot be shown to
  // anyone, so it is not a degraded row, it is not a row.
  it("rejects a hook with no name", () => {
    expect(() => RuntimeHookSchema.parse({ id: "h-1" })).toThrow();
  });

  it("tolerates fields it has never seen", () => {
    const parsed = RuntimeHookSchema.parse({
      id: "h-1",
      hook_name: "x",
      timeout_seconds: 30,
    });
    expect(parsed.hook_name).toBe("x");
  });
});

describe("RuntimeHookGroupSchema", () => {
  it("parses a well-formed group", () => {
    const parsed = RuntimeHookGroupSchema.parse({
      runtime_id: "r-1",
      name: "Claude (mac)",
      provider: "claude",
      host: "mac · darwin-arm64",
      status: "online",
      last_seen_at: "2026-08-24T12:05:00Z",
      observed_at: "2026-08-24T12:05:00Z",
      supported: false,
      hooks: [],
    });
    expect(parsed.supported).toBe(false);
    expect(parsed.hooks).toEqual([]);
  });

  // Guessing `false` would tell someone their machine cannot run hooks at all,
  // which is a much worse thing to be wrong about than showing an empty list.
  it("defaults a missing supported flag to true", () => {
    const parsed = RuntimeHookGroupSchema.parse({ runtime_id: "r-1" });
    expect(parsed.supported).toBe(true);
    expect(parsed.status).toBe("offline");
    expect(parsed.observed_at).toBeNull();
    expect(parsed.hooks).toEqual([]);
  });

  it("rejects a group with no runtime id", () => {
    expect(() => RuntimeHookGroupSchema.parse({ name: "x" })).toThrow();
  });
});

describe("ListWorkspaceHooksResponseSchema", () => {
  it("defaults a missing runtimes array", () => {
    const parsed = ListWorkspaceHooksResponseSchema.parse({});
    expect(parsed.runtimes).toEqual([]);
  });

  // A response shaped nothing like the contract fails outright; the client's
  // parseWithFallback turns that into the empty inventory rather than a crash.
  it("rejects a response that is not an object", () => {
    expect(() => ListWorkspaceHooksResponseSchema.parse("nope")).toThrow();
    expect(() =>
      ListWorkspaceHooksResponseSchema.parse({ runtimes: "nope" }),
    ).toThrow();
  });
});
