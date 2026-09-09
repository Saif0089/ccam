package switching

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// upsCommands returns every UserPromptSubmit command string in a settings map.
func upsCommands(t *testing.T, cfg map[string]any) []string {
	t.Helper()
	var out []string
	hooks, _ := cfg["hooks"].(map[string]any)
	ups, _ := hooks["UserPromptSubmit"].([]any)
	for _, e := range ups {
		if cmd, ok := entryCommand(e); ok {
			out = append(out, cmd)
		}
	}
	return out
}

func TestEnsureHookPreservesExistingHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// The user's real shape: a model setting and their own UserPromptSubmit hook.
	os.WriteFile(path, []byte(`{
      "model": "sonnet",
      "hooks": {
        "UserPromptSubmit": [
          {"hooks": [{"type":"command","command":"/Users/me/.claude/hooks/fable-nudge.sh","timeout":5}]}
        ]
      }
    }`), 0o600)

	if err := EnsureUserPromptSubmitHook(path, "/usr/local/bin/ccam"); err != nil {
		t.Fatal(err)
	}
	cfg := loadSettings(t, path)
	if cfg["model"] != "sonnet" {
		t.Error("unrelated settings must be preserved")
	}
	cmds := upsCommands(t, cfg)
	if len(cmds) != 2 {
		t.Fatalf("want 2 UserPromptSubmit hooks (theirs + ccam), got %d: %v", len(cmds), cmds)
	}
	foundFable, foundCcam := false, false
	for _, c := range cmds {
		if c == "/Users/me/.claude/hooks/fable-nudge.sh" {
			foundFable = true
		}
		if c == HookCommand("/usr/local/bin/ccam") {
			foundCcam = true
		}
	}
	if !foundFable || !foundCcam {
		t.Errorf("both hooks must be present: fable=%v ccam=%v", foundFable, foundCcam)
	}
}

func TestEnsureHookIsIdempotentAndDoesNotRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"model":"opus"}`), 0o600)

	if err := EnsureUserPromptSubmitHook(path, "/bin/ccam"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	// Second call must be a pure no-op — byte-identical, no reformat/churn.
	if err := EnsureUserPromptSubmitHook(path, "/bin/ccam"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("second EnsureHook must not rewrite the file:\n%s\n---\n%s", first, second)
	}
}

func TestEnsureHookRefreshesStalePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{}`), 0o600)
	if err := EnsureUserPromptSubmitHook(path, "/old/path/ccam"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureUserPromptSubmitHook(path, "/new/path/ccam"); err != nil {
		t.Fatal(err)
	}
	cmds := upsCommands(t, loadSettings(t, path))
	if len(cmds) != 1 || cmds[0] != HookCommand("/new/path/ccam") {
		t.Errorf("stale ccam hook should be refreshed to one entry with the new path, got %v", cmds)
	}
}

func TestRemoveHookLeavesOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{
      "hooks": {"UserPromptSubmit": [
        {"hooks":[{"type":"command","command":"/Users/me/.claude/hooks/fable-nudge.sh","timeout":5}]}
      ]}
    }`), 0o600)
	EnsureUserPromptSubmitHook(path, "/bin/ccam")

	if err := RemoveUserPromptSubmitHook(path); err != nil {
		t.Fatal(err)
	}
	cmds := upsCommands(t, loadSettings(t, path))
	if len(cmds) != 1 || cmds[0] != "/Users/me/.claude/hooks/fable-nudge.sh" {
		t.Errorf("remove must delete only ccam's hook, got %v", cmds)
	}
}
