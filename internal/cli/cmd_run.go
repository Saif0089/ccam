package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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

// cmdRun is the switchable session supervisor. It launches Claude Code for one
// account and stays resident: when the in-session `ccam <name>` hook records a
// switch, it relaunches Claude Code as the new account with the conversation
// resumed, in the same terminal. When Claude Code exits on its own, so does it.
func cmdRun(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ccam run <account> [claude args...]")
		return 2
	}
	startName := args[0]
	passthrough := args[1:]

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
	claudeJSON := filepath.Join(home, ".claude.json")
	settings := filepath.Join(home, ".claude", "settings.json")

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
	if !ok {
		fmt.Fprintf(os.Stderr, "ccam: no account %q\n", startName)
		return 1
	}

	handoff := filepath.Join(accountsDir, fmt.Sprintf(".handoff-%d.json", os.Getpid()))
	defer os.Remove(handoff)

	sessionArgs := passthrough
	for {
		applyIdentity(acct, accountsDir, claudeJSON)
		switching.ClearHandoff(handoff)

		env := append(accounts.EnvForSharedConfig(acct.ConfigDir),
			switching.HandoffEnvVar+"="+handoff)

		code, switched := claudeRunner(claudeBin, sessionArgs, env, handoff)
		if !switched {
			return code
		}

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
		sessionArgs = []string{"--resume", h.SessionID, "--fork-session"}
	}
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
func runClaudeOnce(bin string, args, env []string, handoff string) (int, bool) {
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
				if _, ok := switching.ReadHandoff(handoff); ok {
					select {
					case switched <- struct{}{}:
					default:
					}
					terminate(cmd)
					return
				}
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

// terminate ends the child Claude Code so the supervisor can relaunch it.
// On Unix it asks politely (SIGTERM, letting SessionEnd hooks run) and forces
// the issue after a grace period; on Windows, where there is no SIGTERM, it
// kills directly. cmd.Wait() in the caller reaps it either way.
func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	time.AfterFunc(3*time.Second, func() { _ = cmd.Process.Kill() })
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
