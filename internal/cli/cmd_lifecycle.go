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
	svc := service.New(binaryPath, *port)
	if err := svc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if waitForHTTP(5 * time.Second) {
		fmt.Printf("ccam is running: http://127.0.0.1:%d\n", *port)
	} else {
		fmt.Println("ccam was started but isn't answering yet; check `ccam status` shortly.")
	}
	return 0
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
	running, err := service.IsHTTPRunning()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if !running {
		fmt.Println("ccam is not running.")
		return 1
	}
	fmt.Printf("ccam is running: http://127.0.0.1:%s\n", currentPortOrUnknown())
	return 0
}
