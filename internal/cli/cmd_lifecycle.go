package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"ccam/internal/config"
	"ccam/internal/service"
)

func cmdStart(args []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	port := fs.Int("port", config.DefaultPort, "port to listen on")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	binaryPath, err := resolveBinaryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	// If ccam is already up on a different port than the one asked
	// for, say so rather than printing a URL nothing is listening on.
	if info, _ := service.Running(); info != nil && info.Port != *port {
		fmt.Printf("ccam is already running on http://127.0.0.1:%d (run `ccam stop` first to move it to %d)\n", info.Port, *port)
		return 0
	}

	svc := service.New(binaryPath, *port)
	if err := svc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if info := waitForRunning(15 * time.Second); info != nil {
		fmt.Printf("ccam is running: http://127.0.0.1:%d\n", info.Port)
		return 0
	}

	fmt.Fprintln(os.Stderr, "ccam: started, but nothing is answering.")
	if reason := lastLogLine(); reason != "" {
		fmt.Fprintln(os.Stderr, "  last log line:", reason)
	}
	fmt.Fprintln(os.Stderr, "  full log: ~/.ccam/ccam.log")
	return 1
}

func cmdStop(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	port := fs.Int("port", config.DefaultPort, "port ccam was running on")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	binaryPath, err := resolveBinaryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	svc := service.New(binaryPath, *port)
	if err := svc.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	fmt.Println("Stopped.")
	return 0
}

func cmdStatus(args []string) int {
	info, err := service.Running()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if info == nil {
		fmt.Println("ccam is not running.")
		return 1
	}
	fmt.Printf("ccam %s is running: http://127.0.0.1:%d\n", info.Version, info.Port)
	return 0
}
