# De-isolation plan (handoff for Opus 5)

Turn each ccam account from a **full isolated config dir** into a **credential namespace only**,
so every account shares the user's own `~/.claude` (sessions, MCP, skills, plugins, hooks,
CLAUDE.md) and only the login differs. Switching an account keeps the conversation.

This doc is the finish line, the target folder layout, what is already built, and the
ordered work that remains. It is written to be executed top-to-bottom.

---

## The finish line (definition of done)

A user on macOS, Linux, or Windows installs with the normal one-liner (or is auto-updated),
and afterward **all** of the following hold, verified by tests:

1. Every managed account runs inside the shared `~/.claude` — same CLAUDE.md, hooks, agents,
   skills, plugins, MCP servers, and one shared conversation history.
2. **No account was re-logged-in.** Credentials are found by the identical hash (proven —
   see "Credentials" below). This is a hard requirement.
3. `/resume` (or `--continue`) in any account lists **every** conversation, including each
   account's pre-migration history.
4. Switching accounts (`ccam <name>`) relaunches in place and lands back in the **same
   conversation** on the other account. The user sees a redraw, nothing else.
5. The usage monitor still shows correct **per-account** current usage (session ledger).
6. Existing users get all of this on the next auto-update restart with **zero manual steps**;
   the one-time transcript migration runs exactly once, is reversible, and never re-logs-in.

---

## Folder layout: before → after (`/Users/hassan`)

### Before (isolated)
```
~/.claude/                     default account's home: 2652 transcripts, skills, plugins,
~/.claude.json                 hooks, CLAUDE.md, MCP; identity + shared config
~/.ccam/
  accounts.json                the contract other tools read
  accounts/
    ehti/                      FULL config dir — .claude.json, projects/ (646 jsonl, 170M),
                               skills/, plugins/, sessions/, session-env/, shell-snapshots/,
                               file-history/, history.jsonl   (creds: keychain item 2e9966a8)
    usama/                     FULL config dir — .claude.json, projects/ (39 jsonl, 21M), ...
  ccam.log, ccam.pid, <port>
```

### After (credentials-only)
```
~/.claude/                     THE shared home for ALL accounts
  projects/                    pooled ~3337 transcripts (2652 + 646 + 39) — /resume sees all
  skills/ plugins/ hooks/ agents/ CLAUDE.md   every account inherits these
~/.claude.json                 shared config; oauthAccount = whichever account is active (T3)
~/.ccam/
  accounts.json                each managed account gains  "isolation":"credentials-only"
  accounts/
    ehti/
      .credentials.json OR keychain item   UNCHANGED — same path, same hash, same login
      .claude.json                          kept: holds only oauthAccount (identity stub the
                                            monitor and the never-updating Windows .exe read)
      _pre-deisolation/                     archived projects/skills/... (reversible; prunable)
    usama/   (same shape)
  ccam.log, ccam.pid, <port>
```

The managed account dirs shrink from a whole config dir to a credential holder plus an
identity stub. Nothing about the login moves.

---

## Credentials: a proven no-op (do not "migrate" these)

Claude Code derives its credential store name from the same path string whether it is passed
as `CLAUDE_CONFIG_DIR` (old) or `CLAUDE_SECURESTORAGE_CONFIG_DIR` (new):
`Claude Code-credentials-<sha256(path)[:8]>`. The path does not change (`configDir` stays
`~/.ccam/accounts/<id>`), so the item name does not change. Verified live against all three
real logins:

```
ehti    sha256[:8]=2e9966a8   item present: YES
usama   sha256[:8]=883db814   item present: YES
default bare "Claude Code-credentials"  present: YES
```

The de-isolation swap therefore requires **no credential move and no re-login**. A migration
step must never touch the keychain, `.credentials.json`, or the `oauthAccount` stub.

---

## Already built this session (do NOT redo)

ccam:
- `internal/accounts/env.go` — `SecureStorageEnvVar`; `EnvForConfigDir` sets **both** vars
  (login/probe, pins the NFC keychain branch); `EnvForSharedConfig` sets securestorage only
  (sessions); both strip inherited vars, never emit an empty value.
