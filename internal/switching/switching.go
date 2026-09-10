// Package switching implements in-session account switching: the user types
// `ccam <name>` at the Claude prompt, a UserPromptSubmit hook intercepts it
// and records a handoff, and the `ccam run` supervisor relaunches Claude Code
// as the named account with the conversation resumed. Nothing here spawns a
// process; it is the pure trigger/handoff/decision logic the two CLI commands
// share, so it can be tested on its own.
package switching

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ccam/internal/accounts"
)

// HandoffEnvVar names the file the hook writes and the supervisor reads. The
// supervisor picks the path and exports it into Claude Code's environment, so
// the hook (a child of claude) finds it without guessing.
const HandoffEnvVar = "CCAM_HANDOFF"

// SupervisorEnvVar carries the `ccam run` supervisor's pid into the session it
// runs, so a switch staged from a shell command can tell a live supervisor
// from an inherited environment variable left over by one that has exited.
const SupervisorEnvVar = "CCAM_SUPERVISOR"

// SessionIDEnvVar is the session Claude Code exports into every process it
// spawns — hooks and the shell commands a user runs with `!`. It is how a
// switch staged from a shell command knows which conversation to carry over,
// since only the hook payload carries it otherwise.
const SessionIDEnvVar = "CLAUDE_CODE_SESSION_ID"

// Handoff is a pending switch: the account the user asked for and the session
// to resume as it.
type Handoff struct {
	Account   string `json:"account"`
	SessionID string `json:"sessionId"`
}

// ParseTrigger reports whether a submitted prompt is a switch command and, if
// so, the account name in it. Accepted forms, whitespace-trimmed:
//
//	ccam <name>
//	ccam switch <name>
//
// Anything else — extra words, a leading slash (Claude Code routes "/…" to
// command resolution before the hook ever runs), a sentence that merely starts
// with "ccam" — is not a trigger and passes through to the model untouched.
func ParseTrigger(prompt string) (name string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(prompt))
	switch {
	case len(fields) == 2 && fields[0] == "ccam":
		return fields[1], true
	case len(fields) == 3 && fields[0] == "ccam" && fields[1] == "switch":
		return fields[2], true
	default:
		return "", false
	}
}

// ResolveAccount finds the account a typed name refers to, matching (case-
// insensitively) its slug, id, alias, or the alias with the "claude-" prefix
// stripped — so `ccam ehti`, `ccam claude-ehti`, and `ccam default` all work.
// Returns the account and true, or false if nothing matches.
func ResolveAccount(list []accounts.Account, name string) (accounts.Account, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, a := range list {
		candidates := []string{a.Slug, a.ID, a.Alias, strings.TrimPrefix(a.Alias, "claude-")}
		for _, c := range candidates {
			if c != "" && strings.EqualFold(c, n) {
				return a, true
			}
		}
	}
	return accounts.Account{}, false
}

// ResumeArgs are the Claude Code flags that carry the conversation across a
// switch, given the staged session id and whether that session has anything
// recorded (see HasTranscript). The three cases are genuinely different:
//
//   - a recorded session forks exactly it, so the thread continues;
//   - a session id with nothing recorded is one switched before its first
//     message. There is no conversation to carry, and --resume on it makes
//     Claude Code exit with "No conversation found with session ID", taking
//     the terminal down with it — so start clean instead;
//   - no session id at all (a Claude Code that does not export it) falls back
//     to --continue, the most recent conversation in this directory, which is
//     the one just terminated.
func ResumeArgs(sessionID string, recorded bool) []string {
	switch {
	case strings.TrimSpace(sessionID) == "":
		return []string{"--continue"}
	case recorded:
		return []string{"--resume", sessionID, "--fork-session"}
	default:
		return nil
	}
}

// HasTranscript reports whether sessionID has a conversation recorded under
// claudeDir. Claude Code stores each one as <session id>.jsonl in a per-working-
// directory folder under projects/, so a glob answers this without reproducing
// how it slugifies a path — and a session that has not been written yet simply
// matches nothing.
func HasTranscript(claudeDir, sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(claudeDir, "projects", "*", sessionID+".jsonl"))
	return err == nil && len(matches) > 0
}

// WriteHandoff atomically writes a pending switch to path.
func WriteHandoff(path string, h Handoff) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadHandoff returns the pending switch at path, or ok=false if there is none
// (the common case — the file only exists in the instant between the hook
// writing it and the supervisor consuming it).
func ReadHandoff(path string) (Handoff, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Handoff{}, false
	}
	var h Handoff
	if err := json.Unmarshal(data, &h); err != nil || h.Account == "" {
		return Handoff{}, false
	}
	return h, true
}

// ClearHandoff removes a consumed handoff. A missing file is not an error.
func ClearHandoff(path string) {
	_ = os.Remove(path)
}

// blockDecision is the UserPromptSubmit hook output that stops the typed
// trigger from reaching the model and omits it from the transcript. The field
// names and shape are Claude Code's contract for this event.
type blockDecision struct {
	Decision           string             `json:"decision"`
	Reason             string             `json:"reason"`
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName          string `json:"hookEventName"`
	SuppressOriginalPrompt bool   `json:"suppressOriginalPrompt"`
}

// BlockDecisionJSON is what the hook prints to intercept a switch trigger:
// decision "block" (the prompt is never sent to the model) with
// suppressOriginalPrompt (the trigger word is not echoed into the
// conversation).
func BlockDecisionJSON(reason string) ([]byte, error) {
	return json.Marshal(blockDecision{
		Decision: "block",
		Reason:   reason,
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:          "UserPromptSubmit",
			SuppressOriginalPrompt: true,
		},
	})
}
