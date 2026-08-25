package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/pelletier/go-toml/v2"
)

// Hook inventory: what Multica hooks a machine actually has installed, per
// provider, and whether anyone has ever seen them run.
//
// PRIVACY IS THE HARD CONSTRAINT HERE. This payload leaves the user's machine.
// Only Multica's own hooks are reported (decided by the hook's script path),
// and only these facts about each: which script, which event, the trigger
// PATTERN, whether the provider considers it live, and its telemetry state.
//
// Never add command arguments, the expanded command line, environment values,
// or anything read out of the script body. Hook commands routinely contain
// absolute paths into unrelated tools, API tokens passed as flags, and inlined
// shell — none of that is Multica's to collect.

const (
	hookTelemetryFired         = "fired"
	hookTelemetryNeverFired    = "never_fired"
	hookTelemetryUnobserved    = "unobserved"
	hookTelemetryUncollectable = "uncollectable"
)

// hookScanInterval is how often each runtime re-reads its provider's hook
// config. Hook files change when a human edits them, which is rare; the point
// of re-scanning at all is that an uninstall must eventually become visible,
// not that it becomes visible instantly.
const hookScanInterval = 10 * time.Minute

// hookInventoryEntry is the wire shape of one reported hook. Every field here
// is deliberate; see the privacy note above before adding another.
type hookInventoryEntry struct {
	// Script file name, e.g. "multica-worktree-sync.sh". Identity within a
	// (runtime, event).
	HookName string `json:"hook_name"`
	// The provider's own event name, verbatim. Providers disagree about
	// casing and vocabulary and Multica does not arbitrate: a name that has
	// been translated cannot be pasted back into the config file it came from.
	Event string `json:"event"`
	// The matcher / if pattern string itself. Not what it matched.
	TriggerSpec string `json:"trigger_spec"`
	// Script path with the user's home collapsed to `~`. Arguments dropped.
	CommandPath string `json:"command_path"`
	Enabled     bool   `json:"enabled"`
	Telemetry   string `json:"telemetry"`
	// RFC3339, omitted unless Telemetry is "fired".
	LastFiredAt string `json:"last_fired_at,omitempty"`
}

// hookConfigEntry is the pre-privacy-filter form: it still carries the raw
// command, which must not escape this file.
type hookConfigEntry struct {
	event      string
	trigger    string
	rawCommand string
	enabled    bool
}

// hookEventGroup is the shape claude, codex and zcode all happen to share for
// one entry under an event: an optional matcher/if plus a list of commands.
// The three differ in where the event map lives and in what disables it, not
// in this.
type hookEventGroup struct {
	Matcher string          `json:"matcher"`
	If      json.RawMessage `json:"if"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"hooks"`
}

// scanRuntimeHooks returns this machine's Multica hook inventory for one
// provider.
//
// The second return value is the distinction the whole feature rests on:
// false means the provider has no hook mechanism, which is NOT the same
// answer as an empty list from a provider that has one. A caller that
// collapses them tells the user to go debug an installation that was never
// possible.
func scanRuntimeHooks(provider string) ([]hookInventoryEntry, bool, error) {
	if !agent.HookMechanismSupported(provider) {
		return nil, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, true, fmt.Errorf("resolve user home: %w", err)
	}

	var raw []hookConfigEntry
	switch provider {
	case "claude":
		raw, err = readClaudeHookConfig(home)
	case "codex":
		raw, err = readCodexHookConfig(home)
	case "zcode":
		raw, err = readZcodeHookConfig(home)
	default:
		// agent.HookMechanismSupported said yes and no reader exists: the two
		// have drifted. Refuse rather than report a false empty inventory.
		return nil, true, fmt.Errorf("provider %q claims hook support with no reader", provider)
	}
	if err != nil {
		return nil, true, err
	}

	telemetry, telemetryErr := loadHookTelemetry(home)
	entries := buildHookInventory(raw, home, telemetry, telemetryErr)
	return entries, true, nil
}

