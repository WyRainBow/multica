package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// Built-in skills reach a dispatched agent because the daemon materialises
// them. A session started by hand never sees them, so the rules they carry had
// to be hand-copied into each machine's config to be available at all — and a
// copy like that goes stale silently when the platform's version moves. This
// endpoint is what lets a local mirror exist and report itself stale.

func TestBuiltinSkillsEndpointServesBodiesAndHashes(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	w := httptest.NewRecorder()
	testHandler.ListBuiltinSkills(w, httptest.NewRequest(http.MethodGet, "/api/skills/builtin", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var skills []service.AgentSkillData
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("no built-in skills served; the mirror would have nothing to copy")
	}

	byName := map[string]service.AgentSkillData{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	// The skill holding the rules that could not be deleted from the global
	// config files. If this stops being served, those rules go back to being
	// hand-maintained per machine.
	issues, ok := byName["multica-working-on-issues"]
	if !ok {
		t.Fatalf("multica-working-on-issues was not served; got %v", keysOf(byName))
	}
	if !strings.Contains(issues.Content, "409") {
		t.Error("the terminal-card freeze rule did not travel with the body")
	}
	for _, want := range []struct {
		label string
		got   string
	}{
		{"body", issues.Content},
		{"hash", issues.Hash},
		{"id", issues.ID},
		{"source", issues.Source},
	} {
		if strings.TrimSpace(want.got) == "" {
			t.Errorf("built-in skill has no %s; a mirror cannot verify what it copied", want.label)
		}
	}
	if issues.Source != "builtin" {
		t.Errorf("source = %q, want builtin — a mirror separates these from workspace skills", issues.Source)
	}
	if !strings.HasPrefix(issues.ID, "builtin:") {
		t.Errorf("id = %q, want a builtin: prefix", issues.ID)
	}
}

func TestBuiltinSkillHashesAreStableAcrossCalls(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	// Staleness is decided by comparing hashes. A hash that moves on its own
	// would report every local copy stale forever, and people stop checking.
	first := builtinHashes(t)
	second := builtinHashes(t)
	if len(first) == 0 {
		t.Fatal("no hashes to compare")
	}
	for name, hash := range first {
		if second[name] != hash {
			t.Errorf("%s hash changed between calls: %q then %q", name, hash, second[name])
		}
	}
}

func builtinHashes(t *testing.T) map[string]string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListBuiltinSkills(w, httptest.NewRequest(http.MethodGet, "/api/skills/builtin", nil))
	var skills []service.AgentSkillData
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := map[string]string{}
	for _, s := range skills {
		out[s.Name] = s.Hash
	}
	return out
}

func keysOf(m map[string]service.AgentSkillData) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
