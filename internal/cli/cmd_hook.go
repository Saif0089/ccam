package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"ccam/internal/accounts"
	"ccam/internal/config"
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
	handoff := os.Getenv(switching.HandoffEnvVar)
	input, _ := io.ReadAll(os.Stdin)
	if handoff == "" {
		return 0 // no supervisor → nothing to switch; let the prompt through
	}

	var in struct {
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return 0
	}
	name, ok := switching.ParseTrigger(in.Prompt)
	if !ok {
		return 0
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

	// Stage the switch for the supervisor and stop the trigger reaching the
	// model. On any failure, fall through and let the prompt run normally.
	if err := switching.WriteHandoff(handoff, switching.Handoff{Account: acct.Slug, SessionID: in.SessionID}); err != nil {
		return 0
	}
	out, err := switching.BlockDecisionJSON("Switching to " + displayName(acct) + "…")
	if err != nil {
		return 0
	}
	os.Stdout.Write(out)
	return 0
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
