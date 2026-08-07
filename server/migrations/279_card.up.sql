-- A card is a short note someone wants to keep: what a piece of work taught
-- them, a link worth remembering, a thing to look at later.
--
-- Free-form on purpose. An earlier version of this asked eight named
-- questions; the questions turned out to be a guess at what a note should
-- contain, and a guessed structure is worse than none — it makes the writer
-- fight the form instead of writing. Title plus body, and the body is the
-- same Markdown an issue description uses.
--
-- A table rather than an issue field: a note has to outlive whatever produced
-- it, and one requirement can leave several unrelated notes behind.
--
-- issue_id is nullable. A note can come from reading, from an incident, from
-- nowhere in particular; requiring a requirement would exclude the ones most
-- worth keeping.
CREATE TABLE card (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id     UUID,
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
