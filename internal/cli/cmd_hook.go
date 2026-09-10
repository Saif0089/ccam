package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/credstore"
	"ccam/internal/switching"
)

// cmdHook runs one of the hooks ccam installs into Claude Code. Today the only
// one is the UserPromptSubmit switch trigger.
func cmdHook(args []string) int {
	if len(args) == 0 || args[0] != "user-prompt-submit" {
		fmt.Fprintln(os.Stderr, "usage: ccam hook user-prompt-submit")
		return 2
	}
	return hookUserPromptSubmit()
}

// hookUserPromptSubmit is Claude Code's UserPromptSubmit hook. It reads the
// hook payload on stdin and, when the prompt is a `ccam <name>` switch command
// inside a `ccam run` supervisor, records the switch and tells Claude Code to
// drop the prompt. Every other prompt passes through untouched.
//
// It must be cheap: it runs before EVERY prompt in every session that shares
// this settings.json, including a plain `claude`. So it returns immediately
// unless CCAM_HANDOFF is set — i.e. unless a supervisor is actually there to
// hand off to — before doing any other work.
func hookUserPromptSubmit() int {
	input, _ := io.ReadAll(os.Stdin)

	var in struct {
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return 0
	}
	name, ok := switching.ParseTrigger(in.Prompt)
	if !ok {
		return 0 // the overwhelmingly common case: an ordinary prompt
	}

	// Only intercept a switch to a real account. A typo passes through to the
	// model rather than being silently swallowed.
	list, err := loadAccounts()
	if err != nil {
		return 0
	}
	acct, ok := switching.ResolveAccount(list, name)
	if !ok {
		return 0
	}

	// With a supervisor, hand it the switch: it owns the session's store and,
	// if the switch cannot be applied in place, it is the only thing that can
	// relaunch the session instead.
	if handoff := os.Getenv(switching.HandoffEnvVar); handoff != "" {
		if err := switching.WriteHandoff(handoff, switching.Handoff{Account: acct.Slug, SessionID: in.SessionID}); err != nil {
			return 0
		}
		return block("Switching to " + displayName(acct) + "…")
	}

	// No supervisor — an editor's conversation, or any session ccam launched
	// without one. The switch can still be done from here, because it is only a
	// write to the credential store this session is reading, and the hook runs
	// inside that session with the store's path in its environment.
	//
	// Strictly the stores ccam made for one session. Anything else — an
	// account's own directory, the user's default login — is read by every
	// other session using it, and switching this conversation must not move
	// theirs.
	store := ownSessionStore()
	if store == "" {
		return 0
	}
	if err := credstore.Copy(acct.ConfigDir, store); err != nil {
		return 0
	}
	if !credstore.Same(acct.ConfigDir, store) {
		return 0 // the write did not land where Claude Code will look
	}
	if home, err := os.UserHomeDir(); err == nil {
		_ = switching.AppendOwnership(switching.LedgerPath(home), in.SessionID, acct.ConfigDir)
		if accountsDir, err := config.AccountsDir(); err == nil {
			applyIdentity(acct, accountsDir, filepath.Join(home, ".claude.json"))
		}
	}
	return block("Switched to " + displayName(acct) + ". This conversation continues on that account.")
}

// block stops the trigger reaching the model and shows the user why.
func block(reason string) int {
	out, err := switching.BlockDecisionJSON(reason)
	if err != nil {
		return 0
	}
	os.Stdout.Write(out)
	return 0
}

// ownSessionStore is the credential store this session is reading, but only
// when ccam made it for this session alone. A store under sessions/ or
// editors/ is ccam's own; an account directory is shared by every session of
// that account, and the default login is shared by everything.
func ownSessionStore() string {
	dir := strings.TrimSpace(os.Getenv(accounts.SecureStorageEnvVar))
	if dir == "" {
		return ""
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		return ""
	}
	root := filepath.Dir(accountsDir)
	for _, own := range []string{filepath.Join(root, "sessions"), filepath.Join(root, "editors")} {
		if rel, err := filepath.Rel(own, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return dir
		}
	}
	return ""
}

func loadAccounts() ([]accounts.Account, error) {
	f, err := config.AccountsFile()
	if err != nil {
		return nil, err
	}
	return accounts.NewStore(f).Load()
}

// displayName is the human label for an account: its name if it has one, else
// its slug.
func displayName(a accounts.Account) string {
	if a.Name != "" {
		return a.Name
	}
	return a.Slug
}
