-- One hook a runtime actually has installed, as last observed on that machine.
--
-- This is an inventory, not a configuration: V1 only reads what the user's
-- agent CLIs already have on disk and never writes those files back. The row
-- exists so "is the guard actually installed on that machine, and has it ever
-- run" stops being a question you answer by SSHing somewhere.
--
-- Only Multica-related hooks are recorded. The daemon filters by the hook's
-- script path before anything leaves the machine, and it reports the pattern
-- (matcher / if) and the normalized script path only -- never command
-- arguments, never an expanded command line, never environment values, never
-- the script body.
--
-- telemetry is four values, and the split between the middle two is the whole
-- point:
--   fired          observed firing at least once; last_fired_at carries when
--   never_fired    telemetry IS in place for this hook and it has not fired
--   unobserved     telemetry is NOT in place for this hook yet (the default) --
--                  a hook installed before the telemetry path existed has not
--                  "never fired", nobody was watching
--   uncollectable  this machine cannot report firing at all
-- Defaulting to unobserved means a hook is never accused of being dead merely
-- because we started counting after it was installed.
--
-- Online / offline / stale is NOT copied here. That lives on agent_runtime
-- (status, last_seen_at); duplicating it would create a second, slower truth.
-- observed_at says when this row was last confirmed by a scan, which is a
-- different fact and the only one this table owns.
--
-- No foreign keys, per repository rule. workspace / runtime teardown deletes
-- these rows explicitly in the same transaction as the parent delete.
CREATE TABLE runtime_hook (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    runtime_id    UUID NOT NULL,
    provider      TEXT NOT NULL,
    hook_name     TEXT NOT NULL,
    event         TEXT NOT NULL,
    -- The matcher / if pattern string itself, verbatim. Empty when the
    -- provider's entry carried none.
    trigger_spec  TEXT NOT NULL DEFAULT '',
    -- Normalized script path with the user's home collapsed to `~`. The
    -- argument list is deliberately not part of it.
    command_path  TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    telemetry     TEXT NOT NULL DEFAULT 'unobserved'
                  CHECK (telemetry IN ('fired', 'never_fired', 'unobserved', 'uncollectable')),
    last_fired_at TIMESTAMPTZ,
    observed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
