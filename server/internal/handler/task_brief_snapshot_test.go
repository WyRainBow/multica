package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The brief is rendered fresh from current data on every run, so replaying it
// later answers "what would this issue produce now" rather than "what did that
// run see". Once a spec or a decision has moved, those are different documents
// — and the second is the question asked when a run went wrong.

func postBriefSnapshot(t *testing.T, taskID, brief string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"brief": brief})
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/brief-snapshot",
		nil, testWorkspaceID, batchClaimTestDaemonID)
	req.Body = http.NoBody
	req = req.WithContext(req.Context())
	r := httptest.NewRequest("POST", "/api/daemon/tasks/"+taskID+"/brief-snapshot", bytes.NewReader(body))
	r = r.WithContext(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.RecordTaskBriefSnapshot(w, r)
	return w
}

func readBriefSnapshot(t *testing.T, taskID string) string {
	t.Helper()
	var raw *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT context->>'brief_snapshot' FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if raw == nil {
		return ""
	}
	return *raw
}

func TestABriefSnapshotSurvivesTheWorkdir(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Brief snapshot rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "Brief snapshot")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rt, issueID)

	const brief = "# Multica Agent Runtime\n\n## Workspace Context\n\n- 规则一"
	if w := postBriefSnapshot(t, taskID, brief); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := readBriefSnapshot(t, taskID); got != brief {
		t.Errorf("snapshot did not survive verbatim:\ngot:  %q\nwant: %q", got, brief)
	}
}

func TestAnEmptySnapshotRecordsNothingRatherThanNothingness(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Empty snapshot rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "Empty snapshot")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rt, issueID)

	// Writing an empty string would record "this run was given nothing", which
	// is a claim. Absence is the honest state.
	if w := postBriefSnapshot(t, taskID, "   "); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := readBriefSnapshot(t, taskID); got != "" {
		t.Errorf("an empty snapshot was recorded as %q", got)
	}
}

func TestARecordedSnapshotIsNotOverwrittenByOtherTaskState(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Snapshot keep rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "Snapshot keep")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rt, issueID)

	// A pre-existing context payload must survive: the snapshot sets one key,
	// it does not own the column.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET context = '{"existing":"keep me"}'::jsonb WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}
	if w := postBriefSnapshot(t, taskID, "the brief"); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var existing *string
	if err := testPool.QueryRow(ctx,
		`SELECT context->>'existing' FROM agent_task_queue WHERE id = $1`, taskID).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing == nil || *existing != "keep me" {
		t.Error("recording a snapshot clobbered an unrelated key in the same column")
	}
	if got := readBriefSnapshot(t, taskID); !strings.Contains(got, "the brief") {
		t.Error("the snapshot itself was lost")
	}
}
