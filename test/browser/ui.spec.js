// End-to-end tests that drive ccam's actual web UI in a real browser
// against a real ccam server, with testdata/fakeclaude standing in for
// the `claude` CLI.
const { test, expect } = require("@playwright/test");
const { spawn, execFileSync } = require("child_process");
const fs = require("fs");
const http = require("http");
const net = require("net");
const os = require("os");
const path = require("path");

const repoRoot = path.resolve(__dirname, "..", "..");
const isWindows = process.platform === "win32";
const exe = (name) => (isWindows ? `${name}.exe` : name);

let server;
let baseURL;
let workDir;
let usageServer;

// A stand-in for Anthropic's usage endpoint. Reset times are relative so
// the countdowns in the page are always in the future, whenever the
// suite happens to run.
function usagePayload() {
  const inHours = (h) => new Date(Date.now() + h * 3600_000).toISOString();
  return JSON.stringify({
    limits: [
      { kind: "session", group: "session", percent: 12, severity: "normal", resets_at: inHours(3), is_active: true },
      { kind: "weekly_all", group: "weekly", percent: 94, severity: "normal", resets_at: inHours(50), is_active: true },
      { kind: "weekly_scoped", group: "weekly", percent: 71, severity: "normal", resets_at: inHours(50), is_active: true, scope: { model: { display_name: "Fable" } } },
    ],
    extra_usage: { is_enabled: false },
  });
}

// The one token this stub refuses, so a test can put an account in the
// "the API rejected this login" state without expiring anything.
const REJECTED_TOKEN = "revoked-access-token";

function startUsageStub() {
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      if ((req.headers.authorization || "").includes(REJECTED_TOKEN)) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end("{}");
        return;
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(usagePayload());
    });
    srv.listen(0, "127.0.0.1", () => resolve(srv));
  });
}

// writeCredentials plants a login for an account directly, which is how
// a test picks the two clocks: the short access token and the long
// refresh one that decides whether the login is over.
function writeCredentials(configDir, { accessToken, accessInHours, refreshInDays }) {
  fs.writeFileSync(
    path.join(configDir, ".credentials.json"),
    JSON.stringify({
      claudeAiOauth: {
        accessToken,
        refreshToken: "fake-refresh-token",
        expiresAt: Date.now() + accessInHours * 3600_000,
        refreshTokenExpiresAt: Date.now() + refreshInDays * 24 * 3600_000,
        subscriptionType: "max",
        rateLimitTier: "default_claude_max_20x",
      },
    })
  );
}

async function createAccount(name) {
  const res = await fetch(`${baseURL}/api/accounts`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: baseURL },
    body: JSON.stringify({ name }),
  });
  expect(res.ok).toBeTruthy();
  return res.json();
}

async function deleteAccount(id) {
  const res = await fetch(`${baseURL}/api/accounts/${id}`, {
    method: "DELETE",
    headers: { Origin: baseURL },
  });
  expect(res.ok).toBeTruthy();
}

function build(pkg, outPath) {
  execFileSync("go", ["build", "-o", outPath, pkg], { cwd: repoRoot, stdio: "inherit" });
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

async function waitForServer(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${url}/api/status`);
      if (res.ok) return;
    } catch (_) {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`ccam did not start within ${timeoutMs}ms`);
}

test.beforeAll(async () => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "ccam-browser-"));
  const home = path.join(workDir, "home");
  fs.mkdirSync(home);

  const ccamBin = path.join(workDir, exe("ccam"));
  const fakeClaude = path.join(workDir, exe("claude"));
  build("./cmd/ccam", ccamBin);
  build("./testdata/fakeclaude", fakeClaude);

  const port = await freePort();
  baseURL = `http://127.0.0.1:${port}`;

  usageServer = await startUsageStub();
  const usageURL = `http://127.0.0.1:${usageServer.address().port}/usage`;

  server = spawn(ccamBin, ["serve", "--port", String(port)], {
    env: {
      ...process.env,
      HOME: home,
      USERPROFILE: home,
      APPDATA: path.join(home, "AppData", "Roaming"),
      LOCALAPPDATA: path.join(home, "AppData", "Local"),
      CCAM_CLAUDE_BIN: fakeClaude,
      // Never let the suite call Anthropic for real.
      CCAM_USAGE_ENDPOINT: usageURL,
      // Keep the "here is your URL" state on screen long enough to be
      // asserted on; the fake otherwise finishes in ~300ms and the UI
      // races straight past it to "Connected".
      FAKECLAUDE_LOGIN_DELAY_MS: "2500",
    },
    stdio: "inherit",
  });

  await waitForServer(baseURL, 30_000);
});

