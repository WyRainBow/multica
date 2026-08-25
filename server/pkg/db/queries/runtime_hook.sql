-- name: UpsertRuntimeHook :exec
-- One row of a runtime's hook inventory. The conflict target is
-- idx_runtime_hook_identity, so a re-scan of an unchanged machine rewrites the
-- same row instead of growing the table.
--
-- observed_at is bumped on every scan even when nothing else changed: it is
-- the only thing that says "this was still true a minute ago" as opposed to
-- "this was true whenever the row was written".
INSERT INTO runtime_hook (
    workspace_id, runtime_id, provider, hook_name, event,
    trigger_spec, command_path, enabled, telemetry, last_fired_at, observed_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (workspace_id, runtime_id, provider, hook_name, event)
DO UPDATE SET
    trigger_spec  = EXCLUDED.trigger_spec,
    command_path  = EXCLUDED.command_path,
    enabled       = EXCLUDED.enabled,
    telemetry     = EXCLUDED.telemetry,
    -- A scan that has lost its local telemetry record must not erase a firing
    -- the server already knows about; a scan that carries one always wins.
    last_fired_at = COALESCE(EXCLUDED.last_fired_at, runtime_hook.last_fired_at),
    observed_at   = now(),
    updated_at    = now();

-- name: DeleteRuntimeHooksNotIn :exec
-- The other half of "the server keeps this runtime's LAST FULL inventory":
-- whatever the scan did not report is gone from the machine, so it goes from
-- the table too. Uninstalling a hook has to be visible, and a table that only
-- ever grows cannot show it.
--
-- Keys are hook_name + US + event. An empty key set deletes every row for the
-- runtime, which is the correct reading of a scan that found nothing (or of a
-- provider that has no hook mechanism at all).
DELETE FROM runtime_hook
WHERE workspace_id = @workspace_id
  AND runtime_id = @runtime_id
  AND (hook_name || E'\x1f' || event) <> ALL(@keys::text[]);

-- name: ListRuntimeHooksForWorkspace :many
-- Every hook in the workspace. The caller groups these under the workspace's
-- runtimes and reads status / last_seen_at live from agent_runtime, which is
-- why none of that is copied into runtime_hook: an offline runtime still shows
-- its last inventory, next to an honest liveness signal rather than a stale
-- copy of one.
--
-- The join carries no columns. It is there to drop rows whose runtime is gone,
-- so a teardown that missed its cleanup cannot surface as a phantom group.
SELECT runtime_hook.*
FROM runtime_hook
JOIN agent_runtime ON agent_runtime.id = runtime_hook.runtime_id
WHERE runtime_hook.workspace_id = $1
ORDER BY runtime_hook.event ASC, runtime_hook.hook_name ASC;

-- name: DeleteRuntimeHooksByRuntime :exec
-- Runtime teardown. No FK does this for us on purpose.
DELETE FROM runtime_hook WHERE runtime_id = $1;
