//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
	"time"
)

// endChild asks Claude Code to stop (SIGTERM, so its SessionEnd hooks run and
// the transcript is flushed) and forces the issue if it is still there after a
// grace period. Claude Code is the process group leader of nothing here — it
// is a direct child — so signalling it directly is enough.
func endChild(cmd *exec.Cmd) {
	_ = cmd.Process.Signal(syscall.SIGTERM)
	time.AfterFunc(3*time.Second, func() { _ = cmd.Process.Kill() })
}
