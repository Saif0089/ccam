package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/claudebin"
	"ccam/internal/config"
	"ccam/internal/service"
	"ccam/internal/switching"
)

// switchPollInterval is how often the supervisor checks for a pending switch
// while Claude Code runs. Fast enough that the redraw feels immediate, slow
// enough to be free.
var switchPollInterval = 150 * time.Millisecond

// claudeRunner launches Claude Code and reports (exit code, whether the
// supervisor terminated it for a switch). Swapped out in tests.
var claudeRunner = runClaudeOnce

// onSwitch is called when a switch is staged while Claude Code runs. It
// returns true if it applied the switch in place — the session keeps running,
// and everything inside it survives — and false if the session has to be
// relaunched instead.
type onSwitch func(switching.Handoff) bool

// claudeCodeEnvVar is set in every process Claude Code spawns, so it tells a
// `!ccam ...` invocation that it is running inside a session even when that
// session has no ccam supervisor to switch.
const claudeCodeEnvVar = "CLAUDECODE"

// stdinIsTTY reports whether the session would own a real terminal. Swapped
// out in tests, which run with stdin on a pipe.
var stdinIsTTY = func() bool { return isatty(os.Stdin.Fd()) }

// cmdRun is the switchable session supervisor. It launches Claude Code for one
// account and stays resident: when the in-session `ccam <name>` hook records a
// switch, it relaunches Claude Code as the new account with the conversation
// resumed, in the same terminal. When Claude Code exits on its own, so does it.
func cmdRun(args []string) int {
	// --auto is how the `claude` shell wrapper calls in: the user did not name
	// an account, so ccam supervises whichever one a plain `claude` would have
	// used. Everything after it belongs to Claude Code.
	auto := len(args) > 0 && args[0] == "--auto"
	if auto {
		args = args[1:]
	}
	if !auto && len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ccam run <account> [claude args...]")
		return 2
	}
	var startName string
	passthrough := args
	if !auto {
		startName = args[0]
		passthrough = args[1:]
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	accountsFile, err := config.AccountsFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	store := accounts.NewStore(accountsFile)
	claudeBin := claudebin.Resolve()
	claudeDir := sharedClaudeDir(home)
	claudeJSON := filepath.Join(home, ".claude.json")
	settings := filepath.Join(claudeDir, "settings.json")

	// Install the switch hook into the shared settings.json (idempotent). A
	// failure only means in-session switching won't work; the session still
	// runs, so it is a warning, not fatal.
	if self, err := service.SelfPath(); err == nil {
		if err := switching.EnsureUserPromptSubmitHook(settings, self); err != nil {
			fmt.Fprintln(os.Stderr, "ccam: could not install switch hook:", err)
		}
	}

	list, err := store.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	acct, ok := switching.ResolveAccount(list, startName)
	if auto {
		acct, ok = autoAccount(list)
		if !ok {
			// Nothing to supervise (no default account registered yet): let
			// the caller fall back to plain Claude Code rather than fail.
			return runPlainClaude(claudeBin, passthrough)
		}
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "ccam: no account %q\n", startName)
		return 1
	}
	if startName == "" {
		startName = acct.Slug
	}

	// Inside a session this supervisor is already running, `ccam <account>` is
	// a switch, not a new session: stage the handoff and let the loop below
	// relaunch the terminal on the other account. This is the path `!ccam
	// <name>` takes — Claude Code runs it as a plain shell command, so it
	// never reaches the UserPromptSubmit hook, but it does inherit both the
	// handoff path and the session id from the session it was typed in.
	//
	// Two things have to hold. Only the bare form is a switch: `ccam ehti -p
	// "..."` inside a session is a deliberate one-shot on another account, and
	// staging a switch would kill the live session and throw those arguments
	// away. And the supervisor has to still be there: CCAM_HANDOFF is
	// inherited by anything a session spawned, including processes that
	// outlive it, and staging a handoff nobody will read reported a switch
	// that never happened.
	if handoffPath := os.Getenv(switching.HandoffEnvVar); handoffPath != "" && len(passthrough) == 0 && !auto {
		if supervisorAlive() {
			h := switching.Handoff{Account: acct.Slug, SessionID: os.Getenv(switching.SessionIDEnvVar)}
			if err := switching.WriteHandoff(handoffPath, h); err != nil {
				fmt.Fprintln(os.Stderr, "ccam: could not stage the switch:", err)
				return 1
			}
			fmt.Printf("Switching to %s…\n", displayName(acct))
			return 0
		}
		fmt.Fprintln(os.Stderr, "ccam: the ccam session this was launched from is gone, so there is nothing to switch.")
		return 1
	}

	// No supervisor. Starting a session here needs a terminal on stdin, and
	// without one Claude Code falls back to --print and dies with "Input must
	// be provided either through stdin or as a prompt argument" — an error
	// about a flag nobody typed. Say what is actually wrong instead.
	// Passthrough args mean the caller is driving Claude Code deliberately
	// (`ccam ehti -p "..."`), so those are left alone.
	if len(passthrough) == 0 && !stdinIsTTY() {
		if os.Getenv(claudeCodeEnvVar) != "" {
			fmt.Fprintf(os.Stderr, "ccam: this Claude Code session was not started by ccam, so `!ccam %s` cannot switch it.\n", startName)
			fmt.Fprintln(os.Stderr, "      An account is fixed when claude starts; switching in place means relaunching")
			fmt.Fprintln(os.Stderr, "      the session, which only ccam's supervisor can do.")
			fmt.Fprintf(os.Stderr, "      Start sessions as `ccam <account> [claude flags...]` — then `!ccam %s`\n", startName)
			fmt.Fprintln(os.Stderr, "      switches the running session, conversation and all.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "ccam: `ccam %s` starts an interactive Claude Code session, and stdin is not a terminal.\n", startName)
		fmt.Fprintf(os.Stderr, "      Run it in your terminal, or pass Claude Code's own arguments (`ccam %s -p \"...\"`).\n", startName)
		return 1
	}

	handoff := filepath.Join(accountsDir, fmt.Sprintf(".handoff-%d.json", os.Getpid()))
	defer os.Remove(handoff)

	// Give the session a credential store of its own, seeded from the account.
	// That is what makes switching in place possible: Claude Code re-reads its
	// store when it changes, so a switch becomes a write to THIS session's
	// store rather than a restart that kills everything running inside it.
	// Falls back to the account's own store, and to relaunching, when a private
	// store cannot be made.
	ledger := switching.LedgerPath(home)
	creds := newSessionCreds(filepath.Join(filepath.Dir(accountsDir), "sessions"), acct, ledger)
	defer creds.close()

	// Minting the session id means ccam knows it before Claude Code starts, so
	// the session's usage is attributed to the right account from its first
	// token rather than from its first switch. Only on a fresh launch: an id
	// cannot be chosen for a conversation that already has one.
	sessionID := ""
	if creds != nil && !hasSessionArgs(passthrough) {
		if id, err := newSessionID(); err == nil {
			sessionID = id
			passthrough = append([]string{"--session-id", id}, passthrough...)
			if err := switching.AppendOwnership(ledger, id, acct.ConfigDir); err != nil {
				fmt.Fprintln(os.Stderr, "ccam: could not record this session for the usage monitor:", err)
			}
		}
	}

	sessionArgs := passthrough
	for {
		applyIdentity(acct, accountsDir, claudeJSON)
		switching.ClearHandoff(handoff)

		storeDir := acct.ConfigDir
		if d := creds.dir(); d != "" {
			storeDir = d
		}
		env := append(accounts.EnvForSharedConfig(storeDir),
			switching.HandoffEnvVar+"="+handoff,
			fmt.Sprintf("%s=%d", switching.SupervisorEnvVar, os.Getpid()))

		// Claude Code refreshes its access token into whatever store it is
		// reading, so a long session's refreshed token has to be copied back to
		// the account or the account's own store goes stale.
		stopMirror := make(chan struct{})
		go func() {
			t := time.NewTicker(mirrorInterval)
			defer t.Stop()
			for {
				select {
				case <-stopMirror:
					return
				case <-t.C:
					creds.mirror()
				}
			}
		}()

		code, switched := claudeRunner(claudeBin, sessionArgs, env, handoff, func(h switching.Handoff) bool {
			fresh, err := store.Load()
			if err != nil {
				return false
			}
			next, ok := switching.ResolveAccount(fresh, h.Account)
			if !ok {
				fmt.Fprintf(os.Stderr, "ccam: cannot switch to %q\n", h.Account)
				return true // a typo is not a reason to restart the session
			}
			id := h.SessionID
			if id == "" {
				id = sessionID
			}
			if !creds.switchTo(next, id) {
				return false
			}
			applyIdentity(next, accountsDir, claudeJSON)
			acct = next
			fmt.Fprintf(os.Stderr, "\nccam: switched to %s — same session, nothing restarted.\n", displayName(next))
			return true
		})
		close(stopMirror)
		if !switched {
			return code
		}

		// The switch could not be applied in place, so this is the old path:
		// relaunch as the other account. The private store goes with it —
		// whatever stopped the write would stop it again.
		creds.close()
		creds = nil

		h, ok := switching.ReadHandoff(handoff)
		if !ok {
			return code
		}
		fresh, err := store.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ccam:", err)
			return 1
		}
		next, ok := switching.ResolveAccount(fresh, h.Account)
		if !ok {
			fmt.Fprintf(os.Stderr, "ccam: cannot switch to %q\n", h.Account)
			return 1
		}
		acct = next
		// A session that has not written a transcript yet — switched before
		// its first message — cannot be resumed, and asking anyway kills the
		// relaunch instead of switching it.
		//
		// The flags the session was started with are kept: a switch out of
		// `ccam default --dangerously-skip-permissions` that quietly dropped
		// that flag would land the user in a session that behaves differently
		// from the one they were in.
		resume := switching.ResumeArgs(h.SessionID, switching.HasTranscript(claudeDir, h.SessionID))
		sessionArgs = append(append([]string{}, passthrough...), resume...)
	}
}

