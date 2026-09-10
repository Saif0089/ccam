package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	list, err := loadAccounts()
	if err != nil {
		return 0
	}
	acct, ok := switching.ResolveAccount(list, name)
	if !ok {
		// A name that is not an account used to pass through to the model,
		// which answered `ccam saif` as though it were a question and left the
		// user with no sign that ccam had seen it at all. Someone who types
		// `ccam <word>` meant ccam, so say what went wrong and what the
		// accounts actually are.
		return block(fmt.Sprintf("There is no ccam account called %q. Accounts on this machine: %s.\nIf you meant to ask me something, put it in a sentence — `ccam <name>` on its own is the switch command.",
			name, accountNames(list)))
	}

	// With a supervisor, hand it the switch: it owns the session's store and,
	// if the switch cannot be applied in place, it is the only thing that can
	// relaunch the session instead.
	if handoff := os.Getenv(switching.HandoffEnvVar); handoff != "" {
		// Any answer left over from a previous switch would otherwise be read
		// as the answer to this one.
		switching.ClearOutcome(handoff)
		if err := switching.WriteHandoff(handoff, switching.Handoff{Account: acct.Slug, SessionID: in.SessionID}); err != nil {
			return 0
		}
		// Wait for the supervisor to say what it actually did, and report that
		// — it cannot say so itself without writing over the screen Claude Code
		// is drawing. Timing out is not a failure: the supervisor may be
		// relaunching the session, which rebuilds the screen anyway.
		if outcome, ok := switching.AwaitOutcome(handoff, switchReportTimeout, 0); ok {
			return block(outcome.Message)
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

// switchReportTimeout is how long the hook waits for the supervisor to report
// what it did. The supervisor notices a staged switch within
// switchPollInterval (150ms) and the work itself is a credential-store write,
// so this is generous; it only has to be short enough that a supervisor which
// is not going to answer does not hold up the prompt.
const switchReportTimeout = 3 * time.Second

// accountNames lists what the user could have meant, for an error message.
func accountNames(list []accounts.Account) string {
	if len(list) == 0 {
		return "none yet"
	}
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.Slug)
	}
	return strings.Join(names, ", ")
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
