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

func requireGrowthCardDB(t *testing.T) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
}

func postGrowthCard(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.CreateGrowthCard(recorder, newRequest("POST", "/api/growth-cards", body))
	return recorder
}

func decodeGrowthCard(t *testing.T, recorder *httptest.ResponseRecorder) GrowthCardResponse {
	t.Helper()
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("growth card request: unexpected %d: %s", recorder.Code, recorder.Body.String())
	}
	var card GrowthCardResponse
	if err := json.NewDecoder(recorder.Body).Decode(&card); err != nil {
		t.Fatalf("decode growth card: %v", err)
	}
	return card
}

func growthCardRequest(t *testing.T, method, id string, body any) *http.Request {
	t.Helper()
	return withURLParam(newRequest(method, "/api/growth-cards/"+id, body), "id", id)
}

func cleanupGrowthCard(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM growth_card WHERE id = $1`, id)
	})
}

// createGrowthCard posts a card and registers its cleanup, for the many tests
// that only care about the card existing.
func createGrowthCard(t *testing.T, body any) GrowthCardResponse {
	t.Helper()
	card := decodeGrowthCard(t, postGrowthCard(t, body))
	cleanupGrowthCard(t, card.ID)
	return card
}

func getGrowthCard(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.GetGrowthCard(recorder, growthCardRequest(t, "GET", id, nil))
	return recorder
}

// foreignWorkspace creates a second tenant plus one issue and one growth card
// inside it, so isolation can be asserted against rows that really exist.
func foreignWorkspace(t *testing.T) (workspaceID, issueID, cardID string) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id
	`, "growth-card-other", "growth-card-other-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM growth_card WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(bg, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'foreign requirement', 'member', $2) RETURNING id
	`, workspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create foreign issue: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO growth_card (workspace_id, author_type, author_id, title)
		VALUES ($1, 'member', $2, 'foreign card') RETURNING id
	`, workspaceID, testUserID).Scan(&cardID); err != nil {
		t.Fatalf("create foreign growth card: %v", err)
	}
	return workspaceID, issueID, cardID
}

