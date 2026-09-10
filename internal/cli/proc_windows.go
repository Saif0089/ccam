package cli

import (
	"os"
	"strconv"

	"golang.org/x/sys/windows"

	"ccam/internal/switching"
)

// supervisorAlive reports whether the `ccam run` supervisor that exported
// CCAM_SUPERVISOR is still running. Windows has no signal 0, so the handle is
// opened for query only and its exit code inspected: STILL_ACTIVE means the
// process is there, anything else (or a handle that cannot be opened) means
// the session that would have consumed a handoff is gone.
func supervisorAlive() bool {
	pid, err := strconv.Atoi(os.Getenv(switching.SupervisorEnvVar))
	if err != nil || pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
