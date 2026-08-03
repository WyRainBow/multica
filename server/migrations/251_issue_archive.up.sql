-- Archiving is orthogonal to status: status says how the work ended (done /
-- cancelled / blocked), archiving says whether the card should still be in
-- view. Folding it into status would destroy the first answer to record the
-- second. Mirrors agent.archived_at / squad.archived_at.
ALTER TABLE issue ADD COLUMN archived_at TIMESTAMPTZ;
ALTER TABLE issue ADD COLUMN archived_by UUID;
