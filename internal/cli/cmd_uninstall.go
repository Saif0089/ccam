package cli

import (
	"flag"
	"fmt"
	"os"

	"ccam/internal/config"
	"ccam/internal/service"
	"ccam/internal/shellrc"
)

func cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	port := fs.Int("port", config.DefaultPort, "port the service was installed on")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	binaryPath, err := resolveBinaryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	svc := service.New(binaryPath, *port)
	if err := svc.Uninstall(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam: removing autostart registration failed:", err)
		return 1
	}
	fmt.Println("Stopped the service and removed autostart registration.")

	home, err := os.UserHomeDir()
	if err == nil {
		if err := shellrc.NewSyncer(home).RemoveAll(); err != nil {
			fmt.Fprintln(os.Stderr, "ccam: warning: removing shell aliases failed:", err)
		} else {
			fmt.Println("Removed generated shell aliases.")
		}
	}

	if err := deleteSelfBinary(binaryPath); err != nil {
		fmt.Fprintln(os.Stderr, "ccam: warning: could not remove the installed binary at", binaryPath, "-", err)
		fmt.Println("You can delete it yourself; nothing else references it.")
	} else {
		fmt.Println("Removed", binaryPath)
	}

	fmt.Println("\nAccount data under ~/.ccam/accounts was left in place. Remove ~/.ccam yourself if you want a full wipe.")
	return 0
}
