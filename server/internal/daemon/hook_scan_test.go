package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFile is a terse helper: these tests are about what the readers extract,
// not about error handling around fixtures.
func writeHookFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fakeHome points os.UserHomeDir at a scratch directory so a scan reads
// fixtures instead of the developer's real config.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return home
}

// The claude fixture mixes Multica hooks with third-party ones on purpose:
// the privacy filter is the first thing that has to work.
const claudeSettingsFixture = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "$HOME/.claude/hooks/multica-branch-register.sh", "timeout": 5},
          {"type": "command", "command": "/opt/other/unrelated-hook.sh --token abcd1234 --endpoint https://example.test"}
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "if": "tool_input.command contains git",
        "hooks": [
          {"type": "command", "command": "~/.claude/hooks/multica-worktree-sync.sh --workspace cocoyu --token SECRET"}
        ]
      }
    ]
  }
}`

func TestScanRuntimeHooks_ClaudeReportsOnlyMulticaHooks(t *testing.T) {
	home := fakeHome(t)
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsFixture)

	hooks, supported, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !supported {
		t.Fatal("claude must report a hook mechanism")
	}
	if len(hooks) != 2 {
		t.Fatalf("expected 2 Multica hooks, got %d: %+v", len(hooks), hooks)
	}

	post := hooks[0]
	if post.Event != "PostToolUse" || post.HookName != "multica-worktree-sync.sh" {
		t.Fatalf("unexpected first hook: %+v", post)
	}
	if post.CommandPath != "~/.claude/hooks/multica-worktree-sync.sh" {
		t.Fatalf("command path must be the normalized script path, got %q", post.CommandPath)
	}
	if post.TriggerSpec != "Bash if:tool_input.command contains git" {
		t.Fatalf("trigger spec must carry matcher and if verbatim, got %q", post.TriggerSpec)
	}
	if !post.Enabled {
		t.Fatal("hook should be enabled")
	}

	pre := hooks[1]
	if pre.Event != "PreToolUse" || pre.HookName != "multica-branch-register.sh" {
		t.Fatalf("unexpected second hook: %+v", pre)
	}
}

// The payload leaves the machine, so this asserts the absence of things rather
// than the presence of them. A regression here is a privacy incident, not a
// display bug.
func TestScanRuntimeHooks_UploadCarriesNoArgumentsOrSecrets(t *testing.T) {
	home := fakeHome(t)
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsFixture)
	t.Setenv("MULTICA_HOOK_TEST_SECRET", "must-not-appear")

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	raw, err := json.Marshal(hooks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(raw)

	for _, forbidden := range []string{
		"SECRET",              // an argument value
		"--workspace",         // an argument name
		"--token",             // an argument name
		"unrelated-hook",      // a third-party hook that is none of our business
		"example.test",        // a third-party endpoint
		"MULTICA_HOOK_TEST_S", // an environment variable name
		home,                  // the user's real home path
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("hook inventory payload leaked %q: %s", forbidden, payload)
		}
	}
}

// Codex names its events differently from Claude and gates each hook on a
// trust record. Both facts are reported as-found; neither is translated.
func TestScanRuntimeHooks_CodexPreservesEventNamesAndTrustGate(t *testing.T) {
	home := fakeHome(t)
	codexHome := filepath.Join(home, ".codex")
	hooksPath := filepath.Join(codexHome, "hooks.json")
	writeHookFixture(t, hooksPath, `{
  "hooks": {
    "session_start": [
      {"hooks": [{"type": "command", "command": "$HOME/.claude/hooks/multica-first-touch-context.sh"}]}
    ],
    "PreToolUse": [
      {"hooks": [{"type": "command", "command": "$HOME/.claude/hooks/multica-agent-guard.sh"}]},
      {"hooks": [{"type": "command", "command": "$HOME/.claude/hooks/multica-branch-register.sh"}]}
    ]
  }
}`)
	// Trust records exist for session_start:0:0 and pre_tool_use:0:0 only, so
	// multica-branch-register.sh is installed but codex will not run it.
	writeHookFixture(t, filepath.Join(codexHome, "config.toml"), `
[features]
hooks = true

[hooks.state]