// sharedClaudeDir is the ~/.claude every account shares. It deliberately
// ignores an inherited CLAUDE_CONFIG_DIR: EnvForSharedConfig strips that
// variable from the child, so ~/.claude is where Claude Code will actually
// read settings and write transcripts no matter what the launching shell had
// set. Honouring it here instead installed the switch hook in a settings.json
// Claude Code never reads, and looked for transcripts in the wrong tree.
func sharedClaudeDir(home string) string {
	return filepath.Join(home, ".claude")
}

// autoAccount is the account a plain `claude` would have run as: the one whose
// directory the shell already points at (someone who exported
// CLAUDE_SECURESTORAGE_CONFIG_DIR by hand, or one of ccam's own aliases), and
// otherwise the default login — which is exactly what `claude` does with no
// variables set at all.
func autoAccount(list []accounts.Account) (accounts.Account, bool) {
	if dir := strings.TrimSpace(os.Getenv(accounts.SecureStorageEnvVar)); dir != "" {
		want := filepath.Clean(dir)
		for _, a := range list {
			if a.ConfigDir != "" && strings.EqualFold(filepath.Clean(a.ConfigDir), want) {
				return a, true
			}
		}
	}
	for _, a := range list {
		if a.IsDefault() {
			return a, true
		}
	}
	return accounts.Account{}, false
}

