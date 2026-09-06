# ccam — Claude Code Account Manager

Manage and use multiple [Claude Code](https://claude.com/claude-code) (`claude`
CLI) accounts on one machine, from a local web UI backed by a small
background service. No admin/root/elevation required to install, on
Windows, macOS, or Linux, any architecture.

It automates the technique from
["Setting Up Multiple Claude Code Accounts on Your Local Machine"](https://medium.com/@buwanekasumanasekara/setting-up-multiple-claude-code-accounts-on-your-local-machine-f8769a36d1b1):
each account gets its own directory used as its `CLAUDE_CONFIG_DIR`, isolating
its login completely from every other account. ccam:

- lets you add a new account by logging in right from the browser (no manual
  `CLAUDE_CONFIG_DIR=... claude` typing),
- keeps one shell alias per account (`claude-work`, `claude-personal`, ...) in
  sync across bash, zsh, fish, and PowerShell — so opening *any* terminal and
  running that alias launches `claude` scoped to that account,
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
```

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
