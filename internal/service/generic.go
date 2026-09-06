package service

import (
	"fmt"
	"strconv"
	"time"
)

// generic implements the process-lifecycle half of Service (Start, Stop,
// IsRunning) identically on every OS via spawnDetached/killProcess
// (implemented per-OS in spawn_unix.go / spawn_windows.go) plus the
// shared pidfile and port-file conventions. Only autostart-artifact
// registration (Install/Uninstall/IsInstalled) differs per OS — the
// per-platform types embed generic and add just that.
//
// Deliberately, none of the OS service managers (launchd/systemd) are
// configured to supervise/auto-restart ccam; they're used only to start
// it once at login. That keeps a single Start/Stop/IsRunning
// implementation correct everywhere, since there's no risk of a service
// manager silently reviving a process this package just told it to stop.
type generic struct {
	binaryPath string
	port       int
}

func (g generic) Start() error {
	if running, err := IsHTTPRunning(); err != nil {
		return err
	} else if running {
		return nil
	}
	pid, err := spawnDetached(g.binaryPath, []string{"serve", "--port", strconv.Itoa(g.port)})
	if err != nil {
		return fmt.Errorf("starting ccam: %w", err)
	}
	return writePID(pid)
}

func (g generic) Stop() error {
	pid, err := readPID()
	if err != nil {
		return err
	}
	if pid == 0 || !processAlive(pid) {
		return removePIDFile()
	}
	if err := killProcess(pid); err != nil {
		return err
	}
	// Give it a moment to release the port before returning, so a
	// following Start() doesn't race an in-flight shutdown.
	for i := 0; i < 20; i++ {
		if !processAlive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return removePIDFile()
}

func (g generic) IsRunning() (bool, error) {
	return IsHTTPRunning()
}
