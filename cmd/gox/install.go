package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	_ "embed"
)

// global-ok: required by //go:embed (compiler restriction — must be a package-level var).
//
//go:embed hooks/claude_stop.sh
var claudeStopScript []byte

// claudeHookCommand is the exact "command" string written into the hook
// entry. We use it both when registering and when detecting an existing
// install (idempotency + migration).
const claudeHookCommand = "$HOME/.claude/gox-hook.sh"

// claudeHookEvent is the hook event this hook is registered under. It was
// previously "PostToolUse" (ran on every Edit/Write — too frequent); it is
// now "Stop", which fires once per turn.
const claudeHookEvent = "Stop"

// claudeHookTimeout is the per-call timeout (seconds).
const claudeHookTimeout = 30

// grokHookCommand is the exact "command" string written into the Grok hook
// entry under ~/.grok/hooks/gox.json.
const grokHookCommand = "$HOME/.grok/hooks/gox-hook.sh"

// grokHookEvent is the hook event (Stop, same as Claude).
const grokHookEvent = "Stop"

// grokHookTimeout is the per-call timeout (seconds).
const grokHookTimeout = 30

func runInstall(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "gox install: target required (currently supported: claude, grok)")
		return 2
	}
	switch args[0] {
	case "claude":
		return installClaude()
	case "grok":
		return installGrok()
	default:
		fmt.Fprintf(os.Stderr, "gox install: unknown target %q (supported: claude, grok)\n", args[0])
		return 2
	}
}

func installClaude() int {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot resolve home dir:", homeErr)
		return 2
	}
	claudeDir := filepath.Join(home, ".claude")
	if mkErr := os.MkdirAll(claudeDir, 0o755); mkErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot create ~/.claude:", mkErr)
		return 2
	}

	scriptPath := filepath.Join(claudeDir, "gox-hook.sh")
	if writeErr := os.WriteFile(scriptPath, claudeStopScript, 0o755); writeErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot write hook script:", writeErr)
		return 2
	}
	fmt.Printf("✓ wrote %s\n", scriptPath)

	settingsPath := filepath.Join(claudeDir, "settings.json")
	added, migrated, settingsErr := registerClaudeHook(settingsPath)
	if settingsErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot update settings.json:", settingsErr)
		return 2
	}
	switch {
	case migrated:
		fmt.Printf("✓ migrated gox hook from PostToolUse to %s in %s\n", claudeHookEvent, settingsPath)
	case added:
		fmt.Printf("✓ registered %s hook in %s\n", claudeHookEvent, settingsPath)
	default:
		fmt.Printf("✓ hook already registered in %s (script refreshed)\n", settingsPath)
	}
	warnIfNoJQ()
	fmt.Println()
	fmt.Println("note: Claude Code only re-reads settings.json when /hooks is opened or the")
	fmt.Println("      app is restarted, so the hook activates in a new session (or right after")
	fmt.Println("      opening /hooks once in the current one).")
	return 0
}

func installGrok() int {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot resolve home dir:", homeErr)
		return 2
	}
	grokHooksDir := filepath.Join(home, ".grok", "hooks")
	if mkErr := os.MkdirAll(grokHooksDir, 0o755); mkErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot create ~/.grok/hooks:", mkErr)
		return 2
	}

	scriptPath := filepath.Join(grokHooksDir, "gox-hook.sh")
	if writeErr := os.WriteFile(scriptPath, claudeStopScript, 0o755); writeErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot write hook script:", writeErr)
		return 2
	}
	fmt.Printf("✓ wrote %s\n", scriptPath)

	hookFile := filepath.Join(grokHooksDir, "gox.json")
	added, regErr := registerGrokHook(hookFile)
	if regErr != nil {
		fmt.Fprintln(os.Stderr, "gox install: cannot update gox.json:", regErr)
		return 2
	}
	if added {
		fmt.Printf("✓ registered %s hook in %s\n", grokHookEvent, hookFile)
	} else {
		fmt.Printf("✓ hook already registered in %s (script refreshed)\n", hookFile)
	}
	warnIfNoJQ()
	fmt.Println()
	fmt.Println("note: Grok re-reads ~/.grok/hooks/*.json on session start and when you press")
	fmt.Println("      `l` (reload) inside the /hooks modal (Ctrl+L).")
	return 0
}