// runPlainClaude is the last resort for --auto: hand the terminal to Claude
// Code exactly as the shell would have, unsupervised, rather than refuse to
// start because ccam has nothing registered.
func runPlainClaude(bin string, args []string) int {
	name, argv := claudebin.Invocation(bin, args)
	cmd := exec.Command(name, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return exitCodeOf(err)
	}
	return 0
}

// applyIdentity makes the shared ~/.claude.json name the account about to run,
// so /status and the statusline are correct. For a managed account the
// oauthAccount comes from its own stub; for the default account it comes from
// the snapshot ccam took on first boot.
func applyIdentity(acct accounts.Account, accountsDir, claudeJSON string) {
	stubDir := acct.ConfigDir
	if stubDir == "" {
		stubDir = filepath.Join(accountsDir, "default")
	}
	oa, err := accounts.ReadOAuthAccount(stubDir)
	if err != nil || oa == nil {
		return // best-effort: a missing stub just leaves the identity as-is
	}
	_ = accounts.SetActiveIdentity(claudeJSON, oa)
}

// runClaudeOnce launches Claude Code with stdio inherited (it owns the
// terminal) and watches for a pending switch. It returns when Claude Code
// exits — either on its own, or because a switch was staged and the supervisor
// terminated it.
func runClaudeOnce(bin string, args, env []string, handoff string, applyInPlace onSwitch) (int, bool) {
	name, argv := claudebin.Invocation(bin, args)
	cmd := exec.Command(name, argv...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "ccam: launching claude:", err)
		return 1, false
	}

	// The terminal delivers ctrl-c to Claude Code directly (same process
	// group), so the supervisor must not die on it — it absorbs the common
	// signals and lets Claude Code handle them.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	stop := make(chan struct{})
	switched := make(chan struct{}, 1)
	go func() {
		t := time.NewTicker(switchPollInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-sigCh:
				// absorb — Claude Code already received it from the tty
			case <-t.C:
				h, ok := switching.ReadHandoff(handoff)
				if !ok {
					continue
				}
				switching.ClearHandoff(handoff)
				// Preferred path: rewrite the credentials this session is
				// reading. Claude Code notices and continues on the other
				// account, so subagents, background tasks and the
				// conversation itself are untouched.
				if applyInPlace != nil && applyInPlace(h) {
					continue
				}
				// It could not be done in place — relaunch instead.
				if err := switching.WriteHandoff(handoff, h); err == nil {
					select {
					case switched <- struct{}{}:
					default:
					}
				}
				terminate(cmd)
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	close(stop)

	select {
	case <-switched:
		return 0, true
	default:
		return exitCodeOf(waitErr), false
	}
}

// terminate ends the child Claude Code so the supervisor can relaunch it. The
// two platforms need genuinely different endings — see terminate_unix.go and
// terminate_windows.go. cmd.Wait() in the caller reaps it either way.
func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	endChild(cmd)
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// newSessionID mints the uuid Claude Code will use for the session, so ccam
// knows it before the session exists.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// hasSessionArgs reports whether the caller already decided which conversation
// this is — resuming one, continuing one, or naming an id. ccam must not mint
// an id over the top of any of those.
func hasSessionArgs(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--session-id", a == "--resume", a == "-r", a == "--continue", a == "-c":
			return true
		case strings.HasPrefix(a, "--session-id="), strings.HasPrefix(a, "--resume="):
			return true
		}
	}
	return false
}
