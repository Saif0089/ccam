// Browser-level tests for ccam's web UI.
//
// These exist because the Go end-to-end tests drive the HTTP API
// directly and never load the page — so a UI that was completely broken
// in a real browser (the SSE payload's field names didn't match what
// app.js read, and an empty 202 body was parsed as JSON) passed CI
// while failing on every real machine. Anything that only breaks in a
// browser has to be tested in a browser.
//
// Both engines are run: Chromium and WebKit (Safari's engine), since
// the first report of the JSON bug was Safari's distinctive
// "The string did not match the expected pattern."
const { defineConfig, devices } = require("@playwright/test");

module.exports = defineConfig({
  testDir: ".",
  timeout: 90_000,
  expect: { timeout: 30_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
  ],
});