// registerClaudeHook reads settings.json (if it exists), removes any stale
// gox PostToolUse entry from an older install, inserts or refreshes the gox
// Stop hook entry, and writes it back. It returns:
//   - added:    true if a new Stop entry was appended
//   - migrated: true if a stale PostToolUse entry was removed in the process
//
// If the Stop entry was already present and no stale entry existed, both are
// false. Every other key in settings.json is preserved untouched.
func registerClaudeHook(path string) (added, migrated bool, err error) {
	settings := map[string]any{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return false, false, fmt.Errorf("parse %s: %w", path, jsonErr)
		}
	case os.IsNotExist(readErr):
		// fresh install
	default:
		return false, false, fmt.Errorf("read %s: %w", path, readErr)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok || hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}

	// Migration: drop any PostToolUse entry that ran our hook command (from
	// gox <= 0.1.0, when this was a PostToolUse hook).
	if postArr, ok := hooks["PostToolUse"].([]any); ok {
		kept := make([]any, 0, len(postArr))
		for _, raw := range postArr {
			if entryHasCommand(raw, claudeHookCommand) {
				migrated = true
				continue
			}
			kept = append(kept, raw)
		}
		switch len(kept) {
		case 0:
			delete(hooks, "PostToolUse")
		default:
			hooks["PostToolUse"] = kept
		}
	}

	stopArr, stopOK := hooks[claudeHookEvent].([]any)
	if !stopOK {
		stopArr = nil
	}

	// Look for an existing Stop entry with our exact command.
	alreadyInstalled := slices.ContainsFunc(stopArr, func(raw any) bool { // any-ok: hook entries decoded as untyped JSON.
		return entryHasCommand(raw, claudeHookCommand)
	})

	if !alreadyInstalled {
		newEntry := map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": claudeHookCommand,
					"timeout": claudeHookTimeout,
				},
			},
		}
		hooks[claudeHookEvent] = append(stopArr, newEntry)
		added = true
	}

	if !added && !migrated {
		// Nothing structural changed — leave the file byte-identical.
		return false, false, nil
	}

	out, marshalErr := json.MarshalIndent(settings, "", "  ")
	if marshalErr != nil {
		return false, false, fmt.Errorf("marshal settings: %w", marshalErr)
	}
	out = append(out, '\n')
	if wErr := writeFileAtomic(path, out); wErr != nil {
		return false, false, wErr
	}
	return added, migrated, nil
}

// entryHasCommand reports whether a hooks-array entry (the object that
// holds an inner "hooks" list) contains a command hook running the given
// exact command string.
func entryHasCommand(raw any, want string) bool { // any-ok: hook entries decoded as untyped JSON.
	entry, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	inner, okInner := entry["hooks"].([]any)
	if !okInner {
		return false
	}
	for _, rh := range inner {
		h, ok := rh.(map[string]any)
		if !ok {
			continue
		}
		if h["type"] == "command" && h["command"] == want {
			return true
		}
	}
	return false
}

// entryHasGoxCommand is a compatibility wrapper (used by older call sites
// before the two-target refactor). Prefer entryHasCommand directly.
func entryHasGoxCommand(raw any) bool { // any-ok: hook entries decoded as untyped JSON.
	return entryHasCommand(raw, claudeHookCommand)
}

// registerGrokHook reads ~/.grok/hooks/gox.json (if present), ensures a Stop
// entry for our hook command exists (appending if needed), and writes back
// preserving any other events or user content in the file. Returns added=true
// only if we actually appended a new entry.
func registerGrokHook(path string) (added bool, err error) {
	content := map[string]any{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if jsonErr := json.Unmarshal(data, &content); jsonErr != nil {
			return false, fmt.Errorf("parse %s: %w", path, jsonErr)
		}
	case os.IsNotExist(readErr):
		// fresh file — we will create it with just our hook
	default:
		return false, fmt.Errorf("read %s: %w", path, readErr)
	}

	hooks, ok := content["hooks"].(map[string]any)
	if !ok || hooks == nil {
		hooks = map[string]any{}
		content["hooks"] = hooks
	}

	stopArr, stopOK := hooks[grokHookEvent].([]any)
	if !stopOK {
		stopArr = nil
	}

	alreadyInstalled := slices.ContainsFunc(stopArr, func(raw any) bool { // any-ok: hook entries decoded as untyped JSON.
		return entryHasCommand(raw, grokHookCommand)
	})

	if !alreadyInstalled {
		newEntry := map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": grokHookCommand,
					"timeout": grokHookTimeout,
				},
			},
		}
		hooks[grokHookEvent] = append(stopArr, newEntry)
		added = true
	}

	if !added {
		// Nothing changed — leave the file byte-identical if it existed.
		return false, nil
	}

	out, marshalErr := json.MarshalIndent(content, "", "  ")
	if marshalErr != nil {
		return false, fmt.Errorf("marshal: %w", marshalErr)
	}
	out = append(out, '\n')
	if wErr := writeFileAtomic(path, out); wErr != nil {
		return false, wErr
	}
	return true, nil
}

// writeFileAtomic replaces path with data via a temp file + rename. When
// path already exists its permission bits are kept (settings files are
// often 0600 because they can hold tokens); new files get 0644.
func writeFileAtomic(path string, data []byte) error {
	perm := os.FileMode(0o644)
	if st, statErr := os.Stat(path); statErr == nil {
		perm = st.Mode().Perm()
	}
	tmp := path + ".tmp"
	if wErr := os.WriteFile(tmp, data, perm); wErr != nil {
		return fmt.Errorf("write tmp: %w", wErr)
	}
	// WriteFile's perm is filtered by the umask; set it explicitly.
	if chErr := os.Chmod(tmp, perm); chErr != nil {
		return fmt.Errorf("chmod tmp: %w", chErr)
	}
	if rnErr := os.Rename(tmp, path); rnErr != nil {
		return fmt.Errorf("rename: %w", rnErr)
	}
	return nil
}

// warnIfNoJQ prints a warning when jq is missing: the hook script needs it
// to read the payload and to emit the block decision, and without it the
// hook silently does nothing.
func warnIfNoJQ() {
	if _, lookErr := exec.LookPath("jq"); lookErr != nil {
		fmt.Fprintln(os.Stderr, "warning: `jq` not found in PATH — the gox hook requires it and stays inactive until it is installed")
	}
}
