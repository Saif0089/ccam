package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// An editor calls its wrapper as `<wrapper> <real-claude-binary> [args...]` —
// the setting holds one executable path, so there is nowhere to put a
// subcommand. Without recognising that shape ccam printed its usage and exited
// 1, and the Claude Code extension showed "Claude Code process exited with
// code 1" and no chat at all.
func TestAnExecutablePathIsTheEditorWrapperInvocation(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	notExec := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notExec, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !isExecutablePath(bin) {
		t.Error("an executable file is the wrapper invocation")
	}
	if isExecutablePath(dir) {
		t.Error("a directory is not a program to run")
	}
	if isExecutablePath(filepath.Join(dir, "missing")) {
		t.Error("a path that is not there is not a program to run")
	}
	if isExecutablePath("ehti") || isExecutablePath("status") || isExecutablePath("") {
		t.Error("an account name or subcommand must never be taken for a path")
	}
	if runtime.GOOS != "windows" && isExecutablePath(notExec) {
		t.Error("a file with no executable bit is not a program to run")
	}
}
