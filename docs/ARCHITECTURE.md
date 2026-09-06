# Architecture

ccam is a single Go binary (`cmd/ccam`) with no runtime dependencies and no
cgo, so it cross-compiles trivially for darwin/linux/windows ×
amd64/arm64 (`CGO_ENABLED=0`).

## Packages

- **`internal/accounts`** — the source of truth for account metadata. Each
  account is a row in `~/.ccam/accounts.json` plus a directory under
  `~/.ccam/accounts/<slug>/` used as that account's `CLAUDE_CONFIG_DIR`.
  `Prober` checks whether a directory already holds a successful login: it
  looks for a `.credentials.json` file first, and falls back to a short
  headless `claude -p ... --max-turns 1` probe for the case where a backend
  (e.g. the macOS Keychain) doesn't write a file — see the note on Claude
  Code's credential storage below.

- **`internal/ptyio`** — starts a child process attached to a pseudo
  terminal. `claude`'s interactive login flow needs a real TTY to render (it
  hangs on a plain pipe), so this is unavoidable. Unix and Windows need
  genuinely different OS APIs — a real pty (`github.com/creack/pty`) vs the
  ConPTY API (`github.com/UserExistsError/conpty`) — so this is the one
  package where that split lives; everything else only sees the `Session`
  interface.

- **`internal/ptyauth`** — drives one login attempt: spawns `claude` via
  `ptyio`, parses its output into a virtual terminal screen
  (`github.com/hinshun/vt10x` — needed because `claude`'s TUI repaints/clears
  lines rather than printing plain scrolling text), regex-scans the rendered
  screen for the OAuth URL, and polls `accounts.Prober` for completion.
  Emits a small `Event` stream: `url` → `linked`/`failed`/`timeout`.

- **`internal/shellrc`** — maintains one idempotent, clearly delimited block
  (`# >>> ccam accounts >>> ... <<< ccam accounts <<<`) inside each shell's rc
  file, never touching anything outside it. One alias per account, e.g.
  `alias claude-work='CLAUDE_CONFIG_DIR="..." claude'`. Covers bash, zsh,
  fish, and PowerShell (Core, on every OS, plus Windows PowerShell's default
  profile path).

- **`internal/termlauncher`** — opens a new terminal window already scoped
  to one account, per OS (`osascript`/Terminal.app, a
  `gnome-terminal`/`konsole`/`xterm` fallback chain, `wt.exe`/PowerShell).

- **`internal/service`** — per-user autostart registration and process
  lifecycle. Only autostart-artifact registration differs per OS (a
  LaunchAgent plist, a systemd `--user` unit or XDG autostart `.desktop`
  entry, or a Startup-folder script / non-elevated Scheduled Task); `Start`,
  `Stop`, and `IsRunning` are identical everywhere (`generic.go`), built on a
  pidfile plus a check that the HTTP server actually answers on its recorded
  port. None of the OS service managers are configured to supervise/restart
  ccam — they're used only to start it once at login — so there's no risk of
  a service manager silently reviving a process this package just stopped.

- **`internal/httpserver`** — the REST + SSE API and the embedded web UI
  (`internal/httpserver/webui`, plain HTML/CSS/JS via `embed.FS`, no build
  step). Binds `127.0.0.1` only.

- **`internal/cli`** — subcommand wiring (`install`, `uninstall`, `start`,
  `stop`, `status`, `serve`, `version`). The only package that touches
  `os.Args`, exit codes, or signal handling.

## Why a headless probe, not macOS Keychain code

Claude Code's credential storage is file-based on Linux/Windows, and on
macOS may additionally use the Keychain — keyed off `CLAUDE_CONFIG_DIR`
itself, so two accounts (two different config dirs) never collide. Rather
than reimplement Anthropic's exact Keychain key derivation, ccam treats
"can I make a trivial authenticated call with this `CLAUDE_CONFIG_DIR`" as
the single source of truth for "is this account linked" — correct regardless
of which backend a given OS/version of `claude` happens to use, and keeps
`CGO_ENABLED=0` viable everywhere.

## Testing strategy

A real Anthropic OAuth login can't happen headlessly in CI. `testdata/fakeclaude`
is a minimal stand-in that renders an auth URL and then writes a fake
credentials file, exercising every piece of ccam's own logic (PTY spawn,
screen scraping, SSE event delivery, status transitions, alias sync) without
touching Anthropic's servers. `test/e2e` drives the real built `ccam` binary
through install → add account → log in → uninstall against `fakeclaude`,
on all three OSes in CI. What that can't cover — a real login actually
succeeding against Anthropic's servers — is the one thing left to
[`docs/MANUAL_VERIFICATION.md`](MANUAL_VERIFICATION.md).
