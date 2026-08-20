package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Delivery receipts gate done on "a delivery check happened" instead of a
// hand-maintained branch-status string (COC-282). The receipt snapshots the
// issue's delivery metadata as a fingerprint; the done gate only accepts a
// receipt whose fingerprint still matches, so any changed declaration
// (new delivery_ref, new MR, retargeted base) invalidates prior receipts
// without rewriting history.

var deliveryReceiptResults = map[string]bool{
	"merged":               true,
	"delivered_without_mr": true,
	"abandoned":            true,
	"unknown":              true,
}

// deliveryFingerprintKeys are the metadata keys a receipt is bound to, in
// canonical order. Legacy spellings fold into their namespaced successors so
// a card written before the key migration still verifies.
var deliveryFingerprintKeys = [][2]string{
	{"git.repo_resource_id", ""},
	{"git.base_ref", "baseline_ref"},
	{"git.base_sha", ""},
	{"git.delivery_ref", "delivery_branch"},
	{"git.delivery_sha", ""},
	{"vcs.primary_mr_url", "mr_url"},
}

// computeDeliveryFingerprint renders the issue's delivery declarations to a
// stable string. Empty metadata yields "" — cards without any delivery
// declaration are not gated (nothing to verify).
func computeDeliveryFingerprint(issue db.Issue) string {
	var meta map[string]any
	if len(issue.Metadata) > 0 {
		if err := json.Unmarshal(issue.Metadata, &meta); err != nil {
			meta = nil
		}
	}
	parts := make([]string, 0, len(deliveryFingerprintKeys))
	for _, keys := range deliveryFingerprintKeys {
		v, ok := meta[keys[0]]
		if !ok && keys[1] != "" {
			v, ok = meta[keys[1]]
		}
		if !ok || v == nil {
			continue
		}
		parts = append(parts, keys[0]+"="+stringValue(v))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strings.TrimSuffix(strings.TrimSuffix(jsonNumber(t), ".0"), "")
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// hasDeliveryDeclaration reports whether the issue claims any delivery at
// all; only those cards are receipt-gated on done.
func hasDeliveryDeclaration(issue db.Issue) bool {
	return computeDeliveryFingerprint(issue) != ""
}

// validDeliveryReceipt returns the latest receipt when it still matches the
// current declarations, and whether one exists at all.
func (h *Handler) validDeliveryReceipt(ctx context.Context, issue db.Issue) (db.IssueDeliveryReceipt, bool, bool) {
	receipt, err := h.Queries.GetLatestIssueDeliveryReceipt(ctx, issue.ID)
	if err != nil {
		return db.IssueDeliveryReceipt{}, false, false
	}
	if receipt.Fingerprint != computeDeliveryFingerprint(issue) {
		return receipt, false, true
	}
	return receipt, true, true
}

type DeliveryReceiptRequest struct {
	Result   string `json:"result"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

type DeliveryReceiptResponse struct {
	ID            string `json:"id"`
	IssueID       string `json:"issue_id"`
	Result        string `json:"result"`
	Reason        string `json:"reason"`
	Fingerprint   string `json:"fingerprint"`
	DeliveryRef   string `json:"delivery_ref"`
	Evidence      string `json:"evidence"`
	CreatedAt     string `json:"created_at"`
	Valid         bool   `json:"valid"`
	InvalidReason string `json:"invalid_reason,omitempty"`
}

// CreateDeliveryReceipt handles POST /api/issues/{id}/delivery-receipt.
// The fingerprint is computed server-side from current metadata — the client
// never asserts it — so a receipt is by construction a snapshot of what was
// declared at verification time.
func (h *Handler) CreateDeliveryReceipt(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID := requestUserID(r)
	actorType, actorIDStr := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	actorID := parseUUID(actorIDStr)

	var req DeliveryReceiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Result = strings.ReplaceAll(strings.TrimSpace(req.Result), "-", "_")
	if !deliveryReceiptResults[req.Result] {
		writeError(w, http.StatusBadRequest, "result must be one of merged, delivered_without_mr, abandoned, unknown")
		return
	}
	if req.Result == "unknown" && strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "result=unknown requires a reason")
		return
	}
	if !hasDeliveryDeclaration(issue) {
		writeError(w, http.StatusConflict, "issue has no delivery declaration (git.* metadata); nothing to verify — set git.delivery_ref first")
		return
	}

	fingerprint := computeDeliveryFingerprint(issue)
	deliveryRef := deliveryRefOf(issue)
	receipt, err := h.Queries.CreateIssueDeliveryReceipt(r.Context(), db.CreateIssueDeliveryReceiptParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   actorType,
		ActorID:     actorID,
		Result:      req.Result,
		Reason:      strings.TrimSpace(req.Reason),
		Fingerprint: fingerprint,
		DeliveryRef: deliveryRef,
		Evidence:    strings.TrimSpace(req.Evidence),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record receipt")
		return
	}
	writeJSON(w, http.StatusCreated, receiptToResponse(receipt, true))
}

// GetDeliveryReceipt handles GET /api/issues/{id}/delivery-receipt.
func (h *Handler) GetDeliveryReceipt(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	receipt, valid, exists := h.validDeliveryReceipt(r.Context(), issue)
	if !exists {
		writeError(w, http.StatusNotFound, "no delivery receipt recorded")
		return
	}
	writeJSON(w, http.StatusOK, receiptToResponse(receipt, valid))
}

// checkDeliveryReceiptForDone is the done-gate half of COC-282: a card that
// declares a delivery needs a receipt that still matches the declaration.
// Returns a 409-ready message when blocked.
func (h *Handler) checkDeliveryReceiptForDone(ctx context.Context, issue db.Issue) (string, bool) {
	if !hasDeliveryDeclaration(issue) {
		return "", false
	}
	receipt, valid, exists := h.validDeliveryReceipt(ctx, issue)
	switch {
	case !exists:
		return "issue declares a delivery (" + deliveryRefOf(issue) + ") but has no delivery receipt. " +
			"Run `multica issue receipt <key> --result merged|delivered_without_mr|abandoned|unknown` " +
			"(add --verify-local <repo> for machine-checked ancestry evidence), then retry done.", true
	case !valid:
		return "delivery receipt is stale: declarations changed after " + timestampToString(receipt.CreatedAt) +
			". Re-verify with `multica issue receipt <key> ...` against the current git.* metadata, then retry done.", true
	}
	return "", false
}

func deliveryRefOf(issue db.Issue) string {
	var meta map[string]any
	if len(issue.Metadata) > 0 {
		if err := json.Unmarshal(issue.Metadata, &meta); err == nil {
			for _, key := range []string{"git.delivery_ref", "delivery_branch"} {
				if v, ok := meta[key]; ok {
					if s, ok := v.(string); ok && s != "" {
						return s
					}
				}
			}
		}
	}
	return "unspecified branch"
}

func receiptToResponse(r db.IssueDeliveryReceipt, valid bool) DeliveryReceiptResponse {
	resp := DeliveryReceiptResponse{
		ID:          uuidToString(r.ID),
		IssueID:     uuidToString(r.IssueID),
		Result:      r.Result,
		Reason:      r.Reason,
		Fingerprint: r.Fingerprint,
		DeliveryRef: r.DeliveryRef,
		Evidence:    r.Evidence,
		CreatedAt:   timestampToString(r.CreatedAt),
		Valid:       valid,
	}
	if !valid {
		resp.InvalidReason = "declarations_changed"
	}
	return resp
}
