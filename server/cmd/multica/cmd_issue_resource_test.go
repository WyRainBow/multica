package main

import "testing"

// The CLI's own decision is what a row reads as when nobody typed a title. The
// URL rules live on the server, where they cannot be bypassed by another
// client.

func TestResourceLabel_PrefersATypedTitle(t *testing.T) {
	got := resourceLabel(issueResourceRow{
		URL:   "https://example.feishu.cn/docx/abc",
		Title: "  智能纪要  ",
	})
	if got != "智能纪要" {
		t.Fatalf("label = %q, want the trimmed title", got)
	}
}

// Host alone would render three Feishu docs as three identical rows.
func TestResourceLabel_FallsBackToHostAndPath(t *testing.T) {
	got := resourceLabel(issueResourceRow{URL: "https://example.feishu.cn/docx/abc123"})
	if got != "example.feishu.cn/docx/abc123" {
		t.Fatalf("label = %q", got)
	}
}

func TestResourceLabel_DropsATrailingSlash(t *testing.T) {
	if got := resourceLabel(issueResourceRow{URL: "https://example.com/team/"}); got != "example.com/team" {
		t.Fatalf("label = %q", got)
	}
}

// A long path would push the URL column off a terminal, so the fallback is
// clipped — the full URL is still in its own column.
func TestResourceLabel_ClipsALongFallback(t *testing.T) {
	long := "https://example.com/" + string(make([]byte, 0))
	for range 100 {
		long += "x"
	}
	got := resourceLabel(issueResourceRow{URL: long})
	if len([]rune(got)) != 61 {
		t.Fatalf("clipped label is %d runes, want 60 plus the ellipsis: %q", len([]rune(got)), got)
	}
}

// The server only accepts http(s), but an older row could still be
// unparseable — the table must print something rather than a blank cell.
func TestResourceLabel_FallsBackToTheRawStringWhenUnparseable(t *testing.T) {
	if got := resourceLabel(issueResourceRow{URL: "not a url"}); got != "not a url" {
		t.Fatalf("label = %q", got)
	}
}
