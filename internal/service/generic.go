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

// Stop ends the running server and verifies it actually stopped.
//
// The pidfile alone isn't trusted for that verdict: it can be missing
// (a cleaned ~/.ccam, or a RecordSelf that failed) while the server is
// very much alive, and reporting "Stopped." in that case is how
// `ccam uninstall` ends up deleting its own binary and autostart entry
// while leaving an unstoppable server holding the port.
func (g generic) Stop() error {
	pid, err := readPID()
	if err != nil {
		return err
	}
	if pid > 0 && processAlive(pid) {
		if err := killProcess(pid); err != nil {
			return err
		}
		// Give it a moment to release the port before returning, so a
		// following Start() doesn't race an in-flight shutdown.
		for i := 0; i < 30; i++ {
			if !processAlive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if err := removePIDFile(); err != nil {
		return err
	}

	// Whatever the pidfile said, the question that matters is whether
	// anything is still serving.
	for i := 0; i < 20; i++ {
		running, err := IsHTTPRunning()
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	info, _ := Running()
	port := g.port
	if info != nil {
		port = info.Port
	}
	return fmt.Errorf("a ccam server is still answering on port %d and could not be stopped "+
		"(no matching pid on file); stop that process manually, then retry", port)
}

func (g generic) IsRunning() (bool, error) {
	return IsHTTPRunning()
}
