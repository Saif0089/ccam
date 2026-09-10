package editors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const varName = "CLAUDE_SECURESTORAGE_CONFIG_DIR"

// settings.json belongs to the user. ccam edits one value in it and must leave
// everything else — including comments, which a JSON round trip would delete —
// exactly as it was.
func TestPointAtPreservesCommentsAndOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{
  // my editor, my rules
  "editor.fontSize": 13,
  /* block comment mentioning "claudeCode.environmentVariables" as a decoy */
  "claudeCode.preferredLocation": "panel",
  "workbench.colorTheme": "Default Dark+",
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PointAt(path, varName, "/Users/me/.ccam/editors/vscode"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	for _, keep := range []string{
		"// my editor, my rules",
		`"editor.fontSize": 13`,
		"block comment mentioning",
		`"claudeCode.preferredLocation": "panel"`,
		`"workbench.colorTheme": "Default Dark+"`,
	} {
		if !strings.Contains(out, keep) {
			t.Errorf("ccam destroyed %q:\n%s", keep, out)
		}
	}
	if dir := ReadStoreDir(path, varName); dir != "/Users/me/.ccam/editors/vscode" {
		t.Errorf("store dir reads back as %q", dir)
	}
}

// Switching an editor to another account rewrites ccam's entry and must not
// accumulate duplicates or disturb variables the user set themselves.
func TestPointAtReplacesItsOwnEntryAndKeepsTheUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{
  "claudeCode.environmentVariables": [
    { "name": "HTTPS_PROXY", "value": "http://proxy:8080" },
    { "name": "CLAUDE_SECURESTORAGE_CONFIG_DIR", "value": "/old/account" }
  ]
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PointAt(path, varName, "/new/account"); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if strings.Count(string(out), varName) != 1 {
		t.Errorf("ccam's entry was duplicated rather than replaced:\n%s", out)
	}
	if !strings.Contains(string(out), "HTTPS_PROXY") {
		t.Errorf("the user's own variable was dropped:\n%s", out)
	}
	if dir := ReadStoreDir(path, varName); dir != "/new/account" {
		t.Errorf("store dir = %q, want /new/account", dir)
	}
}

func TestPointAtCreatesTheFileAndHandlesAnEmptyObject(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new", "settings.json")
	if err := PointAt(fresh, varName, "/store"); err != nil {
		t.Fatal(err)
	}
	if got := ReadStoreDir(fresh, varName); got != "/store" {
		t.Errorf("fresh file: store dir = %q", got)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PointAt(empty, varName, "/store"); err != nil {
		t.Fatal(err)
	}
	if got := ReadStoreDir(empty, varName); got != "/store" {
		out, _ := os.ReadFile(empty)
		t.Errorf("empty object: store dir = %q, file:\n%s", got, out)
	}
}

// A settings file ccam has never touched reports no account, rather than
// guessing one.
func TestReadStoreDirOnAnUnconfiguredEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{ "editor.fontSize": 12 }`), 0o644)
	if got := ReadStoreDir(path, varName); got != "" {
		t.Errorf("want no store dir, got %q", got)
	}
	if got := ReadStoreDir(filepath.Join(t.TempDir(), "absent.json"), varName); got != "" {
		t.Errorf("want no store dir for a missing file, got %q", got)
	}
}

// The two schemes are mutually exclusive and the extension applies
// environmentVariables LAST — over the environment ccam's wrapper just built.
// So an entry left behind by the older per-editor scheme silently puts every
// conversation back on one shared store, and per-conversation switching stops
// working with nothing on screen to say so. Setting the wrapper has to take it
// away, and must leave the user's own variables alone.
func TestPointAtWrapperRemovesTheOlderPerEditorEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{
  // the user's own note
  "claudeCode.environmentVariables": [
    { "name": "CLAUDE_SECURESTORAGE_CONFIG_DIR", "value": "/Users/x/.ccam/editors/vs-code" },
    { "name": "MY_OWN_VAR", "value": "keep me" }
  ],
  "editor.minimap.enabled": false
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PointAtWrapper(path, "/usr/local/bin/ccam"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if ReadStoreDir(path, "CLAUDE_SECURESTORAGE_CONFIG_DIR") != "" {
		t.Error("the per-editor store entry survived; it will override the wrapper")
	}
	if !strings.Contains(text, "MY_OWN_VAR") || !strings.Contains(text, "keep me") {
		t.Error("a variable the user set was removed")
	}
	if !strings.Contains(text, "// the user's own note") {
		t.Error("the user's comment was lost")
	}
	if WrapperPath(path) != "/usr/local/bin/ccam" {
		t.Errorf("wrapper = %q, want the ccam binary", WrapperPath(path))
	}
	if !strings.Contains(text, `"editor.minimap.enabled": false`) {
		t.Error("an unrelated setting was disturbed")
	}
}

// An editor that never had the older entry must not gain an empty setting it
// did not ask for.
func TestPointAtWrapperLeavesAFileWithNoEnvSettingAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"editor.fontSize": 13}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PointAtWrapper(path, "/usr/local/bin/ccam"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), EnvSetting) {
		t.Errorf("added %s to a file that had none:\n%s", EnvSetting, got)
	}
}

// Uninstalling ccam has to take it back out of the launch path. The setting
// names this binary by absolute path, so leaving it behind has the extension
// launching a file that no longer exists — every conversation failing, and no
// ccam left to explain it.
func TestUnsetWrapperLeavesTheRestOfTheFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{
  // a note the user wrote
  "editor.fontSize": 13,
  "claudeCode.claudeProcessWrapper": "/Users/x/.local/bin/ccam",
  "claudeCode.preferredLocation": "panel"
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UnsetWrapper(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if WrapperPath(path) != "" {
		t.Errorf("wrapper survived: %q", WrapperPath(path))
	}
	if strings.Contains(text, WrapperSetting) {
		t.Errorf("the key is still there:\n%s", text)
	}
	for _, keep := range []string{"// a note the user wrote", `"editor.fontSize": 13`, `"claudeCode.preferredLocation": "panel"`} {
		if !strings.Contains(text, keep) {
			t.Errorf("removed more than the wrapper — lost %s:\n%s", keep, text)
		}
	}
	// What is left has to still be a settings file the editor can read.
	var doc map[string]any
	stripped := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(text, "")
	if err := json.Unmarshal([]byte(stripped), &doc); err != nil {
		t.Fatalf("settings.json is no longer valid JSON: %v\n%s", err, text)
	}
	// Removing a wrapper that is not there is not an error, and changes nothing.
	before, _ := os.ReadFile(path)
	if err := UnsetWrapper(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("removing an absent wrapper rewrote the file")
	}
}
