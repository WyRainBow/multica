/**
 * The hook inventory: what each machine's agent CLI is configured to run on
 * Multica's behalf, and whether anyone has ever seen it run.
 *
 * Read-only in V1. Nothing here writes a user's agent config; the daemon
 * reports what it found on disk and the server keeps the last full inventory
 * per runtime.
 */

/**
 * Telemetry states from the server's closed vocabulary (migration 302). Typed
 * loosely on read: a state a newer backend invents must render as itself
 * rather than drop the row.
 *
 * `never_fired` and `unobserved` are the pair that must never be collapsed.
 * The first is an observation — telemetry was running and the hook stayed
 * quiet. The second is the absence of one — nobody was watching. Showing a
 * hook installed before telemetry existed as "never fired" accuses it of being
 * dead.
 */
export type RuntimeHookTelemetry =
  | "fired"
  | "never_fired"
  | "unobserved"
  | "uncollectable";

export interface RuntimeHook {
  id: string;
  /** Provider-native hook identifier, kept verbatim across the three CLIs. */
  hook_name: string;
  /** Provider-native event name; deliberately not normalized across CLIs. */
  event: string;
  /** The matcher / `if` pattern string itself, never the expanded command. */
  trigger_spec: string;
  /** Normalized script path. Arguments and script bodies are never uploaded. */
  command_path: string;
  enabled: boolean;
  /** One of RuntimeHookTelemetry, but typed as string on read. */
  telemetry: string;
  /** RFC3339, or null when the hook has not been seen firing. */
  last_fired_at: string | null;
  /** RFC3339 — when this row was last confirmed by a scan. */
  observed_at: string;
}

export interface RuntimeHookGroup {
  runtime_id: string;
  name: string;
  provider: string;
  host: string;
  /** From `agent_runtime`, never from the inventory: an offline runtime still
   *  shows the hooks it last reported, it just does not claim they are live. */
  status: string;
  last_seen_at: string | null;
  /** When this runtime's inventory was last confirmed. Null means it has never
   *  reported one — a third answer again, distinct from empty and unsupported. */
  observed_at: string | null;
  /** False means the provider has no hook mechanism. A client that renders
   *  this the same as an empty list sends the user to debug an installation
   *  that was never possible. */
  supported: boolean;
  hooks: RuntimeHook[];
}

export interface ListWorkspaceHooksResponse {
  runtimes: RuntimeHookGroup[];
}
