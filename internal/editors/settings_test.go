package editors

import (
	"os"
	"path/filepath"
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
