-- A retro is what someone wants to remember AFTER a requirement is done:
-- the technique that worked, the trap that cost a day, the thing worth
-- reusing next time.
--
-- Deliberately NOT an issue field and NOT a skill:
--   * an issue's description says what to build; mixing "what we learned"
--     into it buries both, and one requirement can leave several unrelated
--     lessons behind — hence a table, not a column;
--   * `skill` is an agent capability bundle (see agent_skill / skill_file),
--     machine-executable instructions rather than something a person reads
--     back months later.
--
-- issue_id is nullable on purpose: a lesson can come from reading, from a
-- production incident, from anywhere. Tying every retro to a requirement
-- would make the ones that matter most impossible to record.
CREATE TABLE retro (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- The requirement this came out of, when there is one.
    issue_id     UUID,
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    title        TEXT NOT NULL,
    content      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
