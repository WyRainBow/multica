package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// scopedTo puts the request in a workspace the way the middleware does, so the
// endpoint's scoping check is exercised rather than bypassed.
func scopedTo(r *http.Request, workspaceID string) *http.Request {
	return r.WithContext(middleware.SetMemberContext(r.Context(), workspaceID, db.Member{}))
}

// The write was built first and looked finished. A record nobody can retrieve
// is worse than none: the answer seems available right up to the moment
// somebody needs it.

func getBriefResponse(t *testing.T, taskID string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	// Through the real middleware rather than around it: the scoping is half
	// of what this endpoint does, and a test that injects the workspace id
	// directly would never exercise it.
	r := scopedTo(withURLParam(newRequest("GET", "/api/tasks/"+taskID+"/brief", nil), "taskId", taskID), testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetTaskBriefSnapshot(w, r)
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	return w, body
}

func TestARecordedBriefCanBeReadBack(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Brief read rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "Brief read")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rt, issueID)

	const brief = "# Multica Agent Runtime\n\n## Workspace Context\n\n- 规则一"
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET context = jsonb_build_object('brief_snapshot', $1::text) WHERE id = $2
	`, brief, taskID); err != nil {
		t.Fatal(err)
	}

	w, body := getBriefResponse(t, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body["recorded"] != true {
		t.Fatalf("recorded = %v, want true: %v", body["recorded"], body)
	}
	if got, _ := body["brief"].(string); got != brief {
		t.Errorf("brief did not survive verbatim:\ngot:  %q\nwant: %q", got, brief)
	}
}

func TestARunWithNoBriefSaysSoInsteadOfReturningEmpty(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// A run predating the snapshot and a run whose report never landed both
	// land here. An empty string would read as "it was given nothing", which
	// is a claim about the run rather than about the record.
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "No brief rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "No brief")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rt, issueID)

	w, body := getBriefResponse(t, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body["recorded"] != false {
		t.Errorf("recorded = %v, want false", body["recorded"])
	}
	if _, present := body["brief"]; present {
		t.Error("an absent brief must not be reported as an empty one")
	}
}

func TestABriefIsScopedToItsOwnWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// A brief carries the workspace's rules and its issue's documents, so it is
	// scoped exactly as tightly as the task it belongs to.
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Scoped brief rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "Scoped brief")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rt, issueID)

	// Someone else's workspace.
	r := scopedTo(withURLParam(newRequest("GET", "/api/tasks/"+taskID+"/brief", nil), "taskId", taskID),
		"00000000-0000-0000-0000-000000000001")
	w := httptest.NewRecorder()
	testHandler.GetTaskBriefSnapshot(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for another workspace, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "brief") {
		t.Error("the response leaked the field name to a caller who may not read it")
	}
}
