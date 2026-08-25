package agent

// HookMechanismSupported reports whether a provider has a hook mechanism at
// all — a place on disk where the user can register "run this command when X
// happens".
//
// It exists so "this runtime has zero Multica hooks installed" and "this
// runtime cannot have hooks in the first place" stay two different answers.
// Collapsing them into an empty list is the bug this whole inventory is meant
// to avoid: an empty list reads as "supported, and you installed none", which
// sends someone off to debug an installation that was never possible.
//
// Both the daemon scanner and the workspace read endpoint go through this one
// function, so a provider cannot be supported on the machine and unsupported
// in the API.
func HookMechanismSupported(providerType string) bool {
	switch providerType {
	case "claude":
		// ~/.claude/settings.json -> hooks.<Event>[] with matcher / if.
		return true
	case "codex":
		// ~/.codex/hooks.json -> hooks.<event>[]; ~/.codex/config.toml carries
		// the `hooks = true` master switch and [hooks.state] trust records.
		return true
	case "zcode":
		// ~/.zcode/cli/config.json -> hooks.events.<Event>[] with matcher,
		// plus a hooks.enabled master switch. Verified against a real install
		// (COC-341): the file carried five Multica hooks.
		return true
	default:
		// Everything else Multica can drive is an inventory blank, not an
		// inventory of zero. Add a case only alongside a reader in
		// internal/daemon/hook_scan.go — the two must agree.
		return false
	}
}
