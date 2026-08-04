-- A growth card records what one delivery taught the person who delivered it.
--
-- Replaces `retro`, which stored a title and a blob of Markdown. The fields
-- ARE the method: someone working through an Agent can ship a requirement
-- without ever learning why it works, and free prose lets the writer skip
-- exactly the questions that would expose that ("what did I not know",
-- "what did I verify myself"). Named columns make a skipped question visible
-- instead of absent, and are the only shape that answers "which gaps keep
-- coming back" across a month of cards.
--
-- Every field is nullable/empty-able on purpose. A half-filled card is
-- honest data — it says which part of the loop did not happen.
--
-- The old table is dropped rather than migrated: its six rows were distilled
-- knowledge notes, not delivery records, and there is no field of the new
-- shape they map onto.
DROP TABLE IF EXISTS retro;

CREATE TABLE growth_card (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- The delivery this card is about, when it was tracked as an issue.
    -- Nullable: a lesson can come from work that never became a ticket.
    issue_id     UUID,
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    -- 需求 — what was being built. Doubles as the card's display name.
    title        TEXT NOT NULL DEFAULT '',
    -- 系统涉及
    systems      TEXT NOT NULL DEFAULT '',
    -- 我原本不会的东西
    unknowns     TEXT NOT NULL DEFAULT '',
    -- Agent 给出的方案
    agent_plan   TEXT NOT NULL DEFAULT '',
    -- 我确认理解的关键点
    understood   TEXT NOT NULL DEFAULT '',
    -- 我亲自验证了什么
    verified     TEXT NOT NULL DEFAULT '',
    -- 这次真正学会了什么
    learned      TEXT NOT NULL DEFAULT '',
    -- 下次要补的知识 — the field the four-week review reads across cards.
    next_gaps    TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
