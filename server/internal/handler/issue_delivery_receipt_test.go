package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setIssueMetadata(t *testing.T, issueID string, meta map[string]any) {
	t.Helper()
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET metadata = $1 WHERE id = $2`, raw, issueID); err != nil {
		t.Fatalf("set metadata: %v", err)
	}
}

func postReceipt(t *testing.T, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/delivery-receipt", body)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateDeliveryReceipt(w, r)
	return w
}

// A card that declares a delivery cannot reach done without a receipt, and
// can once a matching one exists (COC-282).
func TestDeliveryReceiptGatesDone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "receipt gate", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	setIssueMetadata(t, issueID, map[string]any{
		"git.base_ref":     "feat/base",
		"git.delivery_ref": "feat/delivery",
	})

	blocked := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if blocked.Code != http.StatusConflict {
		t.Fatalf("done without receipt = %d, want 409: %s", blocked.Code, blocked.Body.String())
	}

	created := postReceipt(t, issueID, map[string]any{"result": "merged"})
	if created.Code != http.StatusCreated {
		t.Fatalf("record receipt = %d: %s", created.Code, created.Body.String())
	}

	passed := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if passed.Code != http.StatusOK {
		t.Fatalf("done with valid receipt = %d: %s", passed.Code, passed.Body.String())
	}
}

// Changing any bound declaration invalidates the receipt — done asks for a
// re-verify instead of trusting a snapshot of the previous state.
func TestDeliveryReceiptStaleAfterDeclarationChange(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "receipt stale", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	setIssueMetadata(t, issueID, map[string]any{"git.delivery_ref": "feat/one"})

	if created := postReceipt(t, issueID, map[string]any{"result": "merged"}); created.Code != http.StatusCreated {
		t.Fatalf("record receipt = %d: %s", created.Code, created.Body.String())
	}

	setIssueMetadata(t, issueID, map[string]any{"git.delivery_ref": "feat/two"})
	stale := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if stale.Code != http.StatusConflict {
		t.Fatalf("done with stale receipt = %d, want 409: %s", stale.Code, stale.Body.String())
	}

	if created := postReceipt(t, issueID, map[string]any{"result": "delivered_without_mr"}); created.Code != http.StatusCreated {
		t.Fatalf("re-verify = %d: %s", created.Code, created.Body.String())
	}
	passed := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if passed.Code != http.StatusOK {
		t.Fatalf("done after re-verify = %d: %s", passed.Code, passed.Body.String())
	}
}

// Cards with no delivery declaration keep the old done path — the gate only
// covers cards that claim code whereabouts.
func TestDeliveryReceiptNotGatedWithoutDeclaration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "receipt n/a", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	passed := updateIssueForDoneReview(t, issueID, map[string]any{"status": "done"})
	if passed.Code != http.StatusOK {
		t.Fatalf("done without declaration = %d: %s", passed.Code, passed.Body.String())
	}

	if refused := postReceipt(t, issueID, map[string]any{"result": "merged"}); refused.Code != http.StatusConflict {
		t.Fatalf("receipt without declaration = %d, want 409: %s", refused.Code, refused.Body.String())
	}
}

func TestDeliveryReceiptUnknownRequiresReason(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "receipt unknown", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	setIssueMetadata(t, issueID, map[string]any{"git.delivery_ref": "feat/x"})

	if refused := postReceipt(t, issueID, map[string]any{"result": "unknown"}); refused.Code != http.StatusBadRequest {
		t.Fatalf("unknown without reason = %d, want 400: %s", refused.Code, refused.Body.String())
	}
	if ok := postReceipt(t, issueID, map[string]any{"result": "unknown", "reason": "MR closed unmerged; outcome unconfirmed"}); ok.Code != http.StatusCreated {
		t.Fatalf("unknown with reason = %d: %s", ok.Code, ok.Body.String())
	}
}
