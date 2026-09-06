// Command fakeclaude stands in for the real `claude` CLI in tests and CI,
// where a real Anthropic OAuth login isn't possible headlessly. It's just
// enough of `claude`'s observable behavior for ccam's plumbing to be
// tested against: an interactive login mode that prints an auth URL then
// writes credentials, and a non-interactive "-p" probe mode that succeeds
// only once credentials exist.
//
// It is never shipped — it's built and put on PATH only by tests and the
// e2e CI workflow. See docs/MANUAL_VERIFICATION.md for the real-login
// checklist this can't replace.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".claude")
	}

	if hasFlag("-p") {
		runHeadlessProbe(configDir)
		return
	}

	runInteractiveLogin(configDir)
}

func hasFlag(name string) bool {
	for _, a := range os.Args[1:] {
		if a == name {
			return true
		}
	}
	return false
}

// runHeadlessProbe mimics `claude -p ... --max-turns 1`: it succeeds only
// if this config dir already has credentials, otherwise it exits non-zero
// the way a real un-authenticated headless invocation would.
func runHeadlessProbe(configDir string) {
	info, err := os.Stat(filepath.Join(configDir, ".credentials.json"))
	if err != nil || info.Size() == 0 {
		fmt.Fprintln(os.Stderr, "fakeclaude: not authenticated")
		os.Exit(1)
	}
	fmt.Println("pong")
}

// runInteractiveLogin mimics the first-run login flow: it prints a banner
// and an auth URL (what ptyauth's URL scanner looks for), then — standing
// in for a human completing OAuth in their browser — writes a credentials
// file after a short delay and reports success.
func runInteractiveLogin(configDir string) {
	fmt.Println("Welcome to fakeclaude!")
	fmt.Println("To authenticate, open this URL in your browser:")
	fmt.Println("  https://fake-auth.example.com/oauth?code=demo-12345")
	fmt.Println()

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: creating config dir:", err)
		os.Exit(1)
	}

	if os.Getenv("FAKECLAUDE_NEVER_COMPLETE") != "" {
		// Simulate a login the user never finishes: sit here until the
		// test/caller kills the process.
		select {}
	}

	// Simulate the human taking a moment to complete OAuth in a real
	// browser before the CLI observes success.
	time.Sleep(300 * time.Millisecond)

	credsPath := filepath.Join(configDir, ".credentials.json")
	fakeCreds := `{"fake":true,"issuedAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(credsPath, []byte(fakeCreds), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: writing credentials:", err)
		os.Exit(1)
	}

	fmt.Println("Login successful. You are now authenticated.")
}
