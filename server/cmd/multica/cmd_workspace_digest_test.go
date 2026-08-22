package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/assetmap"
	"github.com/multica-ai/multica/server/internal/cli"
)

// The scans are what turn five separate "run me" mechanisms into one delivery.
// Each is tested against a stubbed API rather than a live workspace, because
// the interesting cases — a folder past the readable size, a card that has
// been open for months — are states nobody wants to create for real.

func digestTestClient(t *testing.T, handler http.HandlerFunc) (*cli.APIClient, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	client, err := newAPIClient(&cobra.Command{})
	if err != nil {
		t.Fatalf("newAPIClient: %v", err)
	}
	return client, srv.URL
}

func TestAssetEntropyIsReportedOnlyOnceAFolderOutgrowsScanning(t *testing.T) {
	// t.Setenv rules out t.Parallel here.
	// The threshold already exists inside `workspace context`, where it can
	// only speak to somebody who ran that command. Here it has to speak on its
	// own, and through a different code path — a count from the list endpoint
	// rather than the length of a rendered group — so it needs its own check.
	totals := map[string]int{
		"AgentWiki/cases_案例":     assetmap.ComfortableIndexSize + 2,
		"指南":                     assetmap.ComfortableIndexSize, // exactly at the line
		"AgentWiki/playbooks_手册": 3,
	}
	client, _ := digestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Query().Get("kind")
		json.NewEncoder(w).Encode(map[string]any{"cards": []any{}, "total": totals[kind]})
	})

	section := scanAssetEntropy(context.Background(), client)
	if len(section.Items) != 1 {
		t.Fatalf("expected exactly the one folder over the line, got %+v", section.Items)
	}
	if !strings.Contains(section.Items[0].Text, "经验案例") {
		t.Errorf("the wrong folder was reported: %q", section.Items[0].Text)
	}
	// A folder sitting exactly at the threshold is not over it. Off-by-one
	// here means the warning fires a folder early, forever.
	if strings.Contains(section.Items[0].Text, "指南") {
		t.Error("a folder exactly at the threshold was reported as over it")
	}
}

func TestUnreviewedDraftsAreListedByIDSoTheyCanBeOpened(t *testing.T) {
	var askedKind string
	client, _ := digestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		askedKind = r.URL.Query().Get("kind")
		json.NewEncoder(w).Encode(map[string]any{
			"cards": []map[string]any{
				{"id": "draft-1", "title": "2026-08-22-归档动作把历史写歪了"},
			},
			"total": 1,
		})
	})

	section := scanUnreviewedDrafts(context.Background(), client)
	// Reading the wrong kind is silent: the section renders empty and looks
	// exactly like a day with no drafts.
	if askedKind != assetmap.CaseDraftKind {
		t.Errorf("read kind %q, want %q", askedKind, assetmap.CaseDraftKind)
	}
	if len(section.Items) != 1 || section.Items[0].Ref != "draft-1" {
		t.Fatalf("got %+v, want the draft with its id", section.Items)
	}
	// Promotion is a kind change; the line has to say so or the draft sits
	// there being re-reported every day it changes.
	if !strings.Contains(section.Do, "AgentWiki/cases_案例") {
		t.Errorf("the section does not say how to promote: %q", section.Do)
	}
}

func TestAFolderThatCannotBeReadDoesNotTakeTheOthersDown(t *testing.T) {
	// One unreadable endpoint must cost one section, not the digest. A patrol
	// that goes silent because a single query failed is a patrol that reports
	// "nothing to do" on the day something broke.
	client, _ := digestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "cases") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"cards": []any{}, "total": assetmap.ComfortableIndexSize + 5,
		})
	})

	section := scanAssetEntropy(context.Background(), client)
	if len(section.Items) == 0 {
		t.Fatal("a failing folder silenced the whole section")
	}
	for _, item := range section.Items {
		if strings.Contains(item.Text, "经验案例") {
			t.Error("the failing folder was reported anyway")
		}
	}
}

func TestMergedOpenCardsAndOtherTroubleAreReportedSeparately(t *testing.T) {
	// One tree can be both merged-with-open-cards and dirty. Reporting it once
	// under a single heading would bury whichever reason the reader was not
	// looking for, and these two want different actions.
	client, _ := digestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/worktrees") && strings.Contains(r.URL.Path, "entries"):
			json.NewEncoder(w).Encode(map[string]any{
				"entries": []map[string]any{{"issue_id": "issue-1"}},
			})
		case r.URL.Path == "/api/worktrees":
			json.NewEncoder(w).Encode(map[string]any{
				"worktrees": []map[string]any{{
					"name": "coc-9", "branch": "b", "status": "merged", "dirty": true,
					"verified_at": "2026-08-01T00:00:00Z",
					"session":     map[string]any{"agent": "claude"},
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			json.NewEncoder(w).Encode(map[string]any{"identifier": "COC-9", "status": "in_progress"})
		default:
			http.NotFound(w, r)
		}
	})

	merged := scanMergedOpenCards(context.Background(), client)
	trouble := scanWorktreeTrouble(context.Background(), client)

	if len(merged.Items) != 1 || !strings.Contains(merged.Items[0].Text, "COC-9") {
		t.Fatalf("merged-open-cards section: %+v", merged.Items)
	}
	if len(trouble.Items) != 1 || !strings.Contains(trouble.Items[0].Text, "有未提交改动") {
		t.Fatalf("worktree trouble section: %+v", trouble.Items)
	}
	// The merged reason must not be repeated in the other section, or the
	// same fact is counted twice in a digest read for its counts.
	if strings.Contains(trouble.Items[0].Text, "已合入") {
		t.Errorf("merged-open-cards leaked into 工作树异常: %q", trouble.Items[0].Text)
	}
}

func TestTheLastDigestIsFoundInABareCommentArray(t *testing.T) {
	// The comments endpoint answers with a bare array. Decoding it as an
	// envelope yields no error and no rows, the suppression rule then reads
	// every day as "nothing posted before", and the digest posts a duplicate
	// every single run — which is what it shipped doing, past a passing unit
	// test that never touched the wire.
	previous := digest{
		Date:     "2026-08-21",
		Sections: []digestSection{{Label: "工作树异常", Items: []digestItem{{Ref: "coc-9", Text: "没人认领"}}}},
	}
	client, _ := digestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"content": "一句人写的回复", "created_at": "2026-08-21T10:00:00Z"},
			{"content": previous.Render(), "created_at": "2026-08-21T09:00:00Z"},
		})
	})

	body, err := lastDigestComment(context.Background(), client, "issue-1")
	if err != nil {
		t.Fatalf("lastDigestComment: %v", err)
	}
	if fingerprintOf(body) != previous.Fingerprint() {
		t.Fatalf("did not find the previous digest: got %q", body)
	}
	// A human reply is newer. Comparing against it would make every scan look
	// changed and repost work already delivered.
	if strings.Contains(body, "一句人写的回复") {
		t.Error("a human comment was mistaken for the last digest")
	}

	today := digest{Date: "2026-08-22", Sections: previous.Sections}
	if post, _ := postDecision(today, body); post {
		t.Error("an unchanged scan would have been posted again")
	}
}
