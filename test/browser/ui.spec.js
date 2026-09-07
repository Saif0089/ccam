// End-to-end tests that drive ccam's actual web UI in a real browser
// against a real ccam server, with testdata/fakeclaude standing in for
// the `claude` CLI.
const { test, expect } = require("@playwright/test");
const { spawn, execFileSync } = require("child_process");
const fs = require("fs");
const net = require("net");
const os = require("os");
const path = require("path");

const repoRoot = path.resolve(__dirname, "..", "..");
const isWindows = process.platform === "win32";
const exe = (name) => (isWindows ? `${name}.exe` : name);

let server;
let baseURL;
let workDir;

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

  server = spawn(ccamBin, ["serve", "--port", String(port)], {
    env: {
      ...process.env,
      HOME: home,
      USERPROFILE: home,
      APPDATA: path.join(home, "AppData", "Roaming"),
      LOCALAPPDATA: path.join(home, "AppData", "Local"),
      CCAM_CLAUDE_BIN: fakeClaude,
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
