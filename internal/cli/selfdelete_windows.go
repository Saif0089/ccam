//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// deleteSelfBinary removes path, ccam's own currently-running
// executable. Since Windows Vista, the loader opens a process's main
// image with FILE_SHARE_DELETE, so a direct delete of a running exe's
// own file usually just works — try that first. If it doesn't (some
// AV/EDR products still lock it), fall back to a detached helper that
// waits for this process to exit and then deletes the file, the trick
// self-updating Windows installers use; it's spawned with
// CREATE_BREAKAWAY_FROM_JOB so a CI runner's job-object cleanup doesn't
// kill it before it runs.
func deleteSelfBinary(path string) error {
	if err := os.Remove(path); err == nil {
		return nil
	}

	const createBreakawayFromJob = 0x01000000
	cmd := exec.Command("cmd", "/C", fmt.Sprintf(`ping 127.0.0.1 -n 2 >nul & del /f /q "%s"`, path))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createBreakawayFromJob,
	}
	return cmd.Start()
}
