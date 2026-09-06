//go:build windows

package cli

import (
	"fmt"
	"os/exec"
	"syscall"
)

// deleteSelfBinary removes path, ccam's own currently-running
// executable. Windows normally locks a running exe's file against
// deletion, so this spawns a short-lived detached helper that waits for
// this process to exit and then deletes the file — the same trick
// self-updating Windows installers use.
func deleteSelfBinary(path string) error {
	cmd := exec.Command("cmd", "/C", fmt.Sprintf(`ping 127.0.0.1 -n 2 >nul & del /f /q "%s"`, path))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
