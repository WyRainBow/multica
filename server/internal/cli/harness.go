package cli

import (
	"os"
	"strings"
)

// DetectHarness names the agent shell this CLI process is running inside, or
// "" when it is a person at a terminal.
//
// The CLI authenticates with the user's own token whichever way it is invoked,
// so the server sees "cocoyu" for an edit a person made and for one Claude
// made on their behalf. Both are true, and neither answers "who actually typed
// this". This is the missing half, self-reported by the only party that knows.
//
// Environment variables rather than a flag: the point is that nobody has to
// remember to pass anything. Every entry here is a variable the harness sets
// for its own child processes, verified by reading it inside a live session
// rather than taken from documentation.
//
// Display only. `middleware/client.go` says it of the sibling X-Client-*
// headers and it is just as true here: this is client-controlled and trivial
// to spoof, so it may label an activity row and must never gate anything.
func DetectHarness() string {
	// Explicit override: an agent that leaves no env-var footprint (grok)
	// exports MULTICA_HARNESS itself. First because an explicit claim beats
	// every inference below it.
	if v := strings.TrimSpace(os.Getenv("MULTICA_HARNESS")); v != "" {
		return v
	}
	// Ordered, and the order matters: harnesses nest, and every one in the
	// chain leaves its variables in the environment. Nothing in an env var
	// says which is inner, so this is a fixed precedence rather than a
	// detection — Codex first, because here Claude coordinates and Codex is
	// the one dispatched to do the work, so when both are present the command
	// came from Codex.
	for _, candidate := range []struct {
		env  string
		name string
	}{
		// Codex sets CODEX_THREAD_ID per session. Chosen over its siblings
		// CODEX_MANAGED_BY_NPM and CODEX_MANAGED_PACKAGE_ROOT, which say how
		// the binary was installed rather than that a session is running, and
		// over CODEX_CI, which describes the environment.
		{"CODEX_THREAD_ID", "codex"},
		// opencode sets OPENCODE_PID per running session. Chosen over
		// OPENCODE=1, which is present for anything opencode ever spawned, and
		// over OPENCODE_CONFIG_DIR, which points at a shared directory rather
		// than at a session. Ranked with codex — both are dispatched workers,
		// and neither nests inside the other — and above claude-code for the
		// same reason codex is: when a coordinator's variables are also in the
		// environment, the command still came from the worker.
		{"OPENCODE_PID", "opencode"},
		// Set by Claude Code for every child process; CLAUDE_CODE_ENTRYPOINT
		// and CLAUDE_CODE_SESSION_ID travel with it, but this one is the flag.
		{"CLAUDECODE", "claude-code"},
		// Set by ZCode for every child process. ZCODE_APP_VERSION is set for
		// the whole app lifetime, not per-session, but zcode does not nest
		// inside another harness — a zcode child is a zcode command.
		{"ZCODE_APP_VERSION", "zcode"},
		// grok sets GROK_BIN_DIR for its children. Same nesting argument as
		// zcode: grok does not nest inside another harness here.
		{"GROK_BIN_DIR", "grok"},
	} {
		if os.Getenv(candidate.env) != "" {
			return candidate.name
		}
	}
	return ""
}
