package claudebin

import (
	"runtime"
	"testing"
)

func TestInvocationPassesThroughRealExecutables(t *testing.T) {
	name, args := Invocation("/usr/local/bin/claude", []string{"auth", "status"})
	if name != "/usr/local/bin/claude" {
		t.Errorf("name = %q, want the binary unchanged", name)
	}
	if len(args) != 2 || args[0] != "auth" {
		t.Errorf("args = %v, want them unchanged", args)
	}
}

// TestInvocationWrapsBatchFiles covers the npm install on Windows, which
// puts a `claude.cmd` shim on PATH. CreateProcess — what both os/exec
// and ConPTY end up calling — cannot execute a .cmd directly, so it has
// to go through cmd.exe.
func TestInvocationWrapsBatchFiles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("batch wrapping only applies on Windows")
	}
	name, args := Invocation(`C:\Users\me\AppData\Roaming\npm\claude.cmd`, []string{"auth", "status"})
	if name != "cmd.exe" {
		t.Errorf("name = %q, want cmd.exe", name)
	}
	want := []string{"/c", `C:\Users\me\AppData\Roaming\npm\claude.cmd`, "auth", "status"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}