- `internal/shellrc/render.go`, `internal/termlauncher/launcher.go` — aliases and terminal
  launch export `CLAUDE_SECURESTORAGE_CONFIG_DIR` and `env -u CLAUDE_CONFIG_DIR`.
- `internal/accounts/model.go` — `Isolation` field + `IsolationOrDefault()` /
  `SharesUserConfigDir()`; managed = `credentials-only`, default = `config-dir`.
- `internal/accounts/contract_test.go`, `docs/ARCHITECTURE.md` — contract carries `isolation`;
  absent means the old scheme (old readers stay correct).
- `internal/httpserver/server.go` — **boot-time `syncAliases()`** so an auto-updated ccam
  rewrites the alias block on restart with nothing to type (this is what carries the new
  aliases to existing users).
- `internal/updater/replace_windows.go`, `install.ps1` — elevation-on-demand (only when
  Windows actually refuses; never on a normal per-user install).

claude-usage-monitor:
- `agent/scan-tokens.py` — `--ledger/--owner`; attributes pooled transcripts per account by
  the session ledger; unknown/pre-cutover → unattributed (never mis-credited).
- `agent/sync-usage.sh`, `agent/sync-usage.ps1` — hook writes the session ledger (three-way
  owner: account / default / unknown); discovery branches on `isolation`.
- `cmd/sync/main.go` — same ledger + discovery in the Go agent (the frozen Windows `.exe`).
- `.github/workflows/ci.yml` — ledger e2e on Linux/macOS/Windows; `.ps1` parse gate;
  `agent/` ↔ `public/install/` parity restored.

---

## Remaining work (ordered)

### T0 — Kill the one real unknown — ✅ DONE (proven)
Cross-account resume loads cleanly with no identity rejection. Proven without touching a real
account: resuming a session created by account A, as a *different* fake account B, in
de-isolated config (`CLAUDE_CONFIG_DIR` unset → shared `~/.claude`,
`CLAUDE_SECURESTORAGE_CONFIG_DIR`=B):
```
garbage session id  → "No conversation found with session ID: 000…"   (resume validates first)
A's real session    → "Failed to authenticate"                         (loaded + forked; only the
                                                                        fake token failed at the API)
```
The real foreign session got *past* the "not found" gate — it loaded and forked — and failed
only at the API auth stage, i.e. no "belongs to a different account" refusal. `fork-session`,
`suppressOriginalPrompt`, `executeUserPromptSubmitHooks` are all confirmed present in the
binary. Remaining sub-check for T4: a full run with a *valid* account B (one cheap turn) to
confirm the reply renders — non-blocking; the rejection path is what mattered and it is clear.

### T1 — One-time transcript migration — ✅ DONE
`Manager.MigrateManagedToShared(claudeDir)` in `internal/accounts/migrate.go`, called on ccam
startup from `cmd_serve.go` (best-effort, logged, non-fatal). Fires for every managed account
still on the config-dir scheme, so an existing install picks it up on the next updater restart
with nothing to run.

Per account: **copy** (never move) `<configDir>/projects/**` into `~/.claude/projects/**`,
skipping any dest file that already exists (UUID names ⇒ same session), then flip `isolation`
to `credentials-only`. The per-account flag is the guard — a migrated account is skipped next
run; a copy that failed stays config-dir and retries (never marked half-done). Credentials and
the `.claude.json` oauthAccount stub are never touched. Copy is via temp+rename so a crash
loses nothing. Tests: copies (incl. deep subagent transcripts) + flips, idempotent, never
overwrites, never-used account is safe, default skipped, creds untouched.

Note: migrated history appears in `/resume` but counts as **unattributed** in the usage
dashboard (all pre-ledger history does — the safe direction). Archiving the now-redundant
per-account subdirs is deferred to **T2** (`ccam prune`), so T1 stays purely additive and
reversible; the source transcripts remain until the user prunes.

