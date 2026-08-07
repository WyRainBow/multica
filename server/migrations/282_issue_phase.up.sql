-- A phase is one station in a requirement's life: 开始 / 已冻结 / 实施中 /
-- 等待部署 / 结束. It is a CONTAINER, not a status — what happened during that
-- stretch hangs underneath it.
--
-- Deliberately not `issue.stage`, which already exists and means something
-- else entirely: that is a barrier group among SIBLING sub-issues, used to
-- decide when to wake the parent's assignee. It schedules; this one archives.
-- An issue sitting in the 实施中 phase can have stage 1 and stage 2 sub-issues
-- underneath it. Same word in English, orthogonal concepts — hence `phase`.
--
-- Also not `status`. Status is a single current value: it answers "where is
-- this now" and forgets everything it passed through. A phase is a row that
-- stays, holding the comments and decisions made while the issue was in it.
CREATE TABLE issue_phase (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id     UUID NOT NULL,
    name         TEXT NOT NULL,
    -- Order along the track, not a status value. Sparse (0, 1000, 2000...) so
    -- inserting a station between two others needs no rewrite of its
    -- neighbours — same convention as issue.position.
    position     INTEGER NOT NULL DEFAULT 0,
    -- Derived from transitions rather than typed by hand, borrowed from
    -- Plane's `_sync_completed_at`: a timestamp somebody has to remember to
    -- fill in is a timestamp that is wrong.
    entered_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which phase a comment belongs to. Nullable, and that is the whole migration
-- story for existing data: every comment written before phases existed simply
-- has no phase, and still reads exactly as it did.
ALTER TABLE comment ADD COLUMN phase_id UUID;