// The eight columns are the method: each one has to survive a round trip, or a
// question the card is supposed to force can silently vanish.
func TestCreateGrowthCard_StoresAllEightFields(t *testing.T) {
	requireGrowthCardDB(t)

	issueID := createTestIssue(t, "growth card parent", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	card := createGrowthCard(t, map[string]any{
		"issue_id":   issueID,
		"title":      "单聊并发接续",
		"systems":    "router / daemon",
		"unknowns":   "不知道 root 串行是什么",
		"agent_plan": "按 root 串行排队",
		"understood": "同一 root 下只有一个在跑",
		"verified":   "本地并发发两条，第二条排队",
		"learned":    "串行化的粒度决定吞吐",
		"next_gaps":  "Postgres advisory lock",
	})

	if card.IssueID == nil || *card.IssueID != issueID {
		t.Fatalf("issue_id = %v, want %s", card.IssueID, issueID)
	}
	if card.AuthorType != "member" || card.AuthorID != testUserID {
		t.Fatalf("author = %s/%s, want member/%s", card.AuthorType, card.AuthorID, testUserID)
	}

	// Read it back through GET so a column that never reached the database is
	// not masked by the create response echoing the request.
	stored := decodeGrowthCard(t, getGrowthCard(t, card.ID))
	for _, tc := range []struct{ name, got, want string }{
		{"title", stored.Title, "单聊并发接续"},
		{"systems", stored.Systems, "router / daemon"},
		{"unknowns", stored.Unknowns, "不知道 root 串行是什么"},
		{"agent_plan", stored.AgentPlan, "按 root 串行排队"},
		{"understood", stored.Understood, "同一 root 下只有一个在跑"},
		{"verified", stored.Verified, "本地并发发两条，第二条排队"},
		{"learned", stored.Learned, "串行化的粒度决定吞吐"},
		{"next_gaps", stored.NextGaps, "Postgres advisory lock"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// A card is filled in over several sittings, and a blank column is itself the
// record that "this part of the loop did not happen". Refusing to save it
// would only teach the writer to fill the boxes with noise.
func TestCreateGrowthCard_EmptyCardIsSaved(t *testing.T) {
	requireGrowthCardDB(t)

	cases := []struct {
		name string
		body any
	}{
		{"no fields at all", map[string]any{}},
		{"every field empty", map[string]any{
			"title": "", "systems": "", "unknowns": "", "agent_plan": "",
			"understood": "", "verified": "", "learned": "", "next_gaps": "",
		}},
		{"whitespace only", map[string]any{"title": "   ", "learned": "\n\t "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := postGrowthCard(t, tc.body)
			if recorder.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
			}
			card := decodeGrowthCard(t, recorder)
			cleanupGrowthCard(t, card.ID)

			if card.IssueID != nil {
				t.Errorf("issue_id = %v, want nil", *card.IssueID)
			}
			// Blank must come back as empty string, not as a placeholder.
			if card.Title != "" || card.Learned != "" || card.NextGaps != "" {
				t.Errorf("blank fields were not stored empty: %+v", card)
			}
			if getGrowthCard(t, card.ID).Code != http.StatusOK {
				t.Errorf("empty card was not persisted")
			}
		})
	}
}

// The caps are character counts, not byte counts: a Chinese card would
// otherwise be rejected at a third of the intended length.
func TestCreateGrowthCard_LimitsCountCharactersNotBytes(t *testing.T) {
	requireGrowthCardDB(t)

	t.Run("title at the cap in CJK is accepted", func(t *testing.T) {
		title := strings.Repeat("字", maxGrowthCardTitleLength)
		card := createGrowthCard(t, map[string]any{"title": title})
		if len([]rune(card.Title)) != maxGrowthCardTitleLength {
			t.Fatalf("title stored with %d characters, want %d",
				len([]rune(card.Title)), maxGrowthCardTitleLength)
		}
		if len(card.Title) <= maxGrowthCardTitleLength {
			t.Fatalf("test is not exercising multi-byte characters: %d bytes", len(card.Title))
		}
	})

	t.Run("body field at the cap in CJK is accepted", func(t *testing.T) {
		body := strings.Repeat("学", maxGrowthCardFieldLength)
		card := createGrowthCard(t, map[string]any{"learned": body})
		if len([]rune(card.Learned)) != maxGrowthCardFieldLength {
			t.Fatalf("learned stored with %d characters, want %d",
				len([]rune(card.Learned)), maxGrowthCardFieldLength)
		}
	})

	overCases := []struct {
		field string
		limit int
	}{
		{"title", maxGrowthCardTitleLength},
		{"systems", maxGrowthCardFieldLength},
		{"unknowns", maxGrowthCardFieldLength},
		{"agent_plan", maxGrowthCardFieldLength},
		{"understood", maxGrowthCardFieldLength},
		{"verified", maxGrowthCardFieldLength},
		{"learned", maxGrowthCardFieldLength},
		{"next_gaps", maxGrowthCardFieldLength},
	}
	for _, tc := range overCases {
		t.Run("over the cap rejects "+tc.field, func(t *testing.T) {
			recorder := postGrowthCard(t, map[string]any{
				tc.field: strings.Repeat("字", tc.limit+1),
			})
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tc.field) {
				t.Fatalf("error does not name the offending field: %s", recorder.Body.String())
			}
		})
	}
}

// A card must never be able to point at another tenant's requirement, on
// create or on update.
func TestGrowthCard_RejectsIssueFromAnotherWorkspace(t *testing.T) {
	requireGrowthCardDB(t)

	_, foreignIssue, _ := foreignWorkspace(t)

	recorder := postGrowthCard(t, map[string]any{"title": "x", "issue_id": foreignIssue})
	if recorder.Code < 400 || recorder.Code >= 500 {
		t.Fatalf("create status = %d, want a 4xx: %s", recorder.Code, recorder.Body.String())
	}

	card := createGrowthCard(t, map[string]any{"title": "x"})
	recorder = httptest.NewRecorder()
	testHandler.UpdateGrowthCard(recorder, growthCardRequest(t, "PUT", card.ID,
		map[string]any{"issue_id": foreignIssue}))
	if recorder.Code < 400 || recorder.Code >= 500 {
		t.Fatalf("update status = %d, want a 4xx: %s", recorder.Code, recorder.Body.String())
	}
	if stored := decodeGrowthCard(t, getGrowthCard(t, card.ID)); stored.IssueID != nil {
		t.Fatalf("rejected update still linked the card: %v", *stored.IssueID)
	}
}

func TestCreateGrowthCard_RejectsUnknownIssue(t *testing.T) {
	requireGrowthCardDB(t)

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-an-issue"} {
		recorder := postGrowthCard(t, map[string]any{"title": "x", "issue_id": id})
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("issue_id %q: status = %d, want 404: %s",
				id, recorder.Code, recorder.Body.String())
		}
	}
}

