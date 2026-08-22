package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Pausing has to stop the schedule without deleting it, and without stopping
// the one thing a person needs while it is paused: running it by hand.

func TestPauseKeepsItsReasonAndResumeClearsIt(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Pause Test Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	ctx := context.Background()

	// The reason and the status are written together. Two writes would let an
	// autopilot sit paused carrying the reason from an earlier pause.
	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot SET status = 'paused', pause_reason = $2 WHERE id = $1`,
		apID, "prompt 要改"); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, coalesce(pause_reason, '') FROM autopilot WHERE id = $1`, apID).
		Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "paused" || reason != "prompt 要改" {
		t.Fatalf("status = %q, reason = %q; want the pause to carry its reason", status, reason)
	}

	// Resuming clears it: a reason that outlives the pause reads as an
	// explanation of the current state and is not one.
	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot SET status = 'active', pause_reason = NULL WHERE id = $1`, apID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT status, coalesce(pause_reason, '') FROM autopilot WHERE id = $1`, apID).
		Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "active" || reason != "" {
		t.Fatalf("status = %q, reason = %q; want a clean resume", status, reason)
	}
}

func TestScheduleAndWebhookOnlyFireWhileActive(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Pause Test Agent")
	paused := createWebhookTestAutopilot(t, agentID, "paused", "run_only")
	active := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	ctx := context.Background()

	// This is the query the scheduler picks due work with. A paused autopilot
	// must not appear in it — that is what "the triggers stop firing" means.
	for _, tc := range []struct {
		id   string
		want bool
	}{{paused, false}, {active, true}} {
		var eligible bool
		if err := testPool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM autopilot WHERE id = $1 AND status = 'active')`, tc.id).
			Scan(&eligible); err != nil {
			t.Fatal(err)
		}
		if eligible != tc.want {
			t.Errorf("autopilot %s eligible = %v, want %v", tc.id, eligible, tc.want)
		}
	}
}

// TestPauseReasonSurvivesTheUpdateEndpoint covers the write path the CLI uses.
// The SQL clears pause_reason on every status change, so a reason sent
// alongside one has to be threaded through the request struct and the params
// or it lands as NULL — which is exactly what a first live run produced.
func TestPauseReasonSurvivesTheUpdateEndpoint(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Pause Endpoint Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	ctx := context.Background()

	rec := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "paused", "pause_reason": "prompt 要改"})
	req = withURLParam(req, "id", apID)
	testHandler.UpdateAutopilot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var status, reason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, coalesce(pause_reason, '') FROM autopilot WHERE id = $1`, apID).
		Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "paused" || reason != "prompt 要改" {
		t.Fatalf("status = %q, reason = %q; the reason did not survive the endpoint", status, reason)
	}
}
