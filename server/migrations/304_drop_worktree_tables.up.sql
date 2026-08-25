-- The code-progress ledger left the database (COC-348).
--
-- Its source of truth is now a git repository of YAML, one file per card, in
-- agent-progress. That move happened because the account answers questions
-- asked from inside a checkout — where is the code, who is driving it, what
-- happens next — often before anyone knows which workspace they are in, and
-- sometimes with the product not running at all. A ledger that only exists
-- inside one Postgres instance dies with that instance, which is exactly what
-- happened on 2026-08-24.
--
-- These two tables have had no reader since that commit: the handlers read the
-- repository, and the workspace-delete sweep dropped its two CTEs in the same
-- change. The three rows they held were migrated before this runs, and a
-- data-only dump of both was taken as well.
--
-- Irreversible, and authorised as such. The down migration recreates the shape
-- but cannot bring the rows back.
DROP TABLE IF EXISTS worktree_entry;
DROP TABLE IF EXISTS worktree;
