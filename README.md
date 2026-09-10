# ccam — Claude Code Account Manager

Manage and use multiple [Claude Code](https://claude.com/claude-code) (`claude`
CLI) accounts on one machine, from a local web UI backed by a small
background service. No admin/root/elevation required for a normal install,
on Windows, macOS, or Linux, any architecture — and when a privileged
location genuinely is involved, it asks rather than failing.

Each account gets its own directory, exported as `CLAUDE_SECURESTORAGE_CONFIG_DIR`,
which scopes **the login and nothing else**: `CLAUDE_CONFIG_DIR` is left unset, so
every account shares your own `~/.claude` and keeps your sessions, MCP servers,
skills, plugins, hooks and `CLAUDE.md`. Switching account does not switch your
setup, and `--continue` picks up the conversation you were already in.

This started as an automation of the technique from
["Setting Up Multiple Claude Code Accounts on Your Local Machine"](https://medium.com/@buwanekasumanasekara/setting-up-multiple-claude-code-accounts-on-your-local-machine-f8769a36d1b1),
which gave each account its own `CLAUDE_CONFIG_DIR` and isolated everything with
it. Claude Code derives its credential store from either variable by the same
hash, so moving to the narrower one keeps every existing login working. ccam:

- lets you add a new account by logging in right from the browser (no manual
  `CLAUDE_CONFIG_DIR=... claude` typing),
- keeps one shell alias per account (`claude-work`, `claude-personal`, ...) in
  sync across bash, zsh, fish, and PowerShell — so opening *any* terminal and
  running that alias launches `claude` scoped to that account,
- switches accounts **without restarting anything**: in any session, type
  `ccam <name>` at the Claude prompt or run `!ccam <name>` as a shell command,
  and that session is on the other account a moment later — same process, same
  conversation, and every subagent, workflow and background task still running.
  Nothing is killed, because nothing is relaunched: ccam gives each session a
  credential store of its own and a switch rewrites it, which Claude Code
  notices by itself. (If the store cannot be written, it falls back to the old
  behaviour — relaunch with the conversation resumed — rather than pretend.)
  This works in a session you started with `ccam <account>` *and* in one you
  started by just typing `claude`: ccam puts a small `claude` function in your
  shell rc that runs the same supervisor. It steps aside — running Claude Code
  directly — inside an existing session, with no terminal (scripts, pipes, CI),
  when ccam is not on PATH, or with `CCAM_WRAP=0` set.
- runs as a per-user background service that starts at login and serves the
  UI at `http://127.0.0.1:47932`.

Switching accounts is always per-terminal (via its alias), never a global
"current account" setting — you can have several accounts' terminals open
side by side.

## Install

**macOS / Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/Saif0089/ccam/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/Saif0089/ccam/main/install.ps1 | iex
```

Both scripts install a single binary to a per-user directory (`~/.local/bin`
or `%LOCALAPPDATA%\ccam\bin`), register it to start at login, start it, and
print the URL to open. Nothing is written outside your own user profile —
no `sudo`, no `/usr/local`, no `Program Files`, no `HKLM`.

Prerequisite: the [`claude` CLI](https://claude.com/claude-code) itself must
already be installed and on `PATH` — ccam manages *accounts* for it, it
doesn't install Claude Code.

## Using it

Open `http://127.0.0.1:47932`. Click **+ Add account**, give it a name, and
open the URL it shows you to finish logging in — the account flips to
"linked" automatically once you do. Each account's card shows its alias
(`claude-work`, etc.); open a new terminal and that alias is ready to use, or
click **Open terminal** to launch one already scoped to that account.

Each card also shows that account's plan usage — the same numbers as
`/usage` inside Claude Code — with a live countdown to each reset, and two
separate clocks for the login itself: how long you stay signed in (weeks),
and the short-lived access token that Claude Code renews on its own (hours).
The dot beside the name is checked live rather than remembered: `linked`,
`login expired`, `signed out`, or `unknown` when Anthropic can't be reached.

The page keeps itself current — it re-reads every few seconds, so there is
no refresh button to press and nothing to reload. Those reads are answered
by ccam itself; it asks Anthropic for fresh numbers at most once a minute
per account. That endpoint is not a documented API and publishes no rate
limit, so when it does refuse, ccam waits — a minute, then two, four,
eight, up to fifteen — and keeps showing the last numbers it read rather
than emptying the card. Those numbers are kept in `~/.ccam/usage.json`
(percentages and reset times, never a credential), so a restart in the
middle of a rate limit still has something true to show; anything older
than six hours is discarded, because by then the shortest window on the
page has rolled over. The build answering on
that port is named in the top-right corner, which is how you tell a fix
that shipped from a fix that is actually running.

## VS Code, Cursor, and the rest

The Claude Code extension never sees your shell — it starts Claude itself — so
aliases and the `claude` function do nothing for it. It does read its own
`claudeCode.environmentVariables` setting, applied over the environment of every
Claude process it starts, and that is where ccam puts one entry pointing at a
credential store of its own:

```sh
ccam editor ehti      # point every installed editor at an account
ccam editor           # say which account each one is on
```

Switching afterwards is the same write to the same kind of store as in a
terminal, so a conversation that is already open moves across when the extension
next looks, and a new one starts on that account outright. Each editor gets its
own store, so VS Code and Cursor can sit on different accounts at once.

Only the one setting is touched. Your `settings.json` keeps its comments, its
formatting, and any environment variables you set there yourself — ccam edits
that single value in place rather than reformatting the file.

## Where the credentials live

Claude Code keeps an account's login in the macOS Keychain, in the Windows
Credential Manager where one is available, and in a `.credentials.json` file
otherwise — and ccam reads whichever it finds. The stores ccam writes for
sessions and editors follow the same rule, with one addition: a keychain it
cannot write falls back to the file, which is what Claude Code itself does with
the same pair. After writing, ccam reads the store back; if the write did not
land where Claude Code will look for it, the switch is reported as failed and
the session is relaunched instead.

How quickly a switch shows up depends on which of those the session is reading,
and the difference is worth knowing: Claude Code re-reads the credentials *file*
on every request, so a switch lands on the next one, but it caches Keychain
reads for thirty seconds, so on macOS the session may answer once or twice more
as the old account before it flips. ccam says so when it switches.

Set `CCAM_CREDENTIALS_FILE=1` to keep ccam out of the Keychain entirely and use
the file store everywhere — which also makes switches immediate. Claude Code reads it when its keychain item is
absent, so nothing breaks — the trade is that a copy of the token sits in a
`0600` file for as long as that session or editor exists.

## Token usage, per account

If the machine also runs the Claude usage monitor, its agent reports every
ccam account separately: each login is its own account on the dashboard,
under the one device, rather than every account's tokens piling up in a
single number for the machine. There is nothing to configure per account
and nothing to re-run after adding one. The agent re-reads
`~/.ccam/accounts.json` on every sync — so an account you add now starts
reporting within a sync tick, and an account you delete simply stops.

Because accounts share one `~/.claude`, their transcripts pool into a single
`projects/` tree and nothing inside a transcript records which login paid for
it. The monitor's hook closes that gap from inside the session: it runs while
the session is alive, reads the account directory it inherited, and writes
`session id → owner` to its own ledger, which is what the scanner attributes
by. Sessions that predate the ledger are credited to nobody rather than to
whichever account happens to be signed in.

That is the reason `accounts.json` is treated as a contract rather than an
internal file (see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)): renaming
one of its fields still compiles and still passes ccam's own tests, but it
quietly sends another tool's numbers to the wrong account.

