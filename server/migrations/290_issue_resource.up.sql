-- An external page attached to one issue: a design doc, a meeting note, a
-- vendor page — anything whose home is outside Multica.
--
-- A table rather than a line in the description. A link written into the body
-- cannot be listed, counted, or removed without editing prose, and it
-- disappears entirely once the issue is finished and the body is frozen.
--
-- No resource_type + JSONB, unlike project_resource. That table is polymorphic
-- because github_repo and local_directory genuinely differ in shape; a link has
-- one shape, and borrowing the polymorphism would import the complexity without
-- the reason for it.
--
-- title is the reader's label, not the page's. Fetching a title means handling
-- auth walls, timeouts and private documents — and the documents most worth
-- attaching here (Feishu, internal wikis) are exactly the ones that return a
-- login page to an anonymous fetch. Typing a title always works.
CREATE TABLE issue_resource (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id     UUID NOT NULL,
    url          TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    -- Same convention as issue.position: a step of 1000 leaves room to insert
    -- between two rows without renumbering their neighbours.
    position     INT  NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
