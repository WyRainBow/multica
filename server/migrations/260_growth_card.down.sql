DROP TABLE IF EXISTS growth_card;

CREATE TABLE retro (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id     UUID,
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    title        TEXT NOT NULL,
    content      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
