-- When the issue's CURRENT status was set.
--
-- The history already lives in activity_log — every change writes
-- {"from": ..., "to": ...} with a timestamp — but a log answers questions one
-- issue at a time. "Everything finished this week", "sort by when it was
-- done", "how long has this been in review" all need a column, because they
-- are sorts and filters over the whole list.
--
-- Not `updated_at`: that moves when a label changes or a comment lands, so it
-- answers "when was this last touched", which is a different question and
-- usually a misleading answer to this one.
--
-- Backfilled from created_at rather than left NULL: every issue has had its
-- current status since at least the moment it was created, so that is the
-- earliest defensible answer, and it keeps sorts from bunching every existing
-- row into a null group. Rows whose real transition is in activity_log are
-- corrected by the next status change.
ALTER TABLE issue ADD COLUMN status_changed_at TIMESTAMPTZ;
UPDATE issue SET status_changed_at = created_at WHERE status_changed_at IS NULL;
