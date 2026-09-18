package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"clawdh/internal/accounts"
	"clawdh/internal/config"
	"clawdh/internal/switching"
)

// cmdHook runs one of the hooks clawdh installs into Claude Code. Today the only
// one is the UserPromptSubmit switch trigger.
func cmdHook(args []string) int {
	if len(args) == 0 || args[0] != "user-prompt-submit" {
		fmt.Fprintln(os.Stderr, "usage: clawdh hook user-prompt-submit")
		return 2
	}
	return hookUserPromptSubmit()
}

// hookUserPromptSubmit is Claude Code's UserPromptSubmit hook. It reads the
// hook payload on stdin and, when the prompt is a `clawdh <name>` switch command
// inside a `clawdh run` supervisor, records the switch and tells Claude Code to
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

	// A shared (gateway) session runs on a key, not a local login, so there is
	// nothing to switch in place. Say that, by name, rather than the misleading
	// "this session was not started by clawdh".
	if shared := os.Getenv(sharedSessionEnvVar); shared != "" {
		return block("This is a shared session (" + shared + "), and a shared session can't change accounts in place. " +
			"Exit it, then run `clawdh " + name + "` for an account on this machine or `clawdh shared <name>` for a shared one. `clawdh list` shows both.")
	}

	list, err := loadAccounts()
	if err != nil {
		return 0
	}
	acct, ok := switching.ResolveAccount(list, name)
	if !ok {
		// A name that is not an account used to pass through to the model,
		// which answered `clawdh saif` as though it were a question and left the
		// user with no sign that clawdh had seen it at all. Someone who types
		// `clawdh <word>` meant clawdh, so say what went wrong and what the
		// accounts actually are.
		return block(fmt.Sprintf("There is no clawdh account called %q. Accounts on this machine: %s.\nIf you meant to ask me something, put it in a sentence — `clawdh <name>` on its own is the switch command.",
			name, accountNames(list)))
	}

	// With a supervisor, hand it the switch: relaunching the session is the
	// only way to change the login it reads, and the supervisor is the only
	// thing that can relaunch it.
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

	// No supervisor. Switching used to be possible from here, by writing the
	// credential store this session was reading — that write path is gone, and
	// with it the class of bug that destroyed two real logins. Say plainly that
	// this session cannot be switched rather than failing silently.
	return block("This session was not started by clawdh, so it cannot be switched to " +
		displayName(acct) + ". Start sessions with `clawdh <account>` and the same command switches them.")
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
