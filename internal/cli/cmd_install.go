package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"ccam/internal/config"
	"ccam/internal/service"
)

func cmdInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	port := fs.Int("port", config.DefaultPort, "port to listen on")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	binaryPath, err := resolveBinaryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	svc := service.New(binaryPath, *port)
	artifact, err := svc.Install(binaryPath, *port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam: registering autostart failed:", err)
		return 1
	}
	fmt.Printf("Registered autostart at login (%s)\n", artifact)

	// Stop whatever is already serving before starting this build.
	// Installing over a running ccam is the normal upgrade path, and
	// Start() no-ops when something already answers — so without this,
	// an upgrade would leave the *old* binary running (the installer
	// replaces the file, but the running process keeps its old inode)
	// and cheerfully report success.
	if running, _ := service.IsHTTPRunning(); running {
		if err := svc.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "ccam: could not stop the running ccam:", err)
			return 1
		}
	}

	if err := svc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam: starting service failed:", err)
		return 1
	}

	if info := waitForRunning(15 * time.Second); info != nil {
		fmt.Printf("ccam is running: http://127.0.0.1:%d\n", info.Port)
	} else {
		fmt.Println("ccam was started but isn't answering yet; check `ccam status` shortly, or the log at ~/.ccam/ccam.log")
	}
	return 0
}

// waitForRunning polls until a ccam server answers, returning what it
// reported about itself (notably the port it actually bound, which is
// not necessarily the one that was requested).
func waitForRunning(timeout time.Duration) *service.RunningInfo {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := service.Running(); err == nil && info != nil {
			return info
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil
}
