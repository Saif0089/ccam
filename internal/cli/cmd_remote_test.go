package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"":    home,
		"~":   home,
		"~/x": filepath.Join(home, "x"),
		"/etc/hosts": "/etc/hosts", // absolute paths pass through — the browser navigates freely
	}
	for in, want := range cases {
		if got := expandPath(in); got != want {
			t.Errorf("expandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// jobLs lists a folder (names/dir/size, no contents) and jobGet ships one file;
// together they are the file browser's navigate + fetch.
func TestJobLsAndGet(t *testing.T) {
	dir := t.TempDir()
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
}
