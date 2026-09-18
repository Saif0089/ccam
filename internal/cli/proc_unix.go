//go:build !windows

package cli

import (
	"os"
	"strconv"
	"syscall"

	"clawdh/internal/switching"
)

// supervisorAlive reports whether the `clawdh run` supervisor that exported
// CLAWDH_SUPERVISOR is still running. Signal 0 asks the kernel about the process
// without touching it: no such process means the session that would have
// consumed a handoff is long gone. EPERM means it exists under another user,
// which is not our supervisor either.
func supervisorAlive() bool {
	pid, err := strconv.Atoi(os.Getenv(switching.SupervisorEnvVar))
	if err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
