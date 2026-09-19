package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Remote browsing is confined to ~/.claude: a path under it resolves, and any
// attempt to escape — an absolute path elsewhere, a "..", or the home folder
// itself — is refused. resolveClaudePath is the whole of that boundary.
func TestResolveClaudePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	claudeRootOverride = root
	t.Cleanup(func() { claudeRootOverride = "" })

	ok := map[string]string{
		"":                 root,
		"~":                root,
		".":                root,
		"projects":         filepath.Join(root, "projects"),
		"~/projects":       filepath.Join(root, "projects"),
		root + "/projects": filepath.Join(root, "projects"),
	}
	for in, want := range ok {
		got, err := resolveClaudePath(in)
		if err != nil || got != want {
			t.Errorf("resolveClaudePath(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
	for _, bad := range []string{"/etc/hosts", "..", "~/../secrets", "projects/../../elsewhere", filepath.Dir(root)} {
		if got, err := resolveClaudePath(bad); err == nil {
			t.Errorf("resolveClaudePath(%q) = %q, nil; want an out-of-bounds error", bad, got)
		}
	}
}

// jobLs lists a folder (names/dir/size, no contents) and jobGet ships one file;
// together they are the file browser's navigate + fetch — confined to ~/.claude.
func TestJobLsAndGet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	claudeRootOverride = dir
	t.Cleanup(func() { claudeRootOverride = "" })
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi there"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}

	res, status := jobLs(dir)
	if status != "done" {
		t.Fatalf("ls status = %q (%s)", status, res)
	}
	var ls struct {
		Path    string
		Entries []struct {
			Name string
			Dir  bool
		}
	}
	if err := json.Unmarshal([]byte(res), &ls); err != nil {
		t.Fatal(err)
	}
	dirs := map[string]bool{}
	for _, e := range ls.Entries {
		dirs[e.Name] = e.Dir
	}
	if _, ok := dirs["hello.txt"]; !ok {
		t.Error("ls did not list hello.txt")
	}
	if !dirs["sub"] {
		t.Error("ls should mark sub as a directory")
	}

	res, status = jobGet(filepath.Join(dir, "hello.txt"))
	if status != "done" {
		t.Fatalf("get status = %q", status)
	}
	var g struct {
		Encoding, Content string
		Size              int64
	}
	if err := json.Unmarshal([]byte(res), &g); err != nil {
		t.Fatal(err)
	}
	if g.Encoding != "utf8" || g.Content != "hi there" || g.Size != 8 {
		t.Errorf("get = %+v, want utf8 'hi there' size 8", g)
	}

	if _, status := jobGet(dir); status != "error" {
		t.Error("get on a folder should be an error")
	}
	if _, status := jobGet(filepath.Join(dir, "nope")); status != "error" {
		t.Error("get on a missing file should be an error")
	}

	// Out of scope: neither ls nor get may reach outside ~/.claude.
	if _, status := jobLs("/etc"); status != "error" {
		t.Error("ls outside ~/.claude must be refused")
	}
	if _, status := jobGet("/etc/hosts"); status != "error" {
		t.Error("get outside ~/.claude must be refused")
	}
}
