package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A claimed issue task must carry the issue's own assets — the stations it
// passes through and the documents written for it. Both reach the agent only
// through this payload; if the handler stops populating them the brief renders
// without the sections and nothing else notices, because every test downstream
// builds its context by hand.

func seedIssuePhaseForClaim(t *testing.T, ctx context.Context, issueID, name string, position int, entered, completed bool) {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_phase (workspace_id, issue_id, name, position, entered_at, completed_at)
		VALUES ($1, $2, $3, $4,
			CASE WHEN $5 THEN now() ELSE NULL END,
			CASE WHEN $6 THEN now() ELSE NULL END)
		RETURNING id
	`, testWorkspaceID, issueID, name, position, entered, completed).Scan(&id); err != nil {
		t.Fatalf("seed phase %q: %v", name, err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue_phase WHERE id = $1`, id) })
}

func seedIssueCardForClaim(t *testing.T, ctx context.Context, issueID, title, kind string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO card (workspace_id, issue_id, author_type, author_id, title, content, kind)
		VALUES ($1, $2, 'member', $3, $4, 'body that must not travel', $5)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID, title, kind).Scan(&id); err != nil {
		t.Fatalf("seed card %q: %v", title, err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM card WHERE id = $1`, id) })
	return id
}

func claimOneIssueTask(t *testing.T, ctx context.Context, name string) AgentTaskResponse {
	t.Helper()
	runtimeID := createClaimReclaimRuntime(t, ctx, name+" rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name)
	seedIssuePhaseForClaim(t, ctx, issueID, "需求梳理", 0, true, true)
	seedIssuePhaseForClaim(t, ctx, issueID, "代码评审", 1, true, false)
	seedIssuePhaseForClaim(t, ctx, issueID, "测试验收", 2, false, false)
	seedIssueCardForClaim(t, ctx, issueID, "spec of record", "SPEC/spec")
	seedIssueCardForClaim(t, ctx, issueID, "R1 代码评审", "SPEC/rounds/R1-代码评审")
	seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	w := postBatchClaim(t, testWorkspaceID, []string{runtimeID}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("claim: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Tasks []AgentTaskResponse `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if len(body.Tasks) != 1 {
		t.Fatalf("expected exactly one claimed task, got %d", len(body.Tasks))
	}
	return body.Tasks[0]
}

func TestClaimedIssueTaskCarriesItsPhases(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	task := claimOneIssueTask(t, context.Background(), "Claim phases")

	if len(task.IssuePhases) != 3 {
		t.Fatalf("expected the issue's three stations, got %d: %+v", len(task.IssuePhases), task.IssuePhases)
	}
	// Track order, not insertion order: the stations are a route.
	for i, want := range []string{"需求梳理", "代码评审", "测试验收"} {
		if task.IssuePhases[i].Name != want {
			t.Errorf("station %d = %q, want %q", i, task.IssuePhases[i].Name, want)
		}
	}
	// The distinction the brief renders as done / CURRENT / not reached has to
	// survive the wire, or every station reads the same.
	if !task.IssuePhases[0].Entered || !task.IssuePhases[0].Completed {
		t.Error("a finished station must arrive entered and completed")
	}
	if !task.IssuePhases[1].Entered || task.IssuePhases[1].Completed {
		t.Error("an open station must arrive entered and not completed")
	}
	if task.IssuePhases[2].Entered || task.IssuePhases[2].Completed {
		t.Error("an unreached station must arrive as neither")
	}
}

func TestClaimedIssueTaskCarriesItsDocumentsWithoutTheirBodies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	task := claimOneIssueTask(t, context.Background(), "Claim docs")

	if len(task.IssueDocs) != 2 {
		t.Fatalf("expected the issue's two documents, got %d: %+v", len(task.IssueDocs), task.IssueDocs)
	}
	byTitle := map[string]IssueDocData{}
	for _, doc := range task.IssueDocs {
		byTitle[doc.Title] = doc
	}
	spec, ok := byTitle["spec of record"]
	if !ok {
		t.Fatalf("the spec document did not travel: %+v", task.IssueDocs)
	}
	if spec.Kind != "SPEC/spec" {
		t.Errorf("kind = %q, want SPEC/spec — the brief groups on it", spec.Kind)
	}
	if spec.ID == "" {
		t.Error("a document with no id cannot be fetched, which is the point of listing it")
	}

	// Bodies must not ride along. Listing titles is what keeps a long spec from
	// costing a context window it may never be read in.
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if strings.Contains(string(raw), "body that must not travel") {
		t.Error("document bodies leaked into the claim payload")
	}
}

func TestClaimedIssueTaskCarriesTheWorkspaceContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Set one rather than reading whatever happens to be there: an empty
	// column would turn this into a skip, and a skipped test reads exactly
	// like a passing one in the output.
	const wsRules = "# 团队通用指令\n\n- 声称「已合入」必须带完整 40 位 commit SHA。"
	var previous *string
	if err := testPool.QueryRow(ctx,
		`SELECT context FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatalf("read workspace context: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET context = $1 WHERE id = $2`, wsRules, testWorkspaceID); err != nil {
		t.Fatalf("set workspace context: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE workspace SET context = $1 WHERE id = $2`, previous, testWorkspaceID)
	})

	task := claimOneIssueTask(t, ctx, "Claim workspace context")

	// The three asset kinds ride the same claim. If this one stopped
	// travelling, every agent in the workspace would lose its shared rules
	// while the issue-scoped sections kept working — the hardest version of
	// this failure to notice, because the brief still looks populated.
	if task.WorkspaceContext != wsRules {
		t.Errorf("workspace context did not survive the claim:\nstored:  %q\narrived: %q", wsRules, task.WorkspaceContext)
	}
}
