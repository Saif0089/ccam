// Package service registers/unregisters clawdh as a per-user background
// process that starts at login, and starts/stops it. Every
// implementation is strictly per-user: none of them ever need
// admin/root/elevation, by construction (LaunchAgent in the user's own
// Library, systemd --user, or a Startup-folder shortcut — never a
// system service, root LaunchDaemon, or elevated Scheduled Task).
package service

// Service is the OS-specific autostart + process-lifecycle backend.
type Service interface {
	// Install registers clawdh to start at the next login and returns the
	// path(s) it wrote, for diagnostics/tests.
	Install(binaryPath string, port int) (artifactPath string, err error)
	// Uninstall removes whatever Install wrote. Uninstalling something
	// that was never installed is not an error.
	Uninstall() error
	// IsInstalled reports whether the autostart artifact currently exists.
	IsInstalled() (bool, error)
	// Start launches the service now (without waiting for the next
	// login), if it isn't already running.
	Start() error
	// Stop stops the currently running service, if any.
	Stop() error
	// IsRunning reports whether the service process is currently up.
	IsRunning() (bool, error)
}

// New returns the Service implementation for the current OS. binaryPath
// is the clawdh executable to run (normally its own os.Executable()); port
// is the port it should serve on.
func New(binaryPath string, port int) Service {
	return newPlatformService(binaryPath, port)
}

// RemoveLegacy best-effort removes a prior ccam install's autostart
// registration — the old service under the com.ccam.agent / ccam.service /
// ccam.desktop names (and the old Windows Startup script / scheduled task) —
// and stops it if it is running. It never errors: installing clawdh on a
// machine that never had ccam simply finds nothing to remove.
//
// The clean cutover is the install script running the old `ccam uninstall`,
// which also strips the old rc block and removes the old binary. This is the
// safety net for a manual `clawdh install` where the old binary is already gone
// but its autostart entry would otherwise start a second daemon that fights the
// new one for the port.
func RemoveLegacy() { removeLegacyPlatform() }
