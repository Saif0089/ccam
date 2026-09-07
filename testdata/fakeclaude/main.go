// Command fakeclaude stands in for the real `claude` CLI in tests and
// CI, where a real Anthropic OAuth login isn't possible headlessly. It
// mirrors the parts of claude's observable behaviour that ccam depends
// on, as verified against the real CLI (v2.1.x) by
// internal/ptyauth/realclaude_test.go:
//
//	claude auth status --json   -> {"loggedIn": bool, ...}, exit 0 either way
//	claude auth login --claudeai -> prints an auth URL, then either
//	                                completes on its own or waits for a
//	                                pasted code
//
// It is never shipped — tests and the e2e workflow build it and put it
// on PATH. See docs/MANUAL_VERIFICATION.md for the real-login checklist
// it can't replace.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".claude")
	}

	args := os.Args[1:]
	switch {
	case hasArgs(args, "--version"):
		fmt.Println("9.9.9 (Claude Code)")
	case hasArgs(args, "auth", "status"):
		authStatus(configDir)
	case hasArgs(args, "auth", "login"):
		authLogin(configDir)
	case hasArgs(args, "auth", "logout"):
		_ = os.Remove(credentialsPath(configDir))
	default:
		// Bare `claude` in a fresh config dir is the interactive
		// first-run flow, which just sits at a theme picker waiting for
		// keystrokes. Emulating that keeps ccam honest: if it ever goes
		// back to spawning bare `claude` for a login, the tests hang
		// exactly the way a real machine did.
		fmt.Println("Welcome to fakeclaude!")
		fmt.Println("Choose the text style that looks best with your terminal")
		fmt.Println("  1. Auto (match terminal)")
		fmt.Println("❯ 2. Dark mode ✔")
		select {}
	}
}

func hasArgs(args []string, want ...string) bool {
	matched := 0
	for _, a := range args {
		if matched < len(want) && a == want[matched] {
			matched++
		}
	}
	return matched == len(want)
}

func credentialsPath(configDir string) string {
	return filepath.Join(configDir, ".credentials.json")
}

// authStatus mirrors `claude auth status --json`: it always exits 0 and
// reports the state in the payload, so callers must read loggedIn
// rather than the exit code.
func authStatus(configDir string) {
	info, err := os.Stat(credentialsPath(configDir))
	loggedIn := err == nil && info.Size() > 0

	out := map[string]any{
		"loggedIn":    loggedIn,
		"authMethod":  "none",
		"apiProvider": "firstParty",
	}
	if loggedIn {
		out["authMethod"] = "claude.ai"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// authLogin mirrors `claude auth login --claudeai`: it prints the auth
// URL immediately (no theme picker, no login-method picker) and then
// waits, either completing on its own or on a pasted code.
func authLogin(configDir string) {
	fmt.Println("Opening browser to sign in…")
	fmt.Printf("If the browser didn't open, visit: %s\n", fakeAuthURL)
	fmt.Print("Paste code here if prompted > ")

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: creating config dir:", err)
		os.Exit(1)
	}

	switch {
	case os.Getenv("FAKECLAUDE_NEVER_COMPLETE") != "":
		// A login the user never finishes: sit here until killed.
		select {}
	case os.Getenv("FAKECLAUDE_REQUIRE_CODE") != "":
		// The paste-a-code flow: block until a line arrives on the tty.
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude: reading code:", err)
			os.Exit(1)
		}
		if len(line) < 2 {
			fmt.Fprintln(os.Stderr, "fakeclaude: empty code")
			os.Exit(1)
		}
	default:
		// Stand in for a human completing OAuth in a real browser. The
		// delay is configurable because tests that watch the UI need the
		// "here is your URL" state to be observable rather than blown
		// past in a few hundred milliseconds.
		time.Sleep(loginDelay())
	}

	writeCredentials(configDir)
	fmt.Println("\nLogin successful. You are now authenticated.")
}

// loginDelay is how long the fake waits before completing a login.
func loginDelay() time.Duration {
	if raw := os.Getenv("FAKECLAUDE_LOGIN_DELAY_MS"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 300 * time.Millisecond
}

// writeCredentials mirrors the record the real CLI stores, down to the
// field names: ccam reads it back to report the plan and how long the
// login lasts, so a placeholder blob here would make the page show a
// signed-out account right after a successful login.
func writeCredentials(configDir string) {
	now := time.Now()
	creds := fmt.Sprintf(`{"claudeAiOauth":{
		"accessToken":"fake-access-token",
		"refreshToken":"fake-refresh-token",
		"expiresAt":%d,
		"refreshTokenExpiresAt":%d,
		"scopes":["user:inference","user:profile"],
		"subscriptionType":"max",
		"rateLimitTier":"default_claude_max_20x"},
		"fake":true,"issuedAt":%q}`,
		now.Add(8*time.Hour).UnixMilli(),
		now.Add(30*24*time.Hour).UnixMilli(),
		now.UTC().Format(time.RFC3339))
	if err := os.WriteFile(credentialsPath(configDir), []byte(creds), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: writing credentials:", err)
		os.Exit(1)
	}
}

// fakeAuthURL is deliberately as long as the real one (~600 chars), so
// tests catch terminal-width truncation: at a normal 80/120-column
// width a URL this long wraps across several screen lines and anything
// scraping the rendered screen gets a broken URL.
const fakeAuthURL = "https://fake-auth.example.com/oauth/authorize?code=true&client_id=00000000-1111-2222-3333-444444444444" +
	"&response_type=code&redirect_uri=https%3A%2F%2Ffake-auth.example.com%2Foauth%2Fcode%2Fcallback" +
	"&scope=org%3Acreate_api_key+user%3Aprofile+user%3Ainference+user%3Asessions+user%3Amcp_servers+user%3Afile_upload" +
	"&code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHI&code_challenge_method=S256" +
	"&state=zyxwvutsrqponmlkjihgfedcba9876543210ZYXWVUTSRQPONMLKJIHGFEDCBA"
