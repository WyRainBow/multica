package handler

import "testing"

// A reply with no phase of its own joins the comment it answers. Without it,
// filtering the timeline to 评审 shows the question and hides the answer.

func TestCreateComment_ReplyInheritsItsParentsPhase(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "reply inherits phase", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	phaseID := createPhase(t, issueID, "评审 inherit").ID

	root := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":  "问题",
		"phase_id": phaseID,
	}))
	if root.PhaseID == nil || *root.PhaseID != phaseID {
		t.Fatalf("root phase_id = %v, want %s", root.PhaseID, phaseID)
	}

	reply := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":   "回答",
		"parent_id": root.ID,
	}))
	if reply.PhaseID == nil || *reply.PhaseID != phaseID {
		t.Fatalf("reply phase_id = %v, want it to inherit %s", reply.PhaseID, phaseID)
	}
}

// An explicit phase on the reply wins: inheritance is the default, not a rule.
func TestCreateComment_ExplicitPhaseOnAReplyWins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "explicit phase wins", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	parentPhase := createPhase(t, issueID, "评审 explicit-a").ID
	otherPhase := createPhase(t, issueID, "评审 explicit-b").ID

	root := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "问题", "phase_id": parentPhase,
	}))
	reply := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "回答", "parent_id": root.ID, "phase_id": otherPhase,
	}))

	if reply.PhaseID == nil || *reply.PhaseID != otherPhase {
		t.Fatalf("reply phase_id = %v, want the explicit %s", reply.PhaseID, otherPhase)
	}
}

// Replying to an unfiled comment leaves the reply unfiled — inheritance
// copies what the parent has, and nothing is a valid answer.
func TestCreateComment_ReplyToAnUnfiledCommentStaysUnfiled(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "unfiled parent", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	root := decodeComment(t, postComment(t, issueID, map[string]any{"content": "问题"}))
	reply := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "回答", "parent_id": root.ID,
	}))

	if reply.PhaseID != nil {
		t.Fatalf("reply phase_id = %v, want nil", *reply.PhaseID)
	}
}

// A reply to a reply follows the DIRECT parent, so a sub-branch someone moved
// deliberately keeps its replies with it rather than snapping back to the root.
func TestCreateComment_ReplyFollowsTheDirectParentNotTheRoot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "direct parent wins", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	rootPhase := createPhase(t, issueID, "评审 direct-root").ID
	branchPhase := createPhase(t, issueID, "评审 direct-branch").ID

	root := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "主楼", "phase_id": rootPhase,
	}))
	branch := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "被移走的分支", "parent_id": root.ID, "phase_id": branchPhase,
	}))
	leaf := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "分支下的回复", "parent_id": branch.ID,
	}))

	if leaf.PhaseID == nil || *leaf.PhaseID != branchPhase {
		t.Fatalf("leaf phase_id = %v, want the direct parent's %s", leaf.PhaseID, branchPhase)
	}
}
