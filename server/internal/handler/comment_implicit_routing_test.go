package handler

import (
	"context"
	"testing"
)

// The fork's contract for implicitCommentRoutingEnabled.
//
// TestMain turns the switch ON so the upstream suite keeps covering the routing
// logic. These cases turn it back OFF, which is how it actually ships, and pin
// what that means: a person can answer an agent without hiring it, while the
// two paths that survive the gate keep working.

// withImplicitCommentRouting runs fn with the switch set, restoring it after.
// Restoring matters more than usual here: TestMain set it true for the whole
// package, so a case that leaked `false` would silently disarm every upstream
// routing test that happened to run after it.
func withImplicitCommentRouting(t *testing.T, enabled bool, fn func()) {
	t.Helper()
	previous := implicitCommentRoutingEnabled
	implicitCommentRoutingEnabled = enabled
	defer func() { implicitCommentRoutingEnabled = previous }()
	fn()
}

// The switch's shipped value is the whole point of it. A merge that flips the
// declaration back — or a test that forgets to restore it — has to fail here
// rather than quietly restore upstream behaviour in production.
func TestImplicitCommentRouting_ShipsDisabled(t *testing.T) {
	if implicitCommentRoutingShipsEnabled {
		t.Fatal("implicitCommentRoutingShipsEnabled is true; this fork ships implicit comment routing OFF")
	}
}

// The case that forced the switch: replying under an agent's comment used to
// enqueue a run for that agent. No mention, no assignment — the reply's parent
// simply happened to be agent-authored.
func TestImplicitCommentRouting_MemberReplyToAgentCommentEnqueuesNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	parentAgentID := createHandlerTestAgent(t, "Disabled Routing Parent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "member reply enqueues nothing", "", "")

	var parentCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'review: this looks wrong')
		RETURNING id
	`, testWorkspaceID, issueID, parentAgentID).Scan(&parentCommentID); err != nil {
		t.Fatalf("insert parent agent comment: %v", err)
	}

	withImplicitCommentRouting(t, false, func() {
		postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content":   "你好",
			"parent_id": parentCommentID,
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, parentAgentID); got != 0 {
			t.Fatalf("parent agent queued tasks = %d, want 0 — a reply is a discussion, not a dispatch", got)
		}
	})
}

// The assignee fallback is implicit too: a top-level comment on an
// agent-assigned issue used to wake the assignee.
func TestImplicitCommentRouting_TopLevelCommentDoesNotWakeAssignee(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Disabled Routing Assignee", nil)
	issueID := createCommentTriggerPreviewIssue(t, "top level does not wake assignee", "agent", assigneeID)

	withImplicitCommentRouting(t, false, func() {
		postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content": "记一笔，暂时不用动",
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, assigneeID); got != 0 {
			t.Fatalf("assignee queued tasks = %d, want 0", got)
		}
	})
}

// Naming an agent is a request, and requests still work. This is the line
// between the two: the switch removes routing nobody asked for, not the ability
// to ask.
func TestImplicitCommentRouting_ExplicitMentionStillTriggers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Disabled Routing Mentioned", nil)
	issueID := createCommentTriggerPreviewIssue(t, "explicit mention still triggers", "", "")

	withImplicitCommentRouting(t, false, func() {
		postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content": "please take this @[Disabled Routing Mentioned](mention://agent/" + agentID + ")",
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
			t.Fatalf("mentioned agent queued tasks = %d, want 1 — an explicit mention is a request", got)
		}
	})
}
