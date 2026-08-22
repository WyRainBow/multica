package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestWorkspaceDeleteRemovesDeliveryReceipts proves the cascade, not the
// manifest.
//
// The manifest is a coverage checklist: adding a row to it makes the drift
// test pass whether or not DeleteWorkspace actually touches the table. That is
// how issue_delivery_receipt could sit unclassified while its rows outlived
// every workspace that owned them — a checklist entry is a claim, and this is
// the check that the claim is true.
func TestWorkspaceDeleteRemovesDeliveryReceipts(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var wsID, issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('receipt cascade', 'receipt-cascade-'||substr(md5(random()::text),1,8), 'RCP')
		RETURNING id`).Scan(&wsID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_delivery_receipt WHERE workspace_id = $1`, wsID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, wsID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, number, title, status, priority, creator_type, creator_id)
		VALUES ($1, 1, 'shipped', 'done', 'none', 'member', $2)
		RETURNING id`, wsID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_delivery_receipt (workspace_id, issue_id, actor_type, actor_id, result, fingerprint)
		VALUES ($1, $2, 'member', $3, 'delivered_without_mr', 'test-fingerprint')`, wsID, issueID, testUserID); err != nil {
		t.Fatalf("insert receipt: %v", err)
	}

	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		t.Fatal(err)
	}
	if err := testHandler.Queries.DeleteWorkspace(ctx, wsUUID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}

	var left int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue_delivery_receipt WHERE workspace_id = $1`, wsID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d delivery receipt(s) outlived their workspace", left)
	}
}
