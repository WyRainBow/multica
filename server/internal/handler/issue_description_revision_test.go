package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/channelmedia"
)

// COC-342: a description write may declare the revision it was composed from.
// The declaration is checked under the same row lock the channel-media merge
// runs in, and it is checked BEFORE that merge — a body the caller never saw
// must not be folded into the one it did.

func createRevisionTestIssue(t *testing.T, title, description string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"description": description,
		"status":      "todo",
		"priority":    "none",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: status %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	return created
}

func updateRevisionTestIssue(t *testing.T, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID)
	testHandler.UpdateIssue(w, req)
	return w
}

func storedDescription(t *testing.T, issueID string) (string, int64) {
	t.Helper()
	var description string
	var revision int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(description, ''), description_revision FROM issue WHERE id = $1`,
		issueID).Scan(&description, &revision); err != nil {
		t.Fatalf("load stored description: %v", err)
	}
	return description, revision
}

// 1. A write that names the revision it edited goes through and moves the
// counter, so the next writer's base is the body this one produced.
func TestUpdateIssueDescriptionWithCurrentRevisionBumps(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision accepted", "Original")
	if created.DescriptionRevision != 1 {
		t.Fatalf("new issue description_revision = %d, want 1", created.DescriptionRevision)
	}

	w := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Edited body",
		"base_description_revision": created.DescriptionRevision,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update with current revision: status %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated issue: %v", err)
	}
	if updated.DescriptionRevision != created.DescriptionRevision+1 {
		t.Fatalf("description_revision = %d, want %d", updated.DescriptionRevision, created.DescriptionRevision+1)
	}
	description, revision := storedDescription(t, created.ID)
	if description != "Edited body" || revision != created.DescriptionRevision+1 {
		t.Fatalf("stored description = %q revision = %d", description, revision)
	}
}

// 2. A write composed from a body that has since been replaced is refused, and
// refused without touching the stored text.
func TestUpdateIssueDescriptionWithStaleRevisionConflicts(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision refused", "Original")

	first := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Winner body",
		"base_description_revision": created.DescriptionRevision,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first update: status %d: %s", first.Code, first.Body.String())
	}

	stale := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Loser body",
		"base_description_revision": created.DescriptionRevision,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update: status %d, want 409: %s", stale.Code, stale.Body.String())
	}
	var conflict map[string]any
	if err := json.Unmarshal(stale.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict body: %v", err)
	}
	if conflict["code"] != "description_revision_stale" {
		t.Fatalf("conflict code = %v, want description_revision_stale (body %s)", conflict["code"], stale.Body.String())
	}
	if conflict["base_description_revision"] != float64(created.DescriptionRevision) {
		t.Fatalf("conflict base_description_revision = %v", conflict["base_description_revision"])
	}
	if conflict["current_description_revision"] != float64(created.DescriptionRevision+1) {
		t.Fatalf("conflict current_description_revision = %v", conflict["current_description_revision"])
	}
	if next, _ := conflict["next"].(string); !strings.Contains(next, "issue get") {
		t.Fatalf("conflict next = %v, want the re-read instruction", conflict["next"])
	}

	description, revision := storedDescription(t, created.ID)
	if description != "Winner body" || revision != created.DescriptionRevision+1 {
		t.Fatalf("refused write leaked through: description = %q revision = %d", description, revision)
	}
}

// 3. Two writers that read the same body: the first to save wins, the second is
// told rather than silently overwriting the first.
func TestUpdateIssueDescriptionConcurrentWritersSecondConflicts(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision race", "Shared original")
	sharedBase := created.DescriptionRevision

	writerA := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Shared original\n\nParagraph from A",
		"base_description_revision": sharedBase,
	})
	if writerA.Code != http.StatusOK {
		t.Fatalf("writer A: status %d: %s", writerA.Code, writerA.Body.String())
	}

	writerB := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Shared original\n\nParagraph from B",
		"base_description_revision": sharedBase,
	})
	if writerB.Code != http.StatusConflict {
		t.Fatalf("writer B: status %d, want 409: %s", writerB.Code, writerB.Body.String())
	}

	description, _ := storedDescription(t, created.ID)
	if !strings.Contains(description, "Paragraph from A") || strings.Contains(description, "Paragraph from B") {
		t.Fatalf("stored description after race = %q", description)
	}
}

// 4. Re-sending the text that is already there is not an edit. If it moved the
// counter, a no-op save would invalidate a base another writer legitimately
// holds.
func TestUpdateIssueDescriptionSameValueDoesNotBumpRevision(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision idempotent", "Unchanged body")

	w := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Unchanged body",
		"base_description_revision": created.DescriptionRevision,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("same-value update: status %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated issue: %v", err)
	}
	if updated.DescriptionRevision != created.DescriptionRevision {
		t.Fatalf("description_revision = %d, want unchanged %d", updated.DescriptionRevision, created.DescriptionRevision)
	}

	// The base the caller held is still usable — that is the point of not
	// bumping on a no-op.
	again := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Real edit",
		"base_description_revision": created.DescriptionRevision,
	})
	if again.Code != http.StatusOK {
		t.Fatalf("follow-up edit on the same base: status %d: %s", again.Code, again.Body.String())
	}
}

// 5. Revision check runs before the channel-media merge: a valid base still
// gets the merge and keeps media that landed asynchronously, and a stale base
// is refused without the merge having rewritten anything.
func TestUpdateIssueDescriptionRevisionCheckPrecedesChannelMediaMerge(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	ctx := context.Background()
	created := createRevisionTestIssue(t, "Revision before media merge", "Original")

	attachmentID := uuid.New().String()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, issue_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'diagram.png', $5, 'image/png', 3)
	`, attachmentID, testWorkspaceID, created.ID, testUserID, "https://cdn.example.test/"+attachmentID); err != nil {
		t.Fatalf("insert channel attachment: %v", err)
	}
	block := channelmedia.Block(attachmentID, "diagram.png", true)
	// Channel media lands out of band and deliberately does not move the
	// revision (see MaterializeIssueChannelMediaMarkdown); the merge, not a
	// conflict, is what protects it.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET description = $1 WHERE id = $2`,
		channelmedia.Append("Original", block), created.ID); err != nil {
		t.Fatalf("append channel media: %v", err)
	}

	// Stale base first: the merge must not have run, so the media-bearing body
	// is exactly as the appender left it.
	stale := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Edited without the image",
		"description_base":          "Original",
		"base_description_revision": created.DescriptionRevision + 5,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale base with media present: status %d, want 409: %s", stale.Code, stale.Body.String())
	}
	description, revision := storedDescription(t, created.ID)
	if description != channelmedia.Append("Original", block) || revision != created.DescriptionRevision {
		t.Fatalf("refused write disturbed the body: description = %q revision = %d", description, revision)
	}

	// Valid base: check passes, merge runs, media survives the user's edit.
	ok := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Original with local edit",
		"description_base":          "Original",
		"base_description_revision": created.DescriptionRevision,
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("valid base with media present: status %d: %s", ok.Code, ok.Body.String())
	}
	var merged IssueResponse
	if err := json.NewDecoder(ok.Body).Decode(&merged); err != nil {
		t.Fatalf("decode merged issue: %v", err)
	}
	if merged.Description == nil ||
		!strings.Contains(*merged.Description, "Original with local edit") ||
		!channelmedia.HasMarker(*merged.Description, attachmentID) {
		t.Fatalf("channel media degraded through the revision check: %#v", merged.Description)
	}
	if merged.DescriptionRevision != created.DescriptionRevision+1 {
		t.Fatalf("merged description_revision = %d, want %d", merged.DescriptionRevision, created.DescriptionRevision+1)
	}
}

