package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postComment sends a create-comment request and returns the recorder so a
// caller can assert on either the created body or a rejection.
func postComment(t *testing.T, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(recorder, req)
	return recorder
}

func decodeComment(t *testing.T, recorder *httptest.ResponseRecorder) CommentResponse {
	t.Helper()
	if recorder.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var comment CommentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	return comment
}

// An inline comment carries the description span it was written against, and
// that span survives the round-trip to the list endpoint — the highlight can
// only be re-located from data the client actually receives.
func TestCreateComment_PersistsInlineAnchor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "anchor round-trip", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":       "这就是给 claude 的测试评论",
		"anchor_text":   "V1 结论",
		"anchor_offset": 42,
	}))

	if created.AnchorText == nil || *created.AnchorText != "V1 结论" {
		t.Fatalf("anchor_text = %v, want %q", created.AnchorText, "V1 结论")
	}
	if created.AnchorOffset == nil || *created.AnchorOffset != 42 {
		t.Fatalf("anchor_offset = %v, want 42", created.AnchorOffset)
	}

	recorder := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/comments", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListComments(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ListComments: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var listed []CommentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&listed); err != nil {
		t.Fatalf("decode comment list: %v", err)
	}
	var found *CommentResponse
	for i := range listed {
		if listed[i].ID == created.ID {
			found = &listed[i]
		}
	}
	if found == nil {
		t.Fatalf("created comment missing from the list")
	}
	if found.AnchorText == nil || *found.AnchorText != "V1 结论" {
		t.Fatalf("listed anchor_text = %v, want %q", found.AnchorText, "V1 结论")
	}
}

// An ordinary comment stays anchorless — inline comments must not change the
// shape of everything already in the timeline.
func TestCreateComment_PlainCommentHasNoAnchor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "plain comment", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeComment(t, postComment(t, issueID, map[string]any{
		"content": "no anchor here",
	}))
	if created.AnchorText != nil || created.AnchorOffset != nil {
		t.Fatalf("plain comment carries an anchor: %v / %v", created.AnchorText, created.AnchorOffset)
	}
}

// Anchors that could never match anything are rejected at the boundary rather
// than stored as a highlight that silently never renders.
func TestCreateComment_RejectsUnusableAnchors(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "anchor validation", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	cases := []struct {
		name string
		body map[string]any
	}{
		{"blank anchor", map[string]any{"content": "c", "anchor_text": "   "}},
		{"offset without text", map[string]any{"content": "c", "anchor_offset": 3}},
		{"negative offset", map[string]any{"content": "c", "anchor_text": "x", "anchor_offset": -1}},
		{
			"oversized anchor",
			map[string]any{
				"content":     "c",
				"anchor_text": strings.Repeat("字", maxCommentAnchorLength+1),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if recorder := postComment(t, issueID, tc.body); recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// The stored anchor is trimmed. A selection almost always carries leading or
// trailing whitespace, and an untrimmed anchor would fail to match the text it
// was taken from.
func TestCreateComment_TrimsAnchorText(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "anchor trimming", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":     "c",
		"anchor_text": "  V1 结论\n",
	}))
	if created.AnchorText == nil || *created.AnchorText != "V1 结论" {
		t.Fatalf("anchor_text = %v, want it trimmed to %q", created.AnchorText, "V1 结论")
	}
}

// A multi-byte anchor is measured in characters, not bytes: the cap exists to
// bound how much document a quote may duplicate, and CJK text would otherwise
// hit it at a third of the intended length.
func TestCreateComment_AnchorLimitCountsCharacters(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "anchor limit", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	atLimit := strings.Repeat("字", maxCommentAnchorLength)
	created := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":     "c",
		"anchor_text": atLimit,
	}))
	if created.AnchorText == nil || len([]rune(*created.AnchorText)) != maxCommentAnchorLength {
		t.Fatalf("a %d-character anchor was rejected or truncated", maxCommentAnchorLength)
	}
	if got := len(*created.AnchorText); got <= maxCommentAnchorLength {
		t.Fatalf("expected the anchor to exceed %d BYTES (got %d), otherwise this test proves nothing",
			maxCommentAnchorLength, got)
	}
}

// Replies inherit nothing: an anchored root does not make its replies anchored,
// otherwise every reply would paint its own highlight over the same span.
func TestCreateComment_ReplyToAnchoredCommentIsNotAnchored(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "anchored thread", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	root := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":     "root",
		"anchor_text": "V1 结论",
	}))
	reply := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":   "reply",
		"parent_id": root.ID,
	}))

	if reply.AnchorText != nil {
		t.Fatalf("reply inherited an anchor: %v", *reply.AnchorText)
	}
	if reply.ParentID == nil || *reply.ParentID != root.ID {
		t.Fatalf("reply parent = %v, want %s", reply.ParentID, root.ID)
	}
}

func TestCreateComment_AnchorOffsetIsOptional(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "anchor without offset", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	created := decodeComment(t, postComment(t, issueID, map[string]any{
		"content":     fmt.Sprintf("c-%d", maxCommentAnchorLength),
		"anchor_text": "V1 结论",
	}))
	if created.AnchorText == nil {
		t.Fatalf("anchor_text was dropped when no offset was supplied")
	}
	if created.AnchorOffset != nil {
		t.Fatalf("anchor_offset = %v, want nil", *created.AnchorOffset)
	}
}
