package switching

import (
	"encoding/json"
	"os"
	"strings"
)

// hookSignature identifies ccam's own UserPromptSubmit hook among any others
// the user has. It is the tail of the command line the hook runs, so it
// matches regardless of where the ccam binary lives.
const hookSignature = "hook user-prompt-submit"

// HookCommand is the command string Claude Code runs for the switch hook,
// with the ccam binary path double-quoted so a path with spaces survives.
func HookCommand(ccamBinary string) string {
	return `"` + ccamBinary + `" ` + hookSignature
}

// EnsureUserPromptSubmitHook makes sure the user's shared settings.json runs
// ccam's switch hook on every submitted prompt, WITHOUT disturbing any other
// hooks they have (the usage-monitor and rule-reminder hooks live under the
// same event). It is idempotent and, in the steady state, writes nothing:
//   - if ccam's hook is already present with the right command, it returns
//     without touching the file, so it never reformats or churns settings.json
//     on a normal launch;
//   - if it is missing or points at an old binary path, it is added or
//     refreshed, preserving every other key and hook.
//
// A missing settings.json is created; an unparseable one is left alone (an
// error is returned) rather than clobbered.
func EnsureUserPromptSubmitHook(settingsPath, ccamBinary string) error {
	command := HookCommand(ccamBinary)

	cfg := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	ups, _ := hooks["UserPromptSubmit"].([]any)

	// Already correct → do nothing (no rewrite, no reformat).
	for _, e := range ups {
		if cmd, ok := entryCommand(e); ok && cmd == command {
			return nil
		}
	}

	ourEntry := map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": command,
			"timeout": 5,
		}},
	}

	// Replace a stale ccam entry (binary moved) or append a new one, leaving
	// every non-ccam hook exactly where it was.
	replaced := false
	for i, e := range ups {
		if cmd, ok := entryCommand(e); ok && strings.Contains(cmd, hookSignature) {
			ups[i] = ourEntry
			replaced = true
			break
		}
	}
	if !replaced {
		ups = append(ups, ourEntry)
	}
	hooks["UserPromptSubmit"] = ups
	cfg["hooks"] = hooks

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := settingsPath + ".ccam-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, settingsPath)
}

// RemoveUserPromptSubmitHook deletes ccam's switch hook from settings.json,
// leaving every other hook untouched. Used by `ccam uninstall`. A missing or
// unparseable file, or an absent hook, is a no-op.
func RemoveUserPromptSubmitHook(settingsPath string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil // don't touch a file we can't parse
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	ups, _ := hooks["UserPromptSubmit"].([]any)
	kept := ups[:0:0]
	removed := false
	for _, e := range ups {
		if cmd, ok := entryCommand(e); ok && strings.Contains(cmd, hookSignature) {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return nil
	}
	if len(kept) == 0 {
		delete(hooks, "UserPromptSubmit")
	} else {
		hooks["UserPromptSubmit"] = kept
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := settingsPath + ".ccam-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, settingsPath)
}

// entryCommand pulls the command string out of one UserPromptSubmit entry
// ({"hooks":[{"type":"command","command":"…"}]}), if it has one.
func entryCommand(entry any) (string, bool) {
	m, ok := entry.(map[string]any)
	if !ok {
		return "", false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return "", false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok {
			return cmd, true
		}
	}
	return "", false
}
