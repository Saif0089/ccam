//go:build windows

package service

import (
	"fmt"
	"log"
	"strconv"
)

// Respawn starts a fresh ccam server on port from binaryPath — after an
// update has replaced that file — and records its pid. The caller exits
// once it returns.
//
// Windows has no execve, so unlike the Unix path this really is a
// separate process. Nothing supervises ccam here (the autostart entry
// only runs it at logon), so there is no service manager to notice the
// old process leaving and tidy up the new one with it.
//
// Only called once the listener is closed, so the successor can bind
// the port immediately.
func Respawn(binaryPath string, port int) error {
	if binaryPath == "" {
		return fmt.Errorf("no binary to restart")
	}
	pid, err := spawnDetached(binaryPath, []string{"serve", "--port", strconv.Itoa(port)})
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
