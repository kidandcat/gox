package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any { // any-ok: settings files are untyped JSON.
	t.Helper()
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	out := map[string]any{} // any-ok: settings files are untyped JSON.
	if jsonErr := json.Unmarshal(data, &out); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	return out
}

// hookRef names a command registered under a hook event.
type hookRef struct{ event, command string }

func countCommand(t *testing.T, settings map[string]any, ref hookRef) int { // any-ok: settings files are untyped JSON.
	t.Helper()
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	arr, ok := hooks[ref.event].([]any)
	if !ok {
		return 0
	}
	n := 0
	for _, raw := range arr {
		if entryHasCommand(raw, ref.command) {
			n++
		}
	}
	return n
}

func TestRegisterClaudeHook_freshInstallIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	added, migrated, err := registerClaudeHook(path)
	if err != nil || !added || migrated {
		t.Fatalf("first install: added=%v migrated=%v err=%v", added, migrated, err)
	}
	before, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	added, migrated, err = registerClaudeHook(path)
	if err != nil || added || migrated {
		t.Fatalf("second install: added=%v migrated=%v err=%v", added, migrated, err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(before) != string(after) {
		t.Fatal("re-install must leave settings.json byte-identical")
	}
	if n := countCommand(t, readJSON(t, path), hookRef{claudeHookEvent, claudeHookCommand}); n != 1 {
		t.Fatalf("want exactly 1 Stop entry, got %d", n)
	}
}

func TestRegisterClaudeHook_migratesPostToolUseAndKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{
  "model": "opus",
  "hooks": {
    "PostToolUse": [
      {"matcher": "Edit", "hooks": [{"type": "command", "command": "$HOME/.claude/gox-hook.sh"}]},
      {"matcher": "Write", "hooks": [{"type": "command", "command": "other.sh"}]}
    ]
  }
}`
	if wErr := os.WriteFile(path, []byte(legacy), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	added, migrated, err := registerClaudeHook(path)
	if err != nil || !added || !migrated {
		t.Fatalf("added=%v migrated=%v err=%v", added, migrated, err)
	}
	got := readJSON(t, path)
	if got["model"] != "opus" {
		t.Errorf("unrelated key lost: %v", got["model"])
	}
	if n := countCommand(t, got, hookRef{"PostToolUse", claudeHookCommand}); n != 0 {
		t.Errorf("legacy PostToolUse entry not removed")
	}
	if n := countCommand(t, got, hookRef{"PostToolUse", "other.sh"}); n != 1 {
		t.Errorf("foreign PostToolUse entry must be kept, got %d", n)
	}
	if n := countCommand(t, got, hookRef{claudeHookEvent, claudeHookCommand}); n != 1 {
		t.Errorf("want 1 Stop entry, got %d", n)
	}
}

func TestRegisterClaudeHook_preservesFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if wErr := os.WriteFile(path, []byte(`{"apiKeyHelper":"secret.sh"}`), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	if _, _, err := registerClaudeHook(path); err != nil {
		t.Fatal(err)
	}
	st, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("settings.json mode changed to %o, want 600", st.Mode().Perm())
	}
}

func TestRegisterClaudeHook_invalidJSONIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if wErr := os.WriteFile(path, []byte(`{not json`), 0o644); wErr != nil {
		t.Fatal(wErr)
	}
	if _, _, err := registerClaudeHook(path); err == nil {
		t.Fatal("expected a parse error")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != `{not json` {
		t.Fatal("a file that failed to parse must not be rewritten")
	}
}

func TestRegisterGrokHook_idempotentAndKeepsOtherEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gox.json")
	if wErr := os.WriteFile(path, []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x.sh"}]}]}}`), 0o644); wErr != nil {
		t.Fatal(wErr)
	}
	added, err := registerGrokHook(path)
	if err != nil || !added {
		t.Fatalf("added=%v err=%v", added, err)
	}
	added, err = registerGrokHook(path)
	if err != nil || added {
		t.Fatalf("second run: added=%v err=%v", added, err)
	}
	got := readJSON(t, path)
	if n := countCommand(t, got, hookRef{grokHookEvent, grokHookCommand}); n != 1 {
		t.Errorf("want 1 Stop entry, got %d", n)
	}
	if n := countCommand(t, got, hookRef{"PreToolUse", "x.sh"}); n != 1 {
		t.Errorf("other events must be preserved, got %d", n)
	}
}
