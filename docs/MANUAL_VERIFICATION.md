# Manual verification checklist

CI (`.github/workflows/e2e.yml`) proves the install → run → uninstall
lifecycle end-to-end on all three OSes, but it drives a fake `claude`
binary — it can't complete a real Anthropic OAuth login, since that needs
a human and a real account. Run this checklist once per OS before tagging a
release.

For each of macOS, Linux, and Windows:

1. Install via the real one-liner from the README (not a local build) on a
   clean machine/VM/container that has never had ccam on it.
2. Confirm the install command finished with no admin/sudo/UAC prompt at
   any point, and printed a `http://127.0.0.1:...` URL.
3. Open that URL. Click **+ Add account**, name it anything, and open the
   URL it shows you in your actual browser — complete a real login with a
   real Claude account.
4. Confirm the account flips to "linked" in the UI within a few seconds of
   finishing the browser login (no manual refresh needed).
5. Open a **new** terminal (a fresh one — not one already open before step
   1) and run the account's alias, e.g. `claude-work`. Confirm `claude`
   starts already authenticated as that account.
6. Repeat steps 3–5 for a second account with a second real login. Confirm
   the two accounts' `claude` sessions are independent (check `claude
   /status` or equivalent in each — different account).
7. Reboot the machine (or just log out and back in). Confirm the ccam
   service is running again automatically with no manual step, by opening
   its URL.
8. Run `ccam uninstall`. Confirm: the autostart entry is gone (check
   `~/Library/LaunchAgents` / `systemctl --user status ccam` or
   `~/.config/autostart` / the Startup folder, per OS), the shell aliases
   are gone from a **new** terminal, and the ccam binary itself is gone.
9. Confirm `~/.ccam/accounts` (the actual account credentials) is still
   present after uninstall — it should not be silently deleted.

## Probing the real `claude` CLI

`internal/ptyauth/realclaude_test.go` is a manual probe that spawns the real
`claude` against a throwaway config dir and dumps the rendered terminal
screen every two seconds, without ever completing a login:

```sh
CCAM_REAL_CLAUDE=1 go test ./internal/ptyauth/ -run TestRealClaudeLoginScreens -v
CCAM_REAL_CLAUDE=1 CCAM_CLAUDE_ARGS="auth login --claudeai" \
  go test ./internal/ptyauth/ -run TestRealClaudeLoginScreens -v
```

Run it whenever a Claude Code release might have changed the login flow.
Every assumption ccam makes about that flow — which subcommand prints a URL,
how wide the terminal must be for the URL not to wrap, whether a code has to
be pasted back — came from this probe, and `testdata/fakeclaude` is written
to match what it shows. If the two ever disagree, the fake is wrong.