test.afterAll(async () => {
  if (server) server.kill();
  if (usageServer) usageServer.close();
});

// Fail any test that logs a page error or a console error: the bug that
// prompted these tests surfaced first as a thrown SyntaxError.
test.beforeEach(async ({ page }) => {
  page.on("pageerror", (err) => {
    throw new Error(`uncaught page error: ${err.message}`);
  });
  page.on("console", (msg) => {
    if (msg.type() === "error") {
      throw new Error(`console error: ${msg.text()}`);
    }
  });
});

test("adds an account, shows the full OAuth URL, and links it", async ({ page }) => {
  await page.goto(baseURL);

  await page.click("#add-account-btn");
  await page.fill("#add-name", "Work");
  await page.click("#add-form button[type=submit]");

  // The regression this whole file exists for: the login dialog used to
  // show "Could not start login: The string did not match the expected
  // pattern." here, and later never rendered the URL at all because the
  // SSE payload's field names didn't match what app.js reads.
  await expect(page.locator("#login-status")).toContainText("Open this URL");

  const url = await page.locator("#login-url").textContent();
  expect(url).toMatch(/^https:\/\//);
  // A URL truncated at the pseudo-terminal's width is a link that
  // simply fails; the real one is ~600 characters.
  expect(url.length).toBeGreaterThan(400);
  expect(url).not.toContain(" ");

  await expect(page.locator("#login-status")).toContainText("Connected");

  await page.click("#login-close");
  await expect(page.locator(".status-badge")).toHaveText("linked");
  await expect(page.locator(".alias-text")).toHaveText("claude-work");
});

test("shows plan usage, reset countdowns and how long the login lasts", async ({ page }) => {
  await page.goto(baseURL);

  const meters = page.locator(".meter");
  await expect(meters).toHaveCount(3);

  await expect(meters.nth(0).locator(".meter-label")).toHaveText("Current session");
  await expect(meters.nth(1).locator(".meter-label")).toHaveText("This week, all models");
  await expect(meters.nth(2).locator(".meter-label")).toHaveText("This week, Fable");

  await expect(meters.nth(0).locator(".meter-pct")).toHaveText("12% used");
  await expect(meters.nth(1).locator(".meter-pct")).toHaveText("94% used");
  await expect(meters.nth(2).locator(".meter-pct")).toHaveText("71% used");

  // Colour carries one meaning on this page: how much headroom is left.
  // If these classes stop tracking the numbers, a nearly-exhausted week
  // renders in the same calm green as an untouched one.
  await expect(meters.nth(0)).toHaveClass(/level-ok/);
  await expect(meters.nth(1)).toHaveClass(/level-crit/);
  await expect(meters.nth(2)).toHaveClass(/level-warn/);

  // The bar has to actually move; a fill left at zero width would look
  // identical for every account.
  const width = await meters.nth(1).locator(".bar-fill").evaluate((el) => el.getBoundingClientRect().width);
  expect(width).toBeGreaterThan(0);

  await expect(meters.nth(0).locator(".meter-reset")).toHaveText(/resets in \d+h \d+m \(.+\)/);
  await expect(meters.nth(1).locator(".meter-reset")).toHaveText(/resets in 2d \d+h \(.+\)/);

  // The two clocks people confuse: the login itself, and the short-lived
  // access token that renews behind their back.
  const session = page.locator(".session");
  await expect(session).toContainText("Login session");
  await expect(session).toContainText(/2\dd \d+h left/);
  await expect(session).toContainText("Access token");
  await expect(session).toContainText("renews on its own");

  await expect(page.locator(".plan-chip")).toHaveText("Max plan");
  await expect(page.locator(".status-badge")).toHaveText("linked");
});

// An account ccam knows about but has no login for must say so, rather
// than keep showing the last status it saw.
test("reports a signed-out account instead of claiming it is linked", async ({ page }) => {
  const created = await fetch(`${baseURL}/api/accounts`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: baseURL },
    body: JSON.stringify({ name: "Never Connected" }),
  });
  expect(created.ok).toBeTruthy();

  await page.goto(baseURL);
  const card = page.locator(".account-card", { hasText: "Never Connected" });
  await expect(card.locator(".status-badge")).toHaveText("signed out");
  await expect(card.locator(".meter")).toHaveCount(0);
  await expect(card.locator(".usage-note")).toBeVisible();

  const removed = await fetch(`${baseURL}/api/accounts/${(await created.json()).id}`, {
    method: "DELETE",
    headers: { Origin: baseURL },
  });
  expect(removed.ok).toBeTruthy();
});