// buildHookInventory applies the privacy filter, resolves telemetry, and
// collapses duplicate identities. Split out from scanRuntimeHooks so it can be
// tested without a home directory.
func buildHookInventory(raw []hookConfigEntry, home string, telemetry *hookTelemetryState, telemetryErr error) []hookInventoryEntry {
	byIdentity := make(map[string]int)
	entries := make([]hookInventoryEntry, 0, len(raw))

	for _, cfg := range raw {
		commandPath := normalizeHookCommandPath(cfg.rawCommand, home)
		if !isMulticaHookPath(commandPath) {
			continue
		}
		name := filepath.Base(commandPath)
		if name == "." || name == string(filepath.Separator) {
			name = commandPath
		}

		state, firedAt := resolveHookTelemetry(telemetry, telemetryErr, commandPath, home, name, cfg.event)
		entry := hookInventoryEntry{
			HookName:    name,
			Event:       cfg.event,
			TriggerSpec: cfg.trigger,
			CommandPath: commandPath,
			Enabled:     cfg.enabled,
			Telemetry:   state,
		}
		if state == hookTelemetryFired && !firedAt.IsZero() {
			entry.LastFiredAt = firedAt.UTC().Format(time.RFC3339)
		}

		// The identity the server upserts on is (hook_name, event), so two
		// registrations of the same script on the same event are one row. That
		// happens legitimately — the same guard registered under two matchers —
		// and the honest merge is to keep both patterns, not to pick one and
		// silently drop the other.
		if idx, seen := byIdentity[name+"\x1f"+cfg.event]; seen {
			existing := &entries[idx]
			if entry.TriggerSpec != "" && !strings.Contains(existing.TriggerSpec, entry.TriggerSpec) {
				if existing.TriggerSpec == "" {
					existing.TriggerSpec = entry.TriggerSpec
				} else {
					existing.TriggerSpec += ", " + entry.TriggerSpec
				}
			}
			// Live in one registration is live.
			existing.Enabled = existing.Enabled || entry.Enabled
			continue
		}
		byIdentity[name+"\x1f"+cfg.event] = len(entries)
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Event != entries[j].Event {
			return entries[i].Event < entries[j].Event
		}
		return entries[i].HookName < entries[j].HookName
	})
	return entries
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// readClaudeHookConfig reads ~/.claude/settings.json. Claude groups hooks by
// event; each group carries a matcher and a list of commands.
func readClaudeHookConfig(home string) ([]hookConfigEntry, error) {
	path := filepath.Join(home, ".claude", "settings.json")
	raw, err := readOptionalFile(path)
	if err != nil || raw == nil {
		return nil, err
	}
	var doc struct {
		Hooks           map[string][]hookEventGroup `json:"hooks"`
		DisableAllHooks bool                        `json:"disableAllHooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return flattenHookEventMap(doc.Hooks, !doc.DisableAllHooks, nil), nil
}

// readZcodeHookConfig reads ~/.zcode/cli/config.json.
//
// The frozen spec says zcode has no hook mechanism. It does — this reader was
// written against a real install that carried five Multica hooks under
// hooks.events, with a hooks.enabled master switch. Reporting "unsupported"
// for a machine whose hooks are demonstrably installed would hide exactly what
// this inventory exists to show. See the COC-341 report.
func readZcodeHookConfig(home string) ([]hookConfigEntry, error) {
	path := filepath.Join(home, ".zcode", "cli", "config.json")
	raw, err := readOptionalFile(path)
	if err != nil || raw == nil {
		return nil, err
	}
	var doc struct {
		Hooks struct {
			Events map[string][]hookEventGroup `json:"events"`
			// Absent means on: zcode writes this key when the user toggles it,
			// and a missing key is not evidence of "off".
			Enabled *bool `json:"enabled"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	enabled := doc.Hooks.Enabled == nil || *doc.Hooks.Enabled
	return flattenHookEventMap(doc.Hooks.Events, enabled, nil), nil
}

// readCodexHookConfig reads ~/.codex/hooks.json plus the two switches in
// ~/.codex/config.toml that decide whether an installed hook actually runs:
// the `[features] hooks` master switch, and the per-hook trust records under
// [hooks.state].
//
// Codex will not execute a hook it has no trust record for, so an entry with
// no record is installed-but-dead. Reporting it as enabled would be the same
// lie in the other direction as reporting a never-observed hook as never
// fired.
func readCodexHookConfig(home string) ([]hookConfigEntry, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	hooksPath := filepath.Join(codexHome, "hooks.json")
	raw, err := readOptionalFile(hooksPath)
	if err != nil || raw == nil {
		return nil, err
	}
	var doc struct {
		Hooks map[string][]hookEventGroup `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", hooksPath, err)
	}

	featureOn, trusted, err := readCodexHookSwitches(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		return nil, err
	}

	// Trust keys are "<source>:<snake_case_event>:<groupIndex>:<hookIndex>".
	// Verified against a live config: PostToolUse group 2 hook 0 in
	// hooks.json appears as "<path>:post_tool_use:2:0".
	trustKey := func(event string, groupIdx, hookIdx int) string {
		if trusted == nil {
			// No [hooks.state] table at all: this codex build does not gate on
			// trust, so absence is not a signal.
			return ""
		}
		return fmt.Sprintf("%s:%s:%d:%d", hooksPath, snakeCaseEvent(event), groupIdx, hookIdx)
	}
	liveness := func(event string, groupIdx, hookIdx int) bool {
		if !featureOn {
			return false
		}
		key := trustKey(event, groupIdx, hookIdx)
		if key == "" {
			return true
		}
		return trusted[key]
	}
	return flattenHookEventMap(doc.Hooks, featureOn, liveness), nil
}

// readCodexHookSwitches returns codex's hooks master switch and the set of
// trust-record keys. A missing config.toml means neither switch is expressed,
// which reads as "on, no trust gate" — the only reading that cannot invent a
// disabled hook out of a missing file.
func readCodexHookSwitches(path string) (bool, map[string]bool, error) {
	raw, err := readOptionalFile(path)
	if err != nil {
		return false, nil, err
	}
	if raw == nil {
		return true, nil, nil
	}
	var doc struct {
		Features struct {
			Hooks *bool `json:"hooks" toml:"hooks"`
		} `json:"features" toml:"features"`
		// Older layout carried the switch at the top level.
		Hooks struct {
			State map[string]any `json:"state" toml:"state"`
		} `json:"hooks" toml:"hooks"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return false, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	featureOn := doc.Features.Hooks == nil || *doc.Features.Hooks
	if doc.Hooks.State == nil {
		return featureOn, nil, nil
	}
	trusted := make(map[string]bool, len(doc.Hooks.State))
	for key := range doc.Hooks.State {
		trusted[key] = true
	}
	return featureOn, trusted, nil
}

// flattenHookEventMap turns a provider's event->groups map into flat entries.
// liveness, when non-nil, overrides `enabled` per (event, group, hook) — codex
// uses it for its per-hook trust gate.
func flattenHookEventMap(
	events map[string][]hookEventGroup,
	enabled bool,
	liveness func(event string, groupIdx, hookIdx int) bool,
) []hookConfigEntry {
	if len(events) == 0 {
		return nil
	}
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]hookConfigEntry, 0, len(events))
	for _, event := range names {
		for groupIdx, group := range events[event] {
			trigger := hookTriggerSpec(group)
			for hookIdx, hook := range group.Hooks {
				if hook.Type != "" && hook.Type != "command" {
					continue
				}
				live := enabled
				if live && liveness != nil {
					live = liveness(event, groupIdx, hookIdx)
				}
				out = append(out, hookConfigEntry{
					event:      event,
					trigger:    trigger,
					rawCommand: hook.Command,
					enabled:    live,
				})
			}
		}
	}
	return out
}

