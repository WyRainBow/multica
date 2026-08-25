package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// createHookTestRuntime makes a runtime with a chosen provider so a test can
// exercise both a provider that has a hook mechanism and one that does not.
func createHookTestRuntime(t *testing.T, provider string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	name := fmt.Sprintf("hook-test-%s-%d", provider, time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, name, provider, "hook-test-host", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_hook WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func reportHooks(t *testing.T, runtimeID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/hooks", body, testWorkspaceID, "")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ReportRuntimeHooks(w, req)
	return w
}

func listHooks(t *testing.T) map[string]RuntimeHookGroupResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/hooks", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceHooks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceHooks: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Runtimes []RuntimeHookGroupResponse `json:"runtimes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make(map[string]RuntimeHookGroupResponse, len(resp.Runtimes))
	for _, group := range resp.Runtimes {
		out[group.RuntimeID] = group
	}
	return out
}

func claudeInventory() []map[string]any {
	return []map[string]any{
		{
			"hook_name":    "multica-worktree-sync.sh",
			"event":        "PostToolUse",
			"trigger_spec": "Bash",
			"command_path": "~/.claude/hooks/multica-worktree-sync.sh",
			"enabled":      true,
			"telemetry":    "unobserved",
		},
		{
			"hook_name":    "multica-branch-register.sh",
			"event":        "PreToolUse",
			"trigger_spec": "Bash",
			"command_path": "~/.claude/hooks/multica-branch-register.sh",
			"enabled":      true,
			"telemetry":    "unobserved",
		},
	}
}

func TestRuntimeHooks_ReportThenReadBack(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")

	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": claudeInventory()}); w.Code != http.StatusOK {
		t.Fatalf("report: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	group, ok := listHooks(t)[runtimeID]
	if !ok {
		t.Fatal("runtime missing from the hook listing")
	}
	if !group.Supported {
		t.Fatal("claude must be reported as supporting hooks")
	}
	if len(group.Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(group.Hooks))
	}
	if group.Status != "online" || group.LastSeenAt == nil {
		t.Fatalf("liveness must come from agent_runtime, got status=%q last_seen_at=%v", group.Status, group.LastSeenAt)
	}
	if group.ObservedAt == nil {
		t.Fatal("a scanned runtime must carry observed_at")
	}
	if group.Hooks[0].Telemetry != "unobserved" {
		t.Fatalf("expected unobserved, got %q", group.Hooks[0].Telemetry)
	}
	if group.Hooks[0].LastFiredAt != nil {
		t.Fatal("an unobserved hook must carry no last_fired_at")
	}
}

// The same machine scanned twice is the same inventory, not twice the rows.
func TestRuntimeHooks_RepeatedReportDoesNotDuplicate(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")

	for i := 0; i < 3; i++ {
		if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": claudeInventory()}); w.Code != http.StatusOK {
			t.Fatalf("report %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM runtime_hook WHERE runtime_id = $1`, runtimeID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows after 3 identical reports, got %d", count)
	}
}

// Uninstalling a hook has to become visible. An inventory that only grows is
// not an inventory.
func TestRuntimeHooks_VanishedHookIsRemoved(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")

	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": claudeInventory()}); w.Code != http.StatusOK {
		t.Fatalf("first report: %d %s", w.Code, w.Body.String())
	}
	remaining := claudeInventory()[:1]
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": remaining}); w.Code != http.StatusOK {
		t.Fatalf("second report: %d %s", w.Code, w.Body.String())
	}

	group := listHooks(t)[runtimeID]
	if len(group.Hooks) != 1 {
		t.Fatalf("expected 1 hook after the second scan, got %d", len(group.Hooks))
	}
	if group.Hooks[0].HookName != "multica-worktree-sync.sh" {
		t.Fatalf("wrong hook survived: %q", group.Hooks[0].HookName)
	}

	// An empty scan clears the runtime entirely.
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": []any{}}); w.Code != http.StatusOK {
		t.Fatalf("empty report: %d %s", w.Code, w.Body.String())
	}
	group = listHooks(t)[runtimeID]
	if len(group.Hooks) != 0 {
		t.Fatalf("expected an empty inventory, got %d hooks", len(group.Hooks))
	}
	if !group.Supported {
		t.Fatal("an empty inventory is not the same as an unsupported provider")
	}
}