## Staying up to date

ccam updates itself. The running service checks the published release every
five minutes, verifies the download against the checksums published beside
it, replaces its own binary and restarts into it — then says so with a
desktop notification, on macOS, Windows and Linux alike. A fix pushed to
main is running on your machine minutes later; nothing to run, nothing to
remember.

Two rules keep that safe to leave alone:

- A download whose SHA-256 doesn't match the release's `checksums.txt` is
  discarded, not installed.
- A release published *before* the binary you are running was written is
  never installed over it — so a build you made yourself from a working
  tree that is ahead of the release is left alone.

Set `CCAM_AUTO_UPDATE=0` in the service's environment to turn it off, and
`CCAM_NOTIFY=0` to keep the notifications quiet. To update by hand at any
time, re-run the install command above.

## Uninstalling

```sh
ccam uninstall
```

Stops the service, removes the autostart registration, strips every
generated shell alias, and removes the installed binary. Your accounts'
login data under `~/.ccam/accounts` is left in place — remove `~/.ccam`
yourself for a full wipe.

## Development

```sh
go build ./...
go vet ./...
go test ./...
go test ./test/e2e/...   # full install → login → uninstall lifecycle

cd test/browser && npm install && npx playwright install chromium webkit
npx playwright test        # drives the real web UI in Chromium and WebKit
```

The browser layer is not optional cover: the Go tests drive the HTTP API
directly and never load the page, so a UI that was broken in every
browser once passed CI while failing on every real machine.

### Releasing

[`.github/workflows/pipeline.yml`](.github/workflows/pipeline.yml) does it
all: every push to `main` runs the full test + e2e suite on macOS/Linux/
Windows, and if that's green, builds all 6 targets and republishes the
rolling `latest` GitHub Release — the one `install.sh`/`install.ps1` pull
from by default. So shipping a change is just `git push`. Pushing a
`vX.Y.Z` tag instead builds the same way but creates a proper pinned
release (`CCAM_VERSION=vX.Y.Z` selects it in either install script). A
pull request runs test + e2e only, as a pre-merge gate.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how the pieces fit
together, and [docs/MANUAL_VERIFICATION.md](docs/MANUAL_VERIFICATION.md) for
the one thing CI can't cover: a real Anthropic login, once per OS, before
tagging a release.

## License

MIT — see [LICENSE](LICENSE).