// hookTriggerSpec renders the group's trigger condition as the pattern string
// it literally is. An `if` that is not a plain string is re-serialized rather
// than described, because a paraphrase cannot be compared against the config
// file the user is looking at.
func hookTriggerSpec(group hookEventGroup) string {
	parts := make([]string, 0, 2)
	if matcher := strings.TrimSpace(group.Matcher); matcher != "" {
		parts = append(parts, matcher)
	}
	if len(group.If) > 0 && string(group.If) != "null" {
		var asString string
		if json.Unmarshal(group.If, &asString) == nil {
			if trimmed := strings.TrimSpace(asString); trimmed != "" {
				parts = append(parts, "if:"+trimmed)
			}
		} else {
			parts = append(parts, "if:"+string(group.If))
		}
	}
	return strings.Join(parts, " ")
}

// snakeCaseEvent converts a provider's PascalCase event name to the snake_case
// codex uses in its trust keys. Used ONLY to look up a trust record; the
// reported event name is always the provider's own.
func snakeCaseEvent(event string) string {
	var b strings.Builder
	for i, r := range event {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Privacy filter
// ---------------------------------------------------------------------------

// isMulticaHookPath decides what leaves the machine. The test is on the SCRIPT
// PATH, not the whole command: a third-party hook that happens to mention
// Multica in an argument is still a third-party hook, and its command line is
// none of our business.
func isMulticaHookPath(commandPath string) bool {
	if commandPath == "" {
		return false
	}
	return strings.Contains(strings.ToLower(commandPath), "multica")
}

// normalizeHookCommandPath extracts the executable from a hook command and
// normalizes it for reporting: shell-quoting removed, $HOME/~ expanded so the
// path can be compared, then the user's home collapsed back to `~` so the
// account name never leaves the machine.
//
// Everything after the first token is dropped on purpose. That is where
// arguments live, and arguments are not ours to upload.
func normalizeHookCommandPath(command, home string) string {
	token := firstShellToken(command)
	if token == "" {
		return ""
	}
	expanded := expandHomePrefix(token, home)
	if home != "" && expanded != home && strings.HasPrefix(expanded, home+string(filepath.Separator)) {
		return "~" + expanded[len(home):]
	}
	return expanded
}

// firstShellToken returns the first word of a shell command, honouring single
// and double quotes so a path containing spaces survives intact.
func firstShellToken(command string) string {
	var b strings.Builder
	var quote rune
	started := false
	for _, r := range strings.TrimSpace(command) {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n':
			if started || b.Len() > 0 {
				return b.String()
			}
		default:
			started = true
			b.WriteRune(r)
		}
	}
	return b.String()
}

// expandHomePrefix resolves a leading ~, $HOME or ${HOME}. Only the prefix:
// a $HOME in the middle of a path is not a home reference worth guessing at.
func expandHomePrefix(path, home string) string {
	if home == "" {
		return path
	}
	switch {
	case path == "~":
		return home
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(home, path[2:])
	case strings.HasPrefix(path, "$HOME/"):
		return filepath.Join(home, path[len("$HOME/"):])
	case strings.HasPrefix(path, "${HOME}/"):
		return filepath.Join(home, path[len("${HOME}/"):])
	case path == "$HOME" || path == "${HOME}":
		return home
	default:
		return path
	}
}

// ---------------------------------------------------------------------------
// Telemetry
// ---------------------------------------------------------------------------

// hookTelemetryState is the machine-local record of hook firings, written by
// the daemon's /hook/fired endpoint and read back at scan time.
//
// ObservingSince is when this machine first had somewhere to record a firing.
// Without it, a hook installed long before the endpoint existed and a hook
// installed after it are indistinguishable, and the first would be reported as
// "never fired" — an accusation nobody was in a position to make.
type hookTelemetryState struct {
	ObservingSince time.Time                    `json:"observing_since"`
	Hooks          map[string]hookTelemetryFire `json:"hooks"`
}

type hookTelemetryFire struct {
	LastFiredAt time.Time `json:"last_fired_at"`
	Count       int64     `json:"count"`
}

func hookTelemetryPath(home string) string {
	return filepath.Join(home, ".multica", "hook-telemetry.json")
}

// hookTelemetryKey identifies one firing record. Two levels on purpose: the
// specific (event, hook) pair, and the hook alone. The same script is
// routinely registered under several events and several providers, and a
// script that does not know which event invoked it can still report itself.
func hookTelemetryKey(event, hookName string) string {
	if event == "" {
		return hookName
	}
	return event + "\x1f" + hookName
}

func loadHookTelemetry(home string) (*hookTelemetryState, error) {
	raw, err := readOptionalFile(hookTelemetryPath(home))
	if err != nil {
		return nil, err
	}
	state := &hookTelemetryState{Hooks: map[string]hookTelemetryFire{}}
	if raw == nil {
		return state, nil
	}
	if err := json.Unmarshal(raw, state); err != nil {
		return nil, fmt.Errorf("parse hook telemetry: %w", err)
	}
	if state.Hooks == nil {
		state.Hooks = map[string]hookTelemetryFire{}
	}
	return state, nil
}

// resolveHookTelemetry decides which of the four states a hook is in.
//
// The order matters and encodes the rule the card exists for: a hook is only
// called never_fired once we can prove someone was watching it. Proof is that
// the script itself carries the telemetry call — a script that never reports
// cannot be said to have not fired, only to be unobserved.
func resolveHookTelemetry(
	state *hookTelemetryState,
	loadErr error,
	commandPath, home, hookName, event string,
) (string, time.Time) {
	if loadErr != nil || state == nil {
		// The channel itself is broken. Say so rather than picking a value
		// that reads like an observation.
		return hookTelemetryUncollectable, time.Time{}
	}
	if fire, ok := state.Hooks[hookTelemetryKey(event, hookName)]; ok && !fire.LastFiredAt.IsZero() {
		return hookTelemetryFired, fire.LastFiredAt
	}
	if fire, ok := state.Hooks[hookTelemetryKey("", hookName)]; ok && !fire.LastFiredAt.IsZero() {
		return hookTelemetryFired, fire.LastFiredAt
	}
	if state.ObservingSince.IsZero() {
		return hookTelemetryUnobserved, time.Time{}
	}
	if !hookScriptReportsTelemetry(commandPath, home) {
		return hookTelemetryUnobserved, time.Time{}
	}
	return hookTelemetryNeverFired, time.Time{}
}

// hookTelemetryMarkers are what an instrumented hook script contains. Reading
// the script for these is a LOCAL read: the marker check produces a boolean,
// never a line of the file, and nothing from the body is reported.
var hookTelemetryMarkers = []string{"MULTICA_HOOK_FIRED_URL", "/hook/fired"}

func hookScriptReportsTelemetry(commandPath, home string) bool {
	if commandPath == "" {
		return false
	}
	path := expandHomePrefix(commandPath, home)
	if !filepath.IsAbs(path) {
		// A bare command name (`multica`, something on PATH). We cannot read
		// it here, so we cannot claim to be watching it.
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 1<<20 {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, marker := range hookTelemetryMarkers {
		if strings.Contains(string(raw), marker) {
			return true
		}
	}
	return false
}

// recordHookFired persists one firing. It also stamps ObservingSince the first
// time it runs, which is what later lets never_fired be said at all.
func recordHookFired(home, event, hookName string, firedAt time.Time) error {
	state, err := loadHookTelemetry(home)
	if err != nil {
		// A corrupt record must not silently swallow every future firing.
		state = &hookTelemetryState{Hooks: map[string]hookTelemetryFire{}}
	}
	if state.ObservingSince.IsZero() {
		state.ObservingSince = firedAt.UTC()
	}
	key := hookTelemetryKey(event, hookName)
	entry := state.Hooks[key]
	entry.LastFiredAt = firedAt.UTC()
	entry.Count++
	state.Hooks[key] = entry
	if event != "" {
		// Also keep the hook-level record so a scan of a DIFFERENT event
		// registration of the same script can still see that it runs.
		nameEntry := state.Hooks[hookName]
		nameEntry.LastFiredAt = firedAt.UTC()
		nameEntry.Count++
		state.Hooks[hookName] = nameEntry
	}
	return writeHookTelemetry(home, state)
}

// markHookTelemetryObserving stamps ObservingSince without recording a firing.
// The daemon calls it once at startup: the local endpoint is up, so from now
// on an instrumented hook that stays silent really has not fired.
func markHookTelemetryObserving(home string, now time.Time) error {
	state, err := loadHookTelemetry(home)
	if err != nil {
		return err
	}
	if !state.ObservingSince.IsZero() {
		return nil
	}
	state.ObservingSince = now.UTC()
	return writeHookTelemetry(home, state)
}

func writeHookTelemetry(home string, state *hookTelemetryState) error {
	path := hookTelemetryPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create telemetry dir: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hook telemetry: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write hook telemetry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace hook telemetry: %w", err)
	}
	return nil
}

// readOptionalFile returns (nil, nil) for a file that is simply not there,
// which is the normal state for every provider the user has not configured.
func readOptionalFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return raw, nil
}

// hookInventoryReportTimeout bounds one inventory upload. The inventory is
// worth retrying on the next tick, never worth blocking a heartbeat goroutine
// on.
const hookInventoryReportTimeout = 20 * time.Second

// reportHookInventory scans this machine for one runtime's Multica hooks and
// uploads the result.
//
// A scan failure is logged and dropped rather than reported as an empty
// inventory: "we could not read the config" and "the config has no Multica
// hooks" are different answers, and only one of them should ever wipe the
// server's rows.
func (d *Daemon) reportHookInventory(ctx context.Context, runtimeID string) {
	rt := d.findRuntime(runtimeID)
	if rt == nil {
		return
	}
	hooks, supported, err := scanRuntimeHooks(rt.Provider)
	if err != nil {
		d.logger.Warn("hook inventory scan failed",
			"runtime_id", runtimeID, "provider", rt.Provider, "error", err)
		return
	}
	if hooks == nil {
		hooks = []hookInventoryEntry{}
	}
	reportCtx, cancel := context.WithTimeout(ctx, hookInventoryReportTimeout)
	defer cancel()
	if err := d.client.ReportHookInventory(reportCtx, runtimeID, supported, hooks); err != nil {
		if ctx.Err() == nil {
			d.logger.Debug("hook inventory report failed",
				"runtime_id", runtimeID, "provider", rt.Provider, "error", err)
		}
		return
	}
	d.logger.Debug("hook inventory reported",
		"runtime_id", runtimeID, "provider", rt.Provider, "supported", supported, "count", len(hooks))
}