// The distinction the whole feature rests on, at the API boundary: a provider
// with no hook mechanism must not look like one with nothing installed.
func TestRuntimeHooks_UnsupportedProviderIsNotAnEmptyList(t *testing.T) {
	claudeID := createHookTestRuntime(t, "claude")
	// handler_test_runtime is not a provider with a hook mechanism.
	otherID := createHookTestRuntime(t, "handler_test_runtime")

	if w := reportHooks(t, claudeID, map[string]any{"supported": true, "hooks": []any{}}); w.Code != http.StatusOK {
		t.Fatalf("claude report: %d %s", w.Code, w.Body.String())
	}
	if w := reportHooks(t, otherID, map[string]any{"supported": false, "hooks": []any{}}); w.Code != http.StatusOK {
		t.Fatalf("other report: %d %s", w.Code, w.Body.String())
	}

	groups := listHooks(t)
	empty, unsupported := groups[claudeID], groups[otherID]
	if !empty.Supported || len(empty.Hooks) != 0 {
		t.Fatalf("claude with no hooks: expected supported=true and an empty list, got %+v", empty)
	}
	if unsupported.Supported {
		t.Fatalf("a provider with no hook mechanism must report supported=false, got %+v", unsupported)
	}
	if len(unsupported.Hooks) != 0 {
		t.Fatalf("expected no hooks for an unsupported provider, got %d", len(unsupported.Hooks))
	}
}