[hooks.state."`+hooksPath+`:session_start:0:0"]
trusted_hash = "sha256:aaa"

[hooks.state."`+hooksPath+`:pre_tool_use:0:0"]
trusted_hash = "sha256:bbb"
`)

	hooks, supported, err := scanRuntimeHooks("codex")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !supported {
		t.Fatal("codex must report a hook mechanism")
	}
	if len(hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d: %+v", len(hooks), hooks)
	}

	byName := map[string]hookInventoryEntry{}
	for _, hook := range hooks {
		byName[hook.HookName] = hook
	}
	// session_start is codex's own spelling. Rewriting it to SessionStart
	// would produce an event name that appears in no config file anywhere.
	if got := byName["multica-first-touch-context.sh"].Event; got != "session_start" {
		t.Fatalf("codex event name must be preserved verbatim, got %q", got)
	}
	if got := byName["multica-agent-guard.sh"].Event; got != "PreToolUse" {
		t.Fatalf("codex event name must be preserved verbatim, got %q", got)
	}
	if !byName["multica-agent-guard.sh"].Enabled {
		t.Fatal("trusted hook should be enabled")
	}
	if byName["multica-branch-register.sh"].Enabled {
		t.Fatal("hook with no codex trust record must not be reported as enabled")
	}
}

func TestScanRuntimeHooks_CodexMasterSwitchOff(t *testing.T) {
	home := fakeHome(t)
	codexHome := filepath.Join(home, ".codex")
	writeHookFixture(t, filepath.Join(codexHome, "hooks.json"), `{
  "hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": "$HOME/.claude/hooks/multica-agent-guard.sh"}]}]}
}`)
	writeHookFixture(t, filepath.Join(codexHome, "config.toml"), "[features]\nhooks = false\n")

	hooks, supported, err := scanRuntimeHooks("codex")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !supported {
		t.Fatal("a disabled mechanism is still a mechanism")
	}
	if len(hooks) != 1 {
		t.Fatalf("expected the hook to still be listed, got %d", len(hooks))
	}
	if hooks[0].Enabled {
		t.Fatal("hooks disabled at the feature switch must report enabled=false")
	}
}

// The frozen spec says zcode has no hook mechanism. A real install proved
// otherwise, so the reader exists; if zcode is ever reclassified this test is
// the thing to delete alongside the reader.
func TestScanRuntimeHooks_ZcodeReadsItsOwnConfig(t *testing.T) {
	home := fakeHome(t)
	writeHookFixture(t, filepath.Join(home, ".zcode", "cli", "config.json"), `{
  "hooks": {
    "enabled": true,
    "events": {
      "PostToolUse": [
        {"matcher": "^Bash$", "hooks": [{"type": "command", "command": "$HOME/.claude/hooks/multica-worktree-sync.sh"}]}
      ]
    }
  }
}`)

	hooks, supported, err := scanRuntimeHooks("zcode")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !supported || len(hooks) != 1 {
		t.Fatalf("expected 1 supported zcode hook, got supported=%v count=%d", supported, len(hooks))
	}
	if hooks[0].TriggerSpec != "^Bash$" {
		t.Fatalf("matcher pattern must be reported verbatim, got %q", hooks[0].TriggerSpec)
	}
}

// "No hooks installed" and "no hook mechanism" must not arrive as the same
// answer. This is the distinction the whole inventory rests on.
func TestScanRuntimeHooks_UnsupportedProviderIsNotAnEmptyList(t *testing.T) {
	fakeHome(t)

	hooks, supported, err := scanRuntimeHooks("cursor")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if supported {
		t.Fatal("cursor has no hook mechanism and must report supported=false")
	}
	if hooks != nil {
		t.Fatalf("an unsupported provider must not report a list at all, got %+v", hooks)
	}

	// A supported provider whose config has no Multica hooks is the other
	// case, and it must be distinguishable.
	home := fakeHome(t)
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks": {}}`)
	hooks, supported, err = scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !supported {
		t.Fatal("claude with zero hooks is still supported")
	}
	if len(hooks) != 0 {
		t.Fatalf("expected an empty list, got %+v", hooks)
	}
}

// ---------------------------------------------------------------------------
// Telemetry
// ---------------------------------------------------------------------------