// Absent, explicit null and a value are three different intents for the issue
// link. Collapsing them silently detaches a card whenever its text is edited.
func TestUpdateGrowthCard_IssueLinkHasThreeStates(t *testing.T) {
	requireGrowthCardDB(t)

	first := createTestIssue(t, "growth card link a", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, first) })
	second := createTestIssue(t, "growth card link b", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, second) })

	card := createGrowthCard(t, map[string]any{"title": "t", "issue_id": first})

	update := func(body any) GrowthCardResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		testHandler.UpdateGrowthCard(recorder, growthCardRequest(t, "PUT", card.ID, body))
		return decodeGrowthCard(t, recorder)
	}

	// Absent: editing the text must not drop the link.
	kept := update(map[string]any{"learned": "something"})
	if kept.IssueID == nil || *kept.IssueID != first {
		t.Fatalf("absent issue_id dropped the link: %v", kept.IssueID)
	}

	// A value: relink to a different requirement.
	relinked := update(json.RawMessage(`{"issue_id":"` + second + `"}`))
	if relinked.IssueID == nil || *relinked.IssueID != second {
		t.Fatalf("issue_id = %v, want %s", relinked.IssueID, second)
	}

	// Explicit null: detach.
	detached := update(json.RawMessage(`{"issue_id":null}`))
	if detached.IssueID != nil {
		t.Fatalf("explicit null did not detach: %v", *detached.IssueID)
	}

	// Absent again, now that there is nothing to keep.
	stillDetached := update(map[string]any{"learned": "more"})
	if stillDetached.IssueID != nil {
		t.Fatalf("absent issue_id re-attached a link: %v", *stillDetached.IssueID)
	}
}

