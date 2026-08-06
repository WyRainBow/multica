package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A card is a note, not an issue: no status, no assignee, no route. The CLI's
// own decisions are the ones worth pinning — what it refuses, and how it
// distinguishes "leave the issue link alone" from "remove it".

func cardTestServer(t *testing.T, handler http.HandlerFunc) (*[]string, *[]map[string]any) {
	t.Helper()
	var paths []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Body != nil {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) == nil {
				bodies = append(bodies, body)
			}
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": testIssueUUID, "identifier": "MUL-1", "title": "test issue",
			})
		case handler != nil:
			handler(w, r)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "card-1"})
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return &paths, &bodies
}

func newCardUpdateCmd() *cobra.Command {
	c := &cobra.Command{Use: "update"}
	c.Flags().String("title", "", "")
	c.Flags().String("content", "", "")
	c.Flags().Bool("content-stdin", false, "")
	c.Flags().String("content-file", "", "")
	c.Flags().Bool("allow-external-file", false, "")
	c.Flags().String("issue", "", "")
	c.Flags().Bool("detach", false, "")
	c.Flags().String("output", "json", "")
	return c
}

func newCardAddCmd() *cobra.Command {
	c := newCardUpdateCmd()
	c.Use = "add"
	return c
}

// Sending an empty PUT would look like a successful no-op while the caller
// believes something changed — most likely a typo'd flag name.
func TestCardUpdate_RefusesAnEmptyUpdate(t *testing.T) {
	paths, _ := cardTestServer(t, nil)

	_, err := captureStdout(t, func() error {
		return runCardUpdate(newCardUpdateCmd(), []string{"card-1"})
	})
	if err == nil {
		t.Fatal("empty update was accepted")
	}
	if !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("error = %v, want it to name the missing flags", err)
	}
	for _, p := range *paths {
		if strings.HasPrefix(p, "PUT ") {
			t.Fatalf("a PUT was sent for an empty update; paths = %v", *paths)
		}
	}
}

// The two say opposite things about one field, and the server distinguishes
// "absent" from "null" — accepting both would make the outcome depend on
// argument order.
func TestCardUpdate_RefusesIssueAndDetachTogether(t *testing.T) {
	cardTestServer(t, nil)

	cmd := newCardUpdateCmd()
	_ = cmd.Flags().Set("issue", testIssueUUID)
	_ = cmd.Flags().Set("detach", "true")

	_, err := captureStdout(t, func() error {
		return runCardUpdate(cmd, []string{"card-1"})
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want a mutual-exclusion refusal", err)
	}
}

// --detach has to send a real JSON null. An empty string would read as "link
// this card to the issue with an empty id" and leave the link in place.
func TestCardUpdate_DetachSendsAnExplicitNull(t *testing.T) {
	_, bodies := cardTestServer(t, nil)

	cmd := newCardUpdateCmd()
	_ = cmd.Flags().Set("detach", "true")
	if _, err := captureStdout(t, func() error {
		return runCardUpdate(cmd, []string{"card-1"})
	}); err != nil {
		t.Fatalf("detach: %v", err)
	}

	if len(*bodies) == 0 {
		t.Fatal("no request body captured")
	}
	body := (*bodies)[len(*bodies)-1]
	raw, present := body["issue_id"]
	if !present {
		t.Fatalf("issue_id absent; detach must send it explicitly: %v", body)
	}
	if raw != nil {
		t.Fatalf("issue_id = %v, want null", raw)
	}
}

// Omitting both leaves the link alone, which is a different request from
// detaching it.
func TestCardUpdate_LeavesTheLinkAloneWhenNeitherFlagIsGiven(t *testing.T) {
	_, bodies := cardTestServer(t, nil)

	cmd := newCardUpdateCmd()
	_ = cmd.Flags().Set("title", "new title")
	if _, err := captureStdout(t, func() error {
		return runCardUpdate(cmd, []string{"card-1"})
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	body := (*bodies)[len(*bodies)-1]
	if _, present := body["issue_id"]; present {
		t.Fatalf("issue_id was sent without --issue or --detach: %v", body)
	}
}

// A card with neither is nothing at all; the server would store a blank row.
func TestCardAdd_RefusesWithNoTitleAndNoContent(t *testing.T) {
	cardTestServer(t, nil)

	_, err := captureStdout(t, func() error {
		return runCardAdd(newCardAddCmd(), nil)
	})
	if err == nil || !strings.Contains(err.Error(), "--title") {
		t.Fatalf("error = %v, want it to name the required flags", err)
	}
}

// An issue KEY is what a person has; the API takes a UUID. Resolution happens
// in the CLI so the caller never has to look one up.
func TestCardAdd_ResolvesAnIssueReferenceToItsUUID(t *testing.T) {
	_, bodies := cardTestServer(t, nil)

	cmd := newCardAddCmd()
	_ = cmd.Flags().Set("title", "linked card")
	_ = cmd.Flags().Set("issue", testIssueUUID)
	if _, err := captureStdout(t, func() error {
		return runCardAdd(cmd, nil)
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	body := (*bodies)[len(*bodies)-1]
	if body["issue_id"] != testIssueUUID {
		t.Fatalf("issue_id = %v, want %s", body["issue_id"], testIssueUUID)
	}
}

// An untitled card is legal on the server. A blank cell in the one place cards
// are listed would make it unfindable, so the body's opening stands in.
func TestCardTitleForTable_FallsBackToTheBody(t *testing.T) {
	if got := cardTitleForTable(cardRow{Content: "## 踩坑\n正文"}); got != "## 踩坑" {
		t.Fatalf("title = %q, want the first line", got)
	}
	if got := cardTitleForTable(cardRow{Title: "  有标题  ", Content: "正文"}); got != "有标题" {
		t.Fatalf("title = %q, want the trimmed title", got)
	}
	if got := cardTitleForTable(cardRow{}); got != "(untitled)" {
		t.Fatalf("title = %q, want a placeholder", got)
	}
	long := strings.Repeat("字", 60)
	got := cardTitleForTable(cardRow{Content: long})
	if len([]rune(got)) != 41 {
		t.Fatalf("clipped title is %d runes, want 40 plus the ellipsis", len([]rune(got)))
	}
}
