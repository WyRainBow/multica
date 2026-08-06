package main

import "testing"

// `--test` exists so a throwaway issue is separable from real work in
// `multica issue list`, whose table has no labels column. The only behaviour
// worth pinning is that the mark is applied exactly once.

func TestMarkTestIssueTitle_AddsThePrefix(t *testing.T) {
	if got := markTestIssueTitle("封存横幅"); got != "[测试] 封存横幅" {
		t.Fatalf("markTestIssueTitle = %q, want %q", got, "[测试] 封存横幅")
	}
}

// A caller who passes --test with an already-marked title means one prefix,
// not two — and an agent retrying a create must not accumulate them.
func TestMarkTestIssueTitle_IsIdempotent(t *testing.T) {
	once := markTestIssueTitle("封存横幅")
	if twice := markTestIssueTitle(once); twice != once {
		t.Fatalf("second call changed the title:\n %q\n %q", once, twice)
	}
}

// Whitespace around the title would put the prefix check on the wrong side of
// a space, marking an already-marked title a second time.
func TestMarkTestIssueTitle_TrimsBeforeChecking(t *testing.T) {
	if got := markTestIssueTitle("  [测试] 封存横幅  "); got != "[测试] 封存横幅" {
		t.Fatalf("markTestIssueTitle = %q, want the title unchanged and trimmed", got)
	}
	if got := markTestIssueTitle("  封存横幅  "); got != "[测试] 封存横幅" {
		t.Fatalf("markTestIssueTitle = %q, want a single prefix and no stray spaces", got)
	}
}

// A title that merely mentions the words must still be marked — only a real
// prefix counts.
func TestMarkTestIssueTitle_OnlyAPrefixCounts(t *testing.T) {
	got := markTestIssueTitle("讨论 [测试] 前缀该不该写死")
	if got != "[测试] 讨论 [测试] 前缀该不该写死" {
		t.Fatalf("markTestIssueTitle = %q, want the title marked", got)
	}
}
