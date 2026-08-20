-- name: CreateIssueDeliveryReceipt :one
INSERT INTO issue_delivery_receipt (
    workspace_id, issue_id, actor_type, actor_id, result, reason,
    fingerprint, delivery_ref, evidence
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetLatestIssueDeliveryReceipt :one
SELECT * FROM issue_delivery_receipt
WHERE issue_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;
