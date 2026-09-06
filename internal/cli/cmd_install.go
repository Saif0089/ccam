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

	if err := svc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam: starting service failed:", err)
		return 1
	}

	if waitForHTTP(5 * time.Second) {
		fmt.Printf("ccam is running: http://127.0.0.1:%d\n", *port)
	} else {
		fmt.Println("ccam was started but isn't answering yet; check `ccam status` shortly, or the log at ~/.ccam/ccam.log")
	}
	return 0
}

func waitForHTTP(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if running, err := service.IsHTTPRunning(); err == nil && running {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}