// 6. Agents and the CLI are refused mechanically when they write a body without
// saying which version they edited. Judged by resolveActor and the CLI's own
// platform header — never by the harness signal, which is display-only.
func TestUpdateIssueDescriptionRequiresBaseRevisionForAgentAndCLI(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision mandatory", "Original")

	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"agent actor", map[string]string{
			"X-Actor-Source": "task_token",
			"X-Agent-ID":     uuid.New().String(),
		}},
		{"cli client", map[string]string{"X-Client-Platform": "cli"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
				"description": "Body written from nowhere",
			})
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			testHandler.UpdateIssue(w, withURLParam(req, "id", created.ID))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode refusal: %v", err)
			}
			if body["code"] != "description_revision_required" {
				t.Fatalf("refusal code = %v (body %s)", body["code"], w.Body.String())
			}
			description, revision := storedDescription(t, created.ID)
			if description != "Original" || revision != created.DescriptionRevision {
				t.Fatalf("refused write leaked through: description = %q revision = %d", description, revision)
			}
		})
	}

	// The same CLI request succeeds once it declares a base.
	ok := httptest.NewRecorder()
	okReq := newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
		"description":               "Body written from a base",
		"base_description_revision": created.DescriptionRevision,
	})
	okReq.Header.Set("X-Client-Platform", "cli")
	testHandler.UpdateIssue(ok, withURLParam(okReq, "id", created.ID))
	if ok.Code != http.StatusOK {
		t.Fatalf("cli update with base: status %d: %s", ok.Code, ok.Body.String())
	}
}

// 7. Installed web/desktop builds predate the field. They keep working exactly
// as before — description writes are NOT globally protected yet, and this test
// pins that remaining legacy unprotected path so its removal is deliberate.
func TestUpdateIssueDescriptionLegacyClientWithoutBaseRevisionStillWrites(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision legacy", "Original")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
		"description": "Legacy unprotected write",
	})
	req.Header.Set("X-Client-Platform", "web")
	testHandler.UpdateIssue(w, withURLParam(req, "id", created.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("legacy update: status %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode legacy update: %v", err)
	}
	// The counter still moves, so a protected writer holding the old base
	// finds out that this write happened.
	if updated.DescriptionRevision != created.DescriptionRevision+1 {
		t.Fatalf("legacy write description_revision = %d, want %d", updated.DescriptionRevision, created.DescriptionRevision+1)
	}
	description, _ := storedDescription(t, created.ID)
	if description != "Legacy unprotected write" {
		t.Fatalf("legacy write did not land: %q", description)
	}
}

// 8. A non-numeric base is a malformed request, not a server fault.
func TestUpdateIssueDescriptionMalformedBaseRevisionIsBadRequest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	created := createRevisionTestIssue(t, "Revision malformed", "Original")

	w := updateRevisionTestIssue(t, created.ID, map[string]any{
		"description":               "Edited body",
		"base_description_revision": "not-a-number",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed base_description_revision: status %d, want 400: %s", w.Code, w.Body.String())
	}
	description, revision := storedDescription(t, created.ID)
	if description != "Original" || revision != created.DescriptionRevision {
		t.Fatalf("malformed request wrote anyway: description = %q revision = %d", description, revision)
	}
}
