//go:build !windows

package service

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
)

// Respawn hands this process's port to a fresh ccam server started from
// binaryPath — after an update has replaced that file.
//
// It does it by *becoming* the new binary rather than starting one
// alongside: execve replaces the process image and keeps the pid, the
// process group, the open log file and — the part that matters — the
// cgroup. A systemd --user unit is Type=simple with the default
// KillMode=control-group, so a ccam that spawned a child and then
// exited would have systemd tear down the cgroup and kill the successor
// it just started, leaving nothing running until the next login. Same
// pid, no exit, no teardown.
//
// Only called once the listener is closed, so the new image can bind
// the port immediately.
func Respawn(binaryPath string, port int) error {
	if binaryPath == "" {
		return fmt.Errorf("no binary to restart")
	}
	args := []string{binaryPath, "serve", "--port", strconv.Itoa(port)}

	// Returns only on failure.
	execErr := syscall.Exec(binaryPath, args, os.Environ())
	log.Printf("could not exec the updated binary (%v); starting it as a separate process instead", execErr)

	pid, err := spawnDetached(binaryPath, args[1:])
	if err != nil {
		return fmt.Errorf("restarting ccam: %w", err)
	}
	// The successor is already running; a pid file that could not be
	// written is worth a log line, not an error that would make the
	// caller believe nothing started.
	if err := writePID(pid); err != nil {
		log.Printf("warning: could not record the restarted server's pid: %v", err)
	}
	return nil
}