func writeInstrumentedHook(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "hooks", name)
	writeHookFixture(t, path, "#!/bin/bash\ncurl -s \"$MULTICA_HOOK_FIRED_URL\" >/dev/null\n")
	return path
}

func writeUninstrumentedHook(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "hooks", name)
	writeHookFixture(t, path, "#!/bin/bash\necho hello\n")
	return path
}

func claudeSettingsWithHook(name string) string {
	return `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "$HOME/.claude/hooks/` + name + `"}]}]}}`
}

// A hook installed before telemetry existed has not "never fired" — nobody was
// watching. This is the case the four-state vocabulary exists for.
func TestHookTelemetry_UninstrumentedHookIsUnobservedNotNeverFired(t *testing.T) {
	home := fakeHome(t)
	writeUninstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))
	// The daemon is up and the endpoint is live...
	if err := markHookTelemetryObserving(home, time.Now()); err != nil {
		t.Fatalf("mark observing: %v", err)
	}

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	// ...but this script cannot report, so silence from it means nothing.
	if hooks[0].Telemetry != hookTelemetryUnobserved {
		t.Fatalf("expected unobserved, got %q", hooks[0].Telemetry)
	}
	if hooks[0].LastFiredAt != "" {
		t.Fatal("unobserved hook must carry no last_fired_at")
	}
}

func TestHookTelemetry_InstrumentedAndSilentIsNeverFired(t *testing.T) {
	home := fakeHome(t)
	writeInstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))
	if err := markHookTelemetryObserving(home, time.Now()); err != nil {
		t.Fatalf("mark observing: %v", err)
	}

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hooks[0].Telemetry != hookTelemetryNeverFired {
		t.Fatalf("expected never_fired, got %q", hooks[0].Telemetry)
	}
}

// Before the daemon ever ran, even an instrumented hook is unobserved: the
// endpoint it reports to did not exist.
func TestHookTelemetry_BeforeObservingStartsEverythingIsUnobserved(t *testing.T) {
	home := fakeHome(t)
	writeInstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hooks[0].Telemetry != hookTelemetryUnobserved {
		t.Fatalf("expected unobserved before observation starts, got %q", hooks[0].Telemetry)
	}
}

func TestHookTelemetry_FiringFlipsStateAndStampsTime(t *testing.T) {
	home := fakeHome(t)
	writeInstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))
	if err := markHookTelemetryObserving(home, time.Now()); err != nil {
		t.Fatalf("mark observing: %v", err)
	}

	firedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := recordHookFired(home, "PreToolUse", "multica-agent-guard.sh", firedAt); err != nil {
		t.Fatalf("record firing: %v", err)
	}

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hooks[0].Telemetry != hookTelemetryFired {
		t.Fatalf("expected fired, got %q", hooks[0].Telemetry)
	}
	got, err := time.Parse(time.RFC3339, hooks[0].LastFiredAt)
	if err != nil {
		t.Fatalf("last_fired_at must be RFC3339, got %q", hooks[0].LastFiredAt)
	}
	if !got.Equal(firedAt.UTC()) {
		t.Fatalf("last_fired_at %s does not match the recorded firing %s", got, firedAt.UTC())
	}
}

// A hook that reports without knowing which event invoked it still counts.
func TestHookTelemetry_HookLevelRecordCoversOtherEvents(t *testing.T) {
	home := fakeHome(t)
	writeInstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))
	if err := markHookTelemetryObserving(home, time.Now()); err != nil {
		t.Fatalf("mark observing: %v", err)
	}
	if err := recordHookFired(home, "", "multica-agent-guard.sh", time.Now()); err != nil {
		t.Fatalf("record firing: %v", err)
	}

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hooks[0].Telemetry != hookTelemetryFired {
		t.Fatalf("expected fired from the hook-level record, got %q", hooks[0].Telemetry)
	}
}

// A broken telemetry store is its own answer. Reporting never_fired here would
// blame the hook for the daemon's problem.
func TestHookTelemetry_BrokenStoreIsUncollectable(t *testing.T) {
	home := fakeHome(t)
	writeInstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))
	writeHookFixture(t, hookTelemetryPath(home), "{ this is not json")

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hooks[0].Telemetry != hookTelemetryUncollectable {
		t.Fatalf("expected uncollectable, got %q", hooks[0].Telemetry)
	}
}

