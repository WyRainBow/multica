package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A card is written to be found again months later. Everything here is about
// whether it can be.

func listCards(t *testing.T, query string) (cards []CardResponse, total int64) {
	t.Helper()
	path := "/api/cards"
	if query != "" {
		path += "?q=" + query
	}
	recorder := httptest.NewRecorder()
	testHandler.ListCards(recorder, newRequest("GET", path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("ListCards: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Cards []CardResponse `json:"cards"`
		Total int64          `json:"total"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode cards: %v", err)
	}
	return resp.Cards, resp.Total
}

func createCard(t *testing.T, title, content string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/cards", map[string]any{"title": title, "content": content})
	testHandler.CreateCard(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("CreateCard: expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var card CardResponse
	if err := json.NewDecoder(recorder.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	t.Cleanup(func() {
		del := httptest.NewRecorder()
		r := newRequest("DELETE", "/api/cards/"+card.ID, nil)
		r = withURLParam(r, "id", card.ID)
		testHandler.DeleteCard(del, r)
	})
	return card.ID
}

// The lesson is in the body, not the heading. A title-only search would miss
// every card whose title is a date or a bare topic.
func TestListCards_SearchesTheBodyNotJustTheTitle(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	want := createCard(t, "2026-08-06", "踩坑：SKILL.md 有 500 行预算")
	createCard(t, "另一张卡片", "内容完全无关")

	cards, _ := listCards(t, "预算")
	found := false
	for _, card := range cards {
		if card.ID == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("card matched only in its body was not returned; got %d cards", len(cards))
	}
}

func TestListCards_SearchMatchesTheTitleToo(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	want := createCard(t, "契约测试逐字钉句子", "正文与关键词无关")

	cards, _ := listCards(t, "契约测试")
	if len(cards) == 0 || cards[0].ID != want {
		t.Fatalf("title match not returned first; got %d cards", len(cards))
	}
}

// The count has to describe the same set the page came from, or "showing 2 of
// 40" reports the workspace while the rows report the search.
func TestListCards_TotalDescribesTheSearchNotTheWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	createCard(t, "唯一关键词 zzqq", "内容一")
	createCard(t, "另一张", "内容二")
	createCard(t, "又一张", "内容三")

	_, allTotal := listCards(t, "")
	cards, searchTotal := listCards(t, "zzqq")

	if searchTotal != int64(len(cards)) {
		t.Fatalf("total = %d but %d cards returned", searchTotal, len(cards))
	}
	if searchTotal >= allTotal {
		t.Fatalf("search total %d is not smaller than the workspace total %d", searchTotal, allTotal)
	}
}

// Lowercasing happens in Go and only the column is lowered in SQL; a query in
// the other case must still match.
func TestListCards_SearchIsCaseInsensitive(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	want := createCard(t, "MULTICA 踩坑", "SKILL.md budget")

	for _, query := range []string{"multica", "MULTICA", "MuLtIcA"} {
		cards, _ := listCards(t, query)
		found := false
		for _, card := range cards {
			if card.ID == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("query %q did not match", query)
		}
	}
}

// A blank q is not a search — it must not filter everything out.
func TestListCards_BlankQueryListsEverything(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	createCard(t, "一张卡片", "内容")

	_, plain := listCards(t, "")
	_, blank := listCards(t, "%20")
	if plain != blank {
		t.Fatalf("blank q returned %d, plain list returned %d", blank, plain)
	}
}
