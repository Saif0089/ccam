package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/switching"
)

// cmdExec is what an editor runs instead of Claude Code.
//
// The Claude Code extension can be told to launch Claude through another
// executable — `claudeCode.claudeProcessWrapper` — which it then runs with the
// real binary as the first argument. ccam puts itself there so that every
// conversation the editor starts gets a credential store of ITS OWN, seeded
// from the account that editor is set to.
//
// That per-conversation store is the whole point. The setting that would
// otherwise carry the account is machine-scoped: one value for the entire
// editor, so writing to it moves every conversation at once. Launching each
// conversation ourselves is the only way to give them separate stores, and
// separate stores are what let `ccam <name>`, typed in one chat, switch that
// chat and leave the others alone.
//
// It is deliberately hard to break. Anything unexpected — no account, no
// credentials, a store that cannot be written — falls through to running the
// real binary exactly as the editor would have. A user's editor must not stop
// working because ccam had an opinion.
func cmdExec(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ccam exec <claude-binary> [args...]")
		return 2
	}
	bin, passthrough := args[0], args[1:]

	// Whichever account this editor is set to, and the user's default login
	// when it has never been set — which is what a plain `claude` would have
	// used, so an editor nobody has configured behaves exactly as before. The
	// store is still per-conversation, so `ccam <name>` in one chat can move
	// that chat without touching the others.
	acct, ok := editorAccount()
	if !ok {
		return runPlainClaude(bin, passthrough)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return runPlainClaude(bin, passthrough)
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		return runPlainClaude(bin, passthrough)
	}

	creds := newSessionCreds(filepath.Join(filepath.Dir(accountsDir), "sessions"), acct, switching.LedgerPath(home))
	if creds == nil {
		return runPlainClaude(bin, passthrough)
	}
	defer creds.close()

	env := accounts.EnvForSharedConfig(creds.dir())
	cmd := exec.Command(bin, passthrough...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// The editor talks to this process over its stdio, so ccam must be a plain
	// conduit: no extra output, and the child's exit code passed straight back.
	if err := cmd.Start(); err != nil {
		return runPlainClaude(bin, passthrough)
	}
	waitErr := cmd.Wait()
	return exitCodeOf(waitErr)
}

// editorAccount is the account this editor's conversations start as: whichever
// one `ccam editor <account>` last recorded. Absent, ccam stays out of the way.
func editorAccount() (accounts.Account, bool) {
	accountsFile, err := config.AccountsFile()
	if err != nil {
		return accounts.Account{}, false
	}
	list, err := accounts.NewStore(accountsFile).Load()
	if err != nil {
		return accounts.Account{}, false
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		return accounts.Account{}, false
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(accountsDir), "editors", "default.json")); err == nil {
		var rec storeOwner
		if json.Unmarshal(data, &rec) == nil {
			for _, a := range list {
				if a.ID == rec.AccountID {
					return a, true
				}
			}
		}
	}
	// Never configured: the default login, exactly as a plain `claude` would.
	for _, a := range list {
		if a.IsDefault() {
			return a, true
		}
	}
	return accounts.Account{}, false
}
