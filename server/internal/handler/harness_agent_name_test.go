package handler

import "testing"

// The map from a harness to the agent whose name a terminal write is recorded
// under. It is closed on purpose: the header is client-controlled, so an
// unknown value has to find nothing rather than send the server looking for an
// agent named after whatever arrived.

func TestHarnessAgentName_KnownHarnesses(t *testing.T) {
	cases := map[string]string{
		"claude-code": "Claude",
		"codex":       "Codex",
		// Named after the model, not the harness — see the comment on the
		// switch. Pinned here so changing the model that shell runs cannot
		// quietly leave the attribution pointing at the old one.
		"opencode": "DeepSeek",
	}
	for harness, want := range cases {
		if got := harnessAgentName(harness); got != want {
			t.Fatalf("harnessAgentName(%q) = %q, want %q", harness, got, want)
		}
	}
}

// An unrecognised value must resolve to nothing, which makes the write fall
// back to the member. A wrong agent label is worse than the person's own name:
// it reads as a machine acting on its own.
func TestHarnessAgentName_UnknownIsEmpty(t *testing.T) {
	for _, harness := range []string{"", "cursor", "Claude", "OPENCODE", "opencode-cli", "../codex"} {
		if got := harnessAgentName(harness); got != "" {
			t.Fatalf("harnessAgentName(%q) = %q, want empty", harness, got)
		}
	}
}