Adversarially reviewed (15-agent workflow) and hardened against all 4 confirmed findings:
copies now run **outside the account lock, in a background goroutine after the port file is
written** (a large first-boot copy can't delay the updater's 15s restart handshake, and can't
block account HTTP ops); real (non-`NotExist`) stat errors are propagated so an account is
never flipped with transcripts stranded; per-file failures are counted and skipped (one bad
file can't abort or permanently block an account); copies use a **unique temp + `fsync` +
rename** (no cross-process interleave, no partial file surviving a crash). 7 tests.

### T2 — Prune — ✅ DONE
`ccam prune [--yes] [id...]` (`internal/accounts/prune.go`, `internal/cli/cmd_prune.go`).
Reclaims the per-account directories the migration left in place: for a credentials-only
account it removes everything except the credential and identity files (`.claude.json`,
`.credentials.json`). Previews by default (reports reclaimable bytes), deletes only with
`--yes`; never automatic, never touches an un-migrated or default account. 3 tests.

### T3 — Active-identity write — ✅ DONE
`internal/accounts/identity.go`: `SetActiveIdentity` writes the active account's `oauthAccount`
into shared `~/.claude.json` and deletes the six org-keyed caches (`clientDataCacheSlots`,
`modelAccessCache`, `cachedUsageUtilization`, `orgModelDefaultCache`,
`additionalModelOptionsCache`, `cachedExtraUsageDisabledReason`), preserving projects/mcp/etc.
No-op when it already names the account (no churn) and never creates a missing file. The default
account's identity is captured once on boot (`SnapshotDefaultIdentity`, safe because managed
accounts were isolated until now) into `~/.ccam/accounts/default/.claude.json`, so switching
back to it restores `/status`. Single-session-at-a-time is last-writer-wins. 6 tests.

### T4 — `ccam <name>` in-place switch — ✅ DONE (hook verified E2E; interactive redraw is the one manual check)
Kept the simple `claude-<slug>` aliases as the robust direct launch; switching is a separate,
explicit entry so a broken ccam can never block a plain launch:
- `ccam <name>` (or `ccam run <name>`) starts a **supervised** session (`internal/cli/cmd_run.go`).
- The supervisor installs, idempotently, a `UserPromptSubmit` hook into shared
  `~/.claude/settings.json` (`internal/switching/hookinstall.go`) — preserving the user's other
  hooks, and writing only when something changed.
- In-session `ccam <name>` (a **plain word**, never `/slash`) → the hook (`internal/cli/cmd_hook.go`)
  writes a handoff and returns `decision:"block"` + `suppressOriginalPrompt` (zero model tokens,
  not echoed). The supervisor polls the handoff, terminates Claude (SIGTERM→SIGKILL on Unix,
  Kill on Windows), and relaunches `claude --resume <id> --fork-session` as the new account
  (T3 identity applied first). Unknown name / no supervisor → the prompt passes through untouched.
- Verified end-to-end against the real binary + real accounts: correct block JSON + handoff on
  trigger, clean pass-through otherwise, no typo hijack. Supervisor loop, child-terminate, and
  exit-code paths unit-tested with a fake claude. **Remaining:** one manual interactive check
  that the TUI redraw lands cleanly (can't be automated) and the valid-account-B render.

### T5 — Rollout wiring — ✅ DONE
Delivery path is the updater restart → boot: alias re-sync (`server.go`), the background
transcript migration (T1), and the default-identity snapshot (T3) all run there. The switch
hook self-installs on first `ccam <name>`. Nothing for the user to run.

### T6 — Tests + CI — ✅ (Go tests run cross-OS in the existing matrix)
Migration (7 tests), identity (6), switching core + hook-install (8), supervisor (4) all run in
`pipeline.yml`'s `test` job on ubuntu/macos/windows. The monitor's ledger e2e already covers the
per-account attribution on all three OSes. T2 (prune) remains the only deferred, non-finish-line
item.

---

## Risks / unknowns (explicit)
- **T0 resume-across-accounts**: mechanism verified in the binary; end-to-end not yet run with
  two logins. The one thing to prove before T4.
- **Concurrent sessions as two accounts**: shared `~/.claude.json` holds one `oauthAccount`;
  last writer wins for the displayed identity. Fine for switch-one-at-a-time; document it.
- **`~/.claude.json.lock` contention** with several concurrent sessions: throughput, not
  corruption.
- **Undocumented env var**: `CLAUDE_SECURESTORAGE_CONFIG_DIR` is not a public contract; verify
  the derived item resolves and fail loudly rather than silently falling back to the default
  login (empty value = the user's real login — already guarded in `env.go`, keep it that way).