// ---------------------------------------------------------------------------
// Path normalization
// ---------------------------------------------------------------------------

func TestNormalizeHookCommandPath(t *testing.T) {
	home := "/home/tester"
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{"expands $HOME and collapses it back", "$HOME/.claude/hooks/multica-a.sh", "~/.claude/hooks/multica-a.sh"},
		{"expands braced HOME", "${HOME}/.claude/hooks/multica-a.sh", "~/.claude/hooks/multica-a.sh"},
		{"expands tilde", "~/.claude/hooks/multica-a.sh", "~/.claude/hooks/multica-a.sh"},
		{"collapses a literal home path", "/home/tester/hooks/multica-a.sh", "~/hooks/multica-a.sh"},
		{"drops arguments", "$HOME/hooks/multica-a.sh --token secret", "~/hooks/multica-a.sh"},
		{"keeps a quoted path with spaces", `"/opt/My Tools/multica-a.sh" --flag`, "/opt/My Tools/multica-a.sh"},
		{"keeps a single-quoted path", `'/opt/My Tools/multica-a.sh'`, "/opt/My Tools/multica-a.sh"},
		{"leaves an unrelated absolute path alone", "/usr/local/bin/multica", "/usr/local/bin/multica"},
		{"empty command", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeHookCommandPath(tc.command, home); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIsMulticaHookPath(t *testing.T) {
	// The test is on the script path, so a third-party hook that merely
	// mentions Multica in an argument stays out of the payload.
	if isMulticaHookPath(normalizeHookCommandPath("/opt/other/hook.sh --tool multica", "/home/tester")) {
		t.Fatal("a third-party script must not be reported because an argument says multica")
	}
	if !isMulticaHookPath("~/.claude/hooks/multica-agent-guard.sh") {
		t.Fatal("a multica script must be reported")
	}
	if isMulticaHookPath("") {
		t.Fatal("an empty path is not a hook")
	}
}

func TestSnakeCaseEvent(t *testing.T) {
	for input, want := range map[string]string{
		"PostToolUse":      "post_tool_use",
		"SessionStart":     "session_start",
		"UserPromptSubmit": "user_prompt_submit",
		"session_start":    "session_start",
	} {
		if got := snakeCaseEvent(input); got != want {
			t.Fatalf("snakeCaseEvent(%q) = %q, want %q", input, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The local firing endpoint
// ---------------------------------------------------------------------------

// A firing is learned only because the hook said so. Nothing infers one from a
// log, which is what keeps never_fired falsifiable.
func TestHookFiredHandler_RecordsFiringAndFlipsTheScan(t *testing.T) {
	home := fakeHome(t)
	writeInstrumentedHook(t, home, "multica-agent-guard.sh")
	writeHookFixture(t, filepath.Join(home, ".claude", "settings.json"), claudeSettingsWithHook("multica-agent-guard.sh"))
	if err := markHookTelemetryObserving(home, time.Now()); err != nil {
		t.Fatalf("mark observing: %v", err)
	}

	d := &Daemon{logger: slog.Default()}
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"hook": "multica-agent-guard.sh", "event": "PreToolUse"}`)
	d.hookFiredHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hook/fired", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	hooks, _, err := scanRuntimeHooks("claude")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hooks[0].Telemetry != hookTelemetryFired {
		t.Fatalf("a reported firing must flip the scan to fired, got %q", hooks[0].Telemetry)
	}
	if hooks[0].LastFiredAt == "" {
		t.Fatal("a fired hook must carry last_fired_at")
	}
}

func TestHookFiredHandler_RejectsBadRequests(t *testing.T) {
	fakeHome(t)
	d := &Daemon{logger: slog.Default()}

	rec := httptest.NewRecorder()
	d.hookFiredHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hook/fired", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	d.hookFiredHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hook/fired", strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on a malformed body, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	d.hookFiredHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hook/fired", strings.NewReader(`{"event": "PreToolUse"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no hook is named, got %d", rec.Code)
	}
}
