-- COC-282: delivery-verification receipts. Done is gated on "a delivery
-- check happened", not on a hand-maintained branch_status string. The
-- fingerprint snapshots the git.* metadata keys at verification time; the
-- done gate compares it against current metadata, so any changed declaration
-- invalidates old receipts without deleting history.
CREATE TABLE IF NOT EXISTS issue_delivery_receipt (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id UUID,
    result TEXT NOT NULL CHECK (result IN ('merged', 'delivered_without_mr', 'abandoned', 'unknown')),
    reason TEXT NOT NULL DEFAULT '',
    fingerprint TEXT NOT NULL,
    delivery_ref TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