// The bug: the account being used at that very moment showed the red
// "login expired" badge. Claude Code only refreshes the short access
// token when it runs, so between runs ccam held a stale one, the API
// answered 401, and a working login was reported as rejected.
test("calls an account with a stale access token linked, not expired", async ({ page }) => {
  const account = await createAccount("Stale Token");
  // The stale token is one the API refuses, exactly as a real expired
  // one is: the fix is that ccam never sends it in the first place.
  writeCredentials(account.configDir, { accessToken: REJECTED_TOKEN, accessInHours: -1, refreshInDays: 27 });

  await page.goto(baseURL);
  const card = page.locator(".account-card", { hasText: "Stale Token" });
  await expect(card.locator(".status-badge")).toHaveText("linked");
  await expect(card.locator(".meter")).toHaveCount(0);
  await expect(card.locator(".usage-note")).toContainText("refreshes");
  // The login clock is the one still running, and it must still run.
  await expect(card.locator(".session")).toContainText(/2\dd \d+h left/);
  await expect(card.locator(".session")).not.toContainText("ended");

  await deleteAccount(account.id);
});

// A login the API really does reject is expired — but its refresh
// window can still have weeks left, and the card used to announce that
// future date as the day the session "ended".
test("never dates a rejected login as having ended in the future", async ({ page }) => {
  const account = await createAccount("Revoked Login");
  writeCredentials(account.configDir, { accessToken: REJECTED_TOKEN, accessInHours: 8, refreshInDays: 27 });

  await page.goto(baseURL);
  const card = page.locator(".account-card", { hasText: "Revoked Login" });
  await expect(card.locator(".status-badge")).toHaveText("login expired");
  await expect(card.locator(".usage-note")).toContainText("rejected");
  await expect(card.locator(".session")).toContainText("Login session");
  await expect(card.locator(".session")).not.toContainText("ended");

  await deleteAccount(account.id);
});

// Closing the dialog mid-login must leave the UI able to start another
// one. The state machine behind that (activeLoginAccountId /
// loginFinished / the EventSource) is easy to leave out of sync, and the
// symptom is a dialog that opens showing a stale error and never renders
// the new URL.
test("can start another login after closing one mid-flight", async ({ page }) => {
  await page.goto(baseURL);

  await page.click(".connect-btn");
  await expect(page.locator("#login-status")).toContainText("Open this URL");
  await page.click("#login-close");
  // Give the cancel time to actually leave the browser before teardown.
  await page.waitForTimeout(1000);
  await expect(page.locator("#login-dialog")).not.toBeVisible();

  await page.click(".connect-btn");
  await expect(page.locator("#login-status")).toContainText("Open this URL");
  const url = await page.locator("#login-url").textContent();
  expect(url.length).toBeGreaterThan(400);
  await page.click("#login-close");
  await page.waitForTimeout(1000);
});

test("renames an account and updates its alias", async ({ page }) => {
  await page.goto(baseURL);

  await page.click(".rename-btn");
  await page.fill("#rename-name", "Side Project");
  await page.click("#rename-form button[type=submit]");

  await expect(page.locator(".account-name")).toHaveText("Side Project");
  await expect(page.locator(".alias-text")).toHaveText("claude-side-project");
});

test("rejects a whitespace-only name instead of silently doing nothing", async ({ page }) => {
  await page.goto(baseURL);

  let alerted = "";
  page.on("dialog", async (dialog) => {
    alerted = dialog.message();
    await dialog.dismiss();
  });

  await page.click("#add-account-btn");
  await page.fill("#add-name", "   ");
  await page.click("#add-form button[type=submit]");

  // Either the browser blocks it via the pattern, or our own check
  // alerts — what must NOT happen is the dialog closing with nothing
  // created and no explanation.
  await expect(page.locator("#add-dialog")).toBeVisible();
  if (alerted) expect(alerted).toContain("empty");
});

test("removes an account and returns to the empty state", async ({ page }) => {
  await page.goto(baseURL);

  page.on("dialog", (dialog) => dialog.accept());
  await page.click(".remove-btn");

  await expect(page.locator("#empty-state")).toBeVisible();
  await expect(page.locator(".account-card")).toHaveCount(0);
});