// An offline runtime keeps its last inventory. What it loses is the claim that
// the inventory was confirmed just now.
func TestRuntimeHooks_OfflineRuntimeKeepsLastInventory(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": claudeInventory()}); w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}

	staleSince := time.Now().Add(-2 * time.Hour)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_runtime SET status = 'offline', last_seen_at = $2 WHERE id = $1`, runtimeID, staleSince); err != nil {
		t.Fatalf("mark offline: %v", err)
	}

	group := listHooks(t)[runtimeID]
	if group.Status != "offline" {
		t.Fatalf("expected offline, got %q", group.Status)
	}
	if len(group.Hooks) != 2 {
		t.Fatalf("an offline runtime must keep its last inventory, got %d hooks", len(group.Hooks))
	}
	if group.ObservedAt == nil {
		t.Fatal("observed_at must still say when the inventory was last confirmed")
	}
}

func TestRuntimeHooks_FiredCarriesLastFiredAt(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")
	firedAt := time.Now().Add(-3 * time.Minute).UTC().Truncate(time.Second)

	body := map[string]any{"supported": true, "hooks": []map[string]any{{
		"hook_name":     "multica-agent-guard.sh",
		"event":         "PreToolUse",
		"trigger_spec":  "Bash",
		"command_path":  "~/.claude/hooks/multica-agent-guard.sh",
		"enabled":       true,
		"telemetry":     "fired",
		"last_fired_at": firedAt.Format(time.RFC3339),
	}}}
	if w := reportHooks(t, runtimeID, body); w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}

	group := listHooks(t)[runtimeID]
	if len(group.Hooks) != 1 || group.Hooks[0].Telemetry != "fired" {
		t.Fatalf("expected one fired hook, got %+v", group.Hooks)
	}
	if group.Hooks[0].LastFiredAt == nil {
		t.Fatal("a fired hook must carry last_fired_at")
	}
	got, err := time.Parse(time.RFC3339, *group.Hooks[0].LastFiredAt)
	if err != nil {
		t.Fatalf("last_fired_at not RFC3339: %q", *group.Hooks[0].LastFiredAt)
	}
	if !got.Equal(firedAt) {
		t.Fatalf("last_fired_at %s does not match %s", got, firedAt)
	}
}

// A later scan that has lost its local telemetry record must not erase a
// firing the server already knows about.
func TestRuntimeHooks_LaterScanDoesNotEraseAKnownFiring(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")
	firedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	entry := map[string]any{
		"hook_name":    "multica-agent-guard.sh",
		"event":        "PreToolUse",
		"command_path": "~/.claude/hooks/multica-agent-guard.sh",
		"enabled":      true,
	}

	fired := map[string]any{}
	for k, v := range entry {
		fired[k] = v
	}
	fired["telemetry"] = "fired"
	fired["last_fired_at"] = firedAt.Format(time.RFC3339)
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": []map[string]any{fired}}); w.Code != http.StatusOK {
		t.Fatalf("first report: %d %s", w.Code, w.Body.String())
	}

	forgotten := map[string]any{}
	for k, v := range entry {
		forgotten[k] = v
	}
	forgotten["telemetry"] = "uncollectable"
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": []map[string]any{forgotten}}); w.Code != http.StatusOK {
		t.Fatalf("second report: %d %s", w.Code, w.Body.String())
	}

	group := listHooks(t)[runtimeID]
	if group.Hooks[0].LastFiredAt == nil {
		t.Fatal("a known firing must survive a later scan that cannot see it")
	}
}

// Malformed reports are the daemon's mistake, not the server's. Every one of
// these must be a 4xx: a 500 would make the daemon retry a body that can never
// succeed.
func TestRuntimeHooks_MalformedReportsAre400(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")

	cases := []struct {
		name string
		body any
	}{
		{"unknown telemetry value", map[string]any{"hooks": []map[string]any{{
			"hook_name": "multica-a.sh", "event": "PreToolUse", "telemetry": "probably_fired",
		}}}},
		{"fired with no timestamp", map[string]any{"hooks": []map[string]any{{
			"hook_name": "multica-a.sh", "event": "PreToolUse", "telemetry": "fired",
		}}}},
		{"unparsable timestamp", map[string]any{"hooks": []map[string]any{{
			"hook_name": "multica-a.sh", "event": "PreToolUse", "telemetry": "fired", "last_fired_at": "yesterday",
		}}}},
		{"missing hook_name", map[string]any{"hooks": []map[string]any{{"event": "PreToolUse"}}}},
		{"missing event", map[string]any{"hooks": []map[string]any{{"hook_name": "multica-a.sh"}}}},
		{"oversized hook_name", map[string]any{"hooks": []map[string]any{{
			"hook_name": strings.Repeat("x", 201), "event": "PreToolUse",
		}}}},
		{"duplicate identity", map[string]any{"hooks": []map[string]any{
			{"hook_name": "multica-a.sh", "event": "PreToolUse"},
			{"hook_name": "multica-a.sh", "event": "PreToolUse"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := reportHooks(t, runtimeID, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}

	t.Run("body that is not JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/hooks", nil, testWorkspaceID, "")
		req.Body = http.NoBody
		req = withURLParam(req, "runtimeId", runtimeID)
		testHandler.ReportRuntimeHooks(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("runtime id that is not a uuid", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/not-a-uuid/hooks",
			map[string]any{"hooks": []any{}}, testWorkspaceID, "")
		req = withURLParam(req, "runtimeId", "not-a-uuid")
		testHandler.ReportRuntimeHooks(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// A rejected report must leave the previous inventory intact rather than
// half-replacing it.
func TestRuntimeHooks_RejectedReportLeavesInventoryIntact(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": claudeInventory()}); w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}

	bad := map[string]any{"hooks": []map[string]any{
		{"hook_name": "multica-worktree-sync.sh", "event": "PostToolUse"},
		{"hook_name": "multica-broken.sh", "event": "PreToolUse", "telemetry": "nonsense"},
	}}
	if w := reportHooks(t, runtimeID, bad); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	group := listHooks(t)[runtimeID]
	if len(group.Hooks) != 2 {
		t.Fatalf("a rejected report must not change the inventory, got %d hooks", len(group.Hooks))
	}
}

// No FK removes these rows, so the delete path has to.
func TestRuntimeHooks_RuntimeDeleteClearsInventory(t *testing.T) {
	runtimeID := createHookTestRuntime(t, "claude")
	if w := reportHooks(t, runtimeID, map[string]any{"supported": true, "hooks": claudeInventory()}); w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM runtime_hook WHERE runtime_id = $1`, runtimeID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("runtime teardown left %d hook rows behind", count)
	}
}
