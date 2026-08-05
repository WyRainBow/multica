package cli

import "testing"

// Verified by reading the variable inside a live Claude Code session rather
// than taken from documentation — the whole mechanism rests on this name.
func TestDetectHarness_RecognisesClaudeCode(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CLAUDECODE", "1")
	if got := DetectHarness(); got != "claude-code" {
		t.Fatalf("DetectHarness() = %q, want %q", got, "claude-code")
	}
}

// Read out of a live Codex session (gpt-5.6-sol, ~/开源工具/multica). Picked
// over CODEX_MANAGED_BY_NPM / CODEX_MANAGED_PACKAGE_ROOT, which say how the
// binary was installed, not that a session is running.
func TestDetectHarness_RecognisesCodex(t *testing.T) {
	// Cleared explicitly: this suite often runs inside Claude Code, which
	// leaves CLAUDECODE in the environment and would otherwise decide the
	// answer for us.
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CODEX_THREAD_ID", "thread_abc123")
	if got := DetectHarness(); got != "codex" {
		t.Fatalf("DetectHarness() = %q, want %q", got, "codex")
	}
}

// Both sets of variables are present whenever Claude Code dispatches work to
// Codex — the environment is inherited down the chain and says nothing about
// which is inner. The fixed precedence answers "Codex", which is the direction
// work flows here.
func TestDetectHarness_PrefersCodexWhenNested(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CODEX_THREAD_ID", "thread_abc123")
	if got := DetectHarness(); got != "codex" {
		t.Fatalf("DetectHarness() = %q, want codex", got)
	}
}

// A person at a terminal is the default, and it must report nothing rather
// than guessing — an activity row wrongly labelled "Claude" is worse than one
// that just says the member's name.
func TestDetectHarness_EmptyForAPerson(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CODEX_THREAD_ID", "")
	if got := DetectHarness(); got != "" {
		t.Fatalf("DetectHarness() = %q, want empty", got)
	}
}