// The CLI fills one line at a time, so an update that names one field must
// leave the other seven exactly as they were — and an explicit empty string
// must still be able to clear one.
func TestUpdateGrowthCard_AbsentFieldsAreLeftAlone(t *testing.T) {
	requireGrowthCardDB(t)

	card := createGrowthCard(t, map[string]any{
		"title": "原始需求", "systems": "router", "unknowns": "a", "agent_plan": "b",
		"understood": "c", "verified": "d", "learned": "e", "next_gaps": "f",
	})

	recorder := httptest.NewRecorder()
	testHandler.UpdateGrowthCard(recorder, growthCardRequest(t, "PUT", card.ID,
		map[string]any{"verified": "我自己跑了一遍"}))
	updated := decodeGrowthCard(t, recorder)

	if updated.Verified != "我自己跑了一遍" {
		t.Fatalf("verified = %q", updated.Verified)
	}
	for _, tc := range []struct{ name, got, want string }{
		{"title", updated.Title, "原始需求"},
		{"systems", updated.Systems, "router"},
		{"unknowns", updated.Unknowns, "a"},
		{"agent_plan", updated.AgentPlan, "b"},
		{"understood", updated.Understood, "c"},
		{"learned", updated.Learned, "e"},
		{"next_gaps", updated.NextGaps, "f"},
	} {
		if tc.got != tc.want {
			t.Errorf("untouched %s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}

	// An explicit empty string is a deliberate "this did not happen".
	recorder = httptest.NewRecorder()
	testHandler.UpdateGrowthCard(recorder, growthCardRequest(t, "PUT", card.ID,
		map[string]any{"unknowns": ""}))
	cleared := decodeGrowthCard(t, recorder)
	if cleared.Unknowns != "" {
		t.Fatalf("explicit empty string did not clear the field: %q", cleared.Unknowns)
	}
	if cleared.Learned != "e" {
		t.Fatalf("clearing one field disturbed another: %q", cleared.Learned)
	}
}

// A card id belonging to another tenant must read as not-found, never as a
// forbidden resource that exists — 403 would confirm the id.
func TestGrowthCard_HidesOtherWorkspaces(t *testing.T) {
	requireGrowthCardDB(t)

	_, _, foreignCard := foreignWorkspace(t)

	cases := []struct {
		name    string
		invoke  func(w http.ResponseWriter, r *http.Request)
		body    any
		method  string
		wantErr int
	}{
		{"get", testHandler.GetGrowthCard, nil, "GET", http.StatusNotFound},
		{"update", testHandler.UpdateGrowthCard, map[string]any{"title": "hijack"}, "PUT", http.StatusNotFound},
		{"delete", testHandler.DeleteGrowthCard, nil, "DELETE", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tc.invoke(recorder, growthCardRequest(t, tc.method, foreignCard, tc.body))
			if recorder.Code != tc.wantErr {
				t.Fatalf("status = %d, want %d: %s",
					recorder.Code, tc.wantErr, recorder.Body.String())
			}
		})
	}

	// The foreign card must still be intact after the write attempts.
	var title string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title FROM growth_card WHERE id = $1`, foreignCard).Scan(&title); err != nil {
		t.Fatalf("foreign card was deleted across the workspace boundary: %v", err)
	}
	if title != "foreign card" {
		t.Fatalf("foreign card was modified across the workspace boundary: %q", title)
	}
}

// The workspace list reads newest first — it answers "what have I learned
// lately", the opposite of the per-issue view.
func TestListGrowthCards_NewestFirstWithTotal(t *testing.T) {
	requireGrowthCardDB(t)

	older := createGrowthCard(t, map[string]any{"title": "older"})
	// Ensure a distinct created_at so the ordering assertion is meaningful.
	time.Sleep(10 * time.Millisecond)
	newer := createGrowthCard(t, map[string]any{"title": "newer"})

	recorder := httptest.NewRecorder()
	testHandler.ListGrowthCards(recorder, newRequest("GET", "/api/growth-cards?limit=200", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Cards []GrowthCardResponse `json:"cards"`
		Total int64                `json:"total"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}

	newerAt, olderAt := -1, -1
	for i, card := range response.Cards {
		switch card.ID {
		case newer.ID:
			newerAt = i
		case older.ID:
			olderAt = i
		}
	}
	if newerAt < 0 || olderAt < 0 {
		t.Fatalf("list is missing the cards just created (newer=%d older=%d)", newerAt, olderAt)
	}
	if newerAt >= olderAt {
		t.Fatalf("newer card at %d is not ahead of older at %d", newerAt, olderAt)
	}
	if response.Total < 2 {
		t.Fatalf("total = %d, want at least 2", response.Total)
	}
}

// The list is workspace-scoped: another tenant's card must never appear.
func TestListGrowthCards_ExcludesOtherWorkspaces(t *testing.T) {
	requireGrowthCardDB(t)

	_, _, foreignCard := foreignWorkspace(t)

	recorder := httptest.NewRecorder()
	testHandler.ListGrowthCards(recorder, newRequest("GET", "/api/growth-cards?limit=200", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Cards []GrowthCardResponse `json:"cards"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, card := range response.Cards {
		if card.ID == foreignCard {
			t.Fatalf("another workspace's card leaked into the list")
		}
	}
}

func TestListGrowthCards_ValidatesPagination(t *testing.T) {
	requireGrowthCardDB(t)

	card := createGrowthCard(t, map[string]any{"title": "pagination probe"})

	accepted := []string{
		"",
		"?limit=1",
		"?limit=" + fmt.Sprint(growthCardMaxPage),
		"?offset=0",
		"?limit=1&offset=1",
	}
	for _, query := range accepted {
		t.Run("accepts "+query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			testHandler.ListGrowthCards(recorder, newRequest("GET", "/api/growth-cards"+query, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	rejected := []string{
		"?limit=0",
		"?limit=-1",
		"?limit=" + fmt.Sprint(growthCardMaxPage+1),
		"?limit=abc",
		"?offset=-1",
		"?offset=abc",
	}
	for _, query := range rejected {
		t.Run("rejects "+query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			testHandler.ListGrowthCards(recorder, newRequest("GET", "/api/growth-cards"+query, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	// limit=1 must actually cap the page, not just pass validation.
	recorder := httptest.NewRecorder()
	testHandler.ListGrowthCards(recorder, newRequest("GET", "/api/growth-cards?limit=1", nil))
	var response struct {
		Cards []GrowthCardResponse `json:"cards"`
		Total int64                `json:"total"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Cards) != 1 {
		t.Fatalf("limit=1 returned %d cards", len(response.Cards))
	}
	// total counts the workspace, not the page.
	if response.Total < 1 {
		t.Fatalf("total = %d, want at least 1 (card %s exists)", response.Total, card.ID)
	}
}

// One delivery can leave several cards behind; read together they are a
// narrative of how the work went, and a narrative reads forwards.
func TestListGrowthCardsForIssue_OldestFirst(t *testing.T) {
	requireGrowthCardDB(t)

	issueID := createTestIssue(t, "growth card multi", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	otherIssue := createTestIssue(t, "growth card unrelated", "done", "none")
	t.Cleanup(func() { deleteTestIssue(t, otherIssue) })

	first := createGrowthCard(t, map[string]any{"title": "第一课", "issue_id": issueID})
	time.Sleep(10 * time.Millisecond)
	second := createGrowthCard(t, map[string]any{"title": "第二课", "issue_id": issueID})
	// A card on another issue and a card with no issue must both stay out.
	createGrowthCard(t, map[string]any{"title": "别的需求", "issue_id": otherIssue})
	createGrowthCard(t, map[string]any{"title": "无需求"})

	recorder := httptest.NewRecorder()
	req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/growth-cards", nil), "id", issueID)
	testHandler.ListGrowthCardsForIssue(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Cards []GrowthCardResponse `json:"cards"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Cards) != 2 {
		t.Fatalf("got %d cards, want 2: %s", len(response.Cards), recorder.Body.String())
	}
	if response.Cards[0].ID != first.ID || response.Cards[1].ID != second.ID {
		t.Fatalf("cards not in creation order")
	}
}

func TestListGrowthCardsForIssue_HidesOtherWorkspaces(t *testing.T) {
	requireGrowthCardDB(t)

	_, foreignIssue, _ := foreignWorkspace(t)

	recorder := httptest.NewRecorder()
	req := withURLParam(newRequest("GET", "/api/issues/"+foreignIssue+"/growth-cards", nil),
		"id", foreignIssue)
	testHandler.ListGrowthCardsForIssue(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDeleteGrowthCard_RemovesIt(t *testing.T) {
	requireGrowthCardDB(t)

	card := decodeGrowthCard(t, postGrowthCard(t, map[string]any{"title": "temp"}))
	cleanupGrowthCard(t, card.ID)

	recorder := httptest.NewRecorder()
	testHandler.DeleteGrowthCard(recorder, growthCardRequest(t, "DELETE", card.ID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}
	if got := getGrowthCard(t, card.ID); got.Code != http.StatusNotFound {
		t.Fatalf("card still readable after delete: %d", got.Code)
	}
}

// Deleting a requirement must not take its cards with it: the lesson outlives
// the ticket, which is why this is a table and not an issue field. There is no
// database foreign key, so nothing enforces this but the absence of cleanup.
func TestGrowthCard_SurvivesItsIssueBeingDeleted(t *testing.T) {
	requireGrowthCardDB(t)

	issueID := createTestIssue(t, "growth card orphan source", "done", "none")
	card := createGrowthCard(t, map[string]any{
		"title": "lesson", "learned": "kept", "issue_id": issueID,
	})

	deleteTestIssue(t, issueID)

	stored := decodeGrowthCard(t, getGrowthCard(t, card.ID))
	if stored.Learned != "kept" {
		t.Fatalf("learned = %q, want kept", stored.Learned)
	}
}

func TestGrowthCard_RejectsMalformedRequests(t *testing.T) {
	requireGrowthCardDB(t)

	card := createGrowthCard(t, map[string]any{"title": "malformed probe"})

	t.Run("create with a non-object body", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req := newRequest("POST", "/api/growth-cards", nil)
		req.Body = http.NoBody
		testHandler.CreateGrowthCard(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("create with a wrongly typed field", func(t *testing.T) {
		recorder := postGrowthCard(t, json.RawMessage(`{"title":123}`))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("update with a non-string issue_id", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		testHandler.UpdateGrowthCard(recorder, growthCardRequest(t, "PUT", card.ID,
			json.RawMessage(`{"issue_id":42}`)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("get with an id that is not a uuid", func(t *testing.T) {
		if got := getGrowthCard(t, "not-a-uuid"); got.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", got.Code, got.Body.String())
		}
	})
}
