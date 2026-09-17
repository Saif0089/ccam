# Hosting the admin panel on Vercel

The panel is one Go serverless function (`api/index.go`) that serves the admin
UI and the API every machine's client talks to. Its state lives in Postgres, not
on disk, because a serverless panel is many short-lived instances at once and the
one-machine-per-account rule is held by a database compare-and-swap that all of
them share. The client on each machine is unchanged: it checks in every thirty
seconds and obeys whatever the panel says.

Both halves update themselves on every release. The client is the ordinary ccam
binary, which already self-updates from the GitHub `latest` release. The server
redeploys itself: the CI pipeline's `deploy-panel` job runs on every push to
main, once the four settings below are in place.

## One-time setup

Everything here is on your Vercel/GitHub account — the parts ccam cannot do for
you. About ten minutes.

1. **A Postgres database.** Any Postgres works; Neon (neon.tech) has a free tier
   and pairs with Vercel in a click. Create one and copy its connection string
   (`postgres://…`). The panel creates its own table on first run.

2. **A sealing key.** On this machine:

   ```sh
   ccam panel genkey
   ```

   It prints one base64 line. This key opens the logins the panel stores; keep a
   copy somewhere safe, because losing it makes every stored login unreadable.

3. **Point the Vercel CLI at the right account** (you said it is signed in to a
   different one):

   ```sh
   vercel logout
   vercel login          # sign in as the account that should own this
   ```

4. **Link and set the environment**, from the repo root:

   ```sh
   vercel link                                    # create/select the project
   vercel env add DATABASE_URL production         # paste the Postgres string
   vercel env add CCAM_PANEL_KEY production        # paste the genkey output
   vercel deploy --prod                            # first deploy
   ```

   The URL it prints is the panel. Open it once and set the admin password.

## Make the server redeploy on every release

So a `git push` ships the client release *and* the panel together, add these to
the GitHub repo (Settings → Secrets and variables → Actions):

- Secret `VERCEL_TOKEN` — from Vercel → Account Settings → Tokens.
- Secret `VERCEL_ORG_ID` and `VERCEL_PROJECT_ID` — both are in `.vercel/project.json`
  after `vercel link` (that folder is gitignored; read the values out of it).
- Variable `VERCEL_DEPLOY` = `true` — the switch that turns the deploy job on.
  Until it is set, the job is skipped and the pipeline is unchanged.

## Enrolling machines against it

On each machine, once:

```sh
# in the panel: People → the person → Send a code, which shows the command:
ccam panel join https://your-panel.vercel.app <code>
```

From then on that machine holds whatever the panel assigns it, and lets go of
whatever it takes back, within one check-in. An account signed in on a machine is
handed to the panel with `ccam panel push <account> https://your-panel.vercel.app`.

## What this does and does not defend against

The panel binds real logins into a Postgres row, sealed with the key. A copy of
the database alone is not a working set of logins; the key is needed too, and the
key lives only in Vercel's environment and wherever you kept it. It does not
defend against someone who already controls the Vercel project or the database.
A machine that cannot reach the panel keeps what it was last told it had, so
taking an account back reaches an online machine within thirty seconds and a
sleeping one when it wakes. If a login may have been copied while someone held
it, sign that account out at Anthropic after taking it back — that is what makes
an old copy useless.

## Note on the Go version

Vercel's Go runtime must be recent enough for this code (Go 1.24+, for
`crypto/pbkdf2`). If a deploy fails on the Go version, that is why; Vercel picks
the runtime, and the fix is on their side or by pinning `GO_VERSION` in the
project's environment.
