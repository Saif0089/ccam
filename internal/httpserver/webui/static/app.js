// ccam web UI — plain JS, no build step, no framework: this is a small
// enough surface that a bundler would cost more than it saves.
"use strict";

const accountsList = document.getElementById("accounts-list");
const emptyState = document.getElementById("empty-state");
const rowTemplate = document.getElementById("account-row-template");
const meterTemplate = document.getElementById("meter-template");
const refreshedLabel = document.getElementById("refreshed");

const addDialog = document.getElementById("add-dialog");
const addForm = document.getElementById("add-form");
const addNameInput = document.getElementById("add-name");

const loginDialog = document.getElementById("login-dialog");
const loginStatus = document.getElementById("login-status");
const loginUrlBox = document.getElementById("login-url-box");
const loginUrlLink = document.getElementById("login-url");
const loginCodeForm = document.getElementById("login-code-form");
const loginCodeInput = document.getElementById("login-code");

const renameDialog = document.getElementById("rename-dialog");
const renameForm = document.getElementById("rename-form");
const renameIdInput = document.getElementById("rename-id");
const renameNameInput = document.getElementById("rename-name");

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      if (body && body.error) message = body.error;
    } catch (_) {}
    throw new Error(message);
  }
  // Not every success carries a body: 202 (login accepted) and 204
  // (cancel/remove/launch) are both empty. Parsing "" as JSON throws a
  // confusing browser-specific error ("The string did not match the
  // expected pattern." on Safari), so decide by what actually arrived
  // rather than by status code.
  const text = await res.text();
  if (!text) return null;
  return JSON.parse(text);
}

async function loadAccounts(forceUsage) {
  const data = await api("/api/accounts");
  renderAccounts(data.accounts || [], forceUsage);
}

// --- time formatting --------------------------------------------------
//
// Every number on this page is a duration, and durations are what people
// misread first. Two units, never three: "1d 6h", never "1d 6h 12m".

function formatLeft(ms) {
  if (!(ms > 0)) return "now";
  const minutes = Math.floor(ms / 60000);
  if (minutes < 1) return "under a minute";
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  const mins = minutes % 60;
  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  if (hours > 0) return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
  return `${mins}m`;
}

// formatWhen answers "when exactly?" — the part a countdown alone can't
// tell you when you're planning tomorrow morning's work.
function formatWhen(date) {
  const time = date.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const dayIndex = Math.floor((date - midnight) / 86400000);
  if (dayIndex === 0) return `today ${time}`;
  if (dayIndex === 1) return `tomorrow ${time}`;
  if (dayIndex > 1 && dayIndex < 7) {
    return `${date.toLocaleDateString([], { weekday: "long" })} ${time}`;
  }
  return date.toLocaleDateString([], { month: "short", day: "numeric" });
}

// Countdowns tick in the browser from the absolute timestamps the server
// sent, so the page stays honest while it sits open without polling.
const countdowns = [];

function countdown(el, isoTime, render) {
  const at = new Date(isoTime);
  if (isNaN(at)) return false;
  const entry = { el, at, render };
  countdowns.push(entry);
  tickOne(entry);
  return true;
}

function tickOne(entry) {
  entry.el.textContent = entry.render(entry.at - Date.now(), entry.at);
}

setInterval(() => {
  for (let i = countdowns.length - 1; i >= 0; i--) {
    // Cards are replaced wholesale on every refresh; their countdowns
    // leave with them.
    if (!countdowns[i].el.isConnected) countdowns.splice(i, 1);
    else tickOne(countdowns[i]);
  }
}, 1000);

function planLabel(plan) {
  if (!plan) return "";
  const known = { max: "Max", pro: "Pro", team: "Team", enterprise: "Enterprise", free: "Free" };
  const key = plan.toLowerCase();
  return (known[key] || plan.charAt(0).toUpperCase() + plan.slice(1)) + " plan";
}

// levelFor decides the one thing colour is allowed to say here.
function levelFor(percent, severity) {
  if (severity === "critical" || severity === "error" || percent >= 90) return "level-crit";
  if (severity === "warning" || severity === "warn" || percent >= 70) return "level-warn";
  return "level-ok";
}

// refreshAccounts is loadAccounts for the places that can't await it
// and must never raise an unhandled rejection.
function refreshAccounts() {
  loadAccounts().catch((err) => {
    loginStatus.textContent = "Could not refresh accounts: " + err.message;
  });
}

function renderAccounts(accounts, forceUsage) {
  accountsList.innerHTML = "";
  emptyState.hidden = accounts.length > 0;

  for (const account of accounts) {
    const node = rowTemplate.content.cloneNode(true);
    const card = node.querySelector(".account-card");
    card.dataset.id = account.id;

    node.querySelector(".account-name").textContent = account.name;

    const isDefault = account.kind === "default";
    if (isDefault) card.classList.add("default-account");

    // The stored status is what ccam last saw; the usage request that
    // follows replaces it with what is true right now.
    const badge = node.querySelector(".status-badge");
    setStatus(badge, account.status);

    node.querySelector(".alias-text").textContent = account.alias;
    if (isDefault) {
      // There is no generated alias for this one: typing `claude` is
      // how you run it, which is also why nothing was written to any
      // shell rc file for it.
      node.querySelector(".alias-text").title = "This is the account plain `claude` already uses";
    }

    const connectBtn = node.querySelector(".connect-btn");
    // Always offer Connect: a linked account's token can expire or be
    // revoked, and re-adding the account (deleting its config dir) is
    // not an acceptable way to recover from that.
    connectBtn.textContent = account.status === "linked" ? "Reconnect" : "Connect";

    const copyBtn = node.querySelector(".copy-alias");
    copyBtn.addEventListener("click", () => copyToClipboard(account.alias, copyBtn));

    connectBtn.addEventListener("click", () => startLogin(account));
    node.querySelector(".launch-terminal").addEventListener("click", async () => {
      try {
        await api(`/api/accounts/${account.id}/launch-terminal`, { method: "POST" });
      } catch (err) {
        alert("Could not open a terminal: " + err.message);
      }
    });
    node.querySelector(".rename-btn").addEventListener("click", () => {
      renameIdInput.value = account.id;
      renameNameInput.value = account.name;
      renameDialog.showModal();
    });
    const removeBtn = node.querySelector(".remove-btn");
    // ccam did not create the default account's directory and must
    // never delete it, so the wording says what actually happens.
    removeBtn.textContent = isDefault ? "Forget" : "Remove";
    removeBtn.addEventListener("click", () => removeAccount(account, isDefault));

    accountsList.appendChild(node);
    loadUsage(account, card, forceUsage);
  }
}

// --- usage ------------------------------------------------------------

const STATUS_TEXT = {
  linked: "linked",
  pending: "not connected",
  expired: "login expired",
  "signed-out": "signed out",
  unknown: "unknown",
};

function setStatus(badge, status) {
  badge.className = "status-badge " + status;
  badge.textContent = STATUS_TEXT[status] || status;
}

async function loadUsage(account, card, force) {
  const path = `/api/accounts/${account.id}/usage` + (force ? "?refresh=1" : "");
  let snapshot;
  try {
    snapshot = await api(path);
  } catch (err) {
    snapshot = { error: "Could not read usage: " + err.message };
  }
  // The list is re-rendered wholesale, so a slow response can arrive
  // after its card is gone. Dropping it here is what keeps a stale
  // account's numbers from reappearing under a live one.
  if (!card.isConnected) return;
  renderUsage(card, account, snapshot);
  refreshedLabel.textContent = "updated " + new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function renderUsage(card, account, snapshot) {
  const meters = card.querySelector(".meters");
  const note = card.querySelector(".usage-note");
  const session = card.querySelector(".session");
  meters.innerHTML = "";
  session.innerHTML = "";

  const state = liveStatus(account, snapshot);
  setStatus(card.querySelector(".status-badge"), state);

  const plan = snapshot.session && snapshot.session.plan;
  card.querySelector(".plan-chip").textContent = planLabel(plan);

  const limits = (snapshot.usage && snapshot.usage.limits) || [];
  for (const limit of limits) {
    meters.appendChild(buildMeter(limit));
  }

  note.hidden = !snapshot.error;
  if (snapshot.error) note.textContent = snapshot.error;

  buildSession(session, snapshot.session, state);
}

// liveStatus is the one word that says whether this account will work if
// you run it right now. The server decides it; the page only falls back
// to the stored status when the request itself failed.
function liveStatus(account, snapshot) {
  return snapshot.state || (account.status === "linked" ? "unknown" : account.status);
}

function buildMeter(limit) {
  const node = meterTemplate.content.cloneNode(true);
  const meter = node.querySelector(".meter");
  const percent = Math.max(0, Math.min(100, limit.percent || 0));

  meter.classList.add(levelFor(percent, limit.severity));
  node.querySelector(".meter-label").textContent = limit.label;
  node.querySelector(".meter-pct").textContent = Math.round(percent) + "% used";

  const bar = node.querySelector(".bar");
  bar.setAttribute("aria-valuenow", Math.round(percent));
  bar.setAttribute("aria-label", limit.label);
  // Painted after the frame so the width transitions in from zero:
  // motion here reports that a number arrived, which is the one thing
  // on this page worth animating.
  requestAnimationFrame(() => {
    meter.querySelector(".bar-fill").style.width = percent + "%";
  });

  const reset = node.querySelector(".meter-reset");
  if (limit.resetsAt) {
    countdown(reset, limit.resetsAt, (ms, at) =>
      ms > 0 ? `resets in ${formatLeft(ms)} (${formatWhen(at)})` : "resetting now"
    );
  } else {
    reset.remove();
  }
  return node;
}

// buildSession answers "how long am I signed in for?" — two clocks that
// are easy to confuse, so each is named for what it actually does.
function buildSession(container, info, state) {
  if (!info) return;

  // A login that is over has no clocks left to run. Showing them ticking
  // would contradict the note right above. Only the date that has
  // actually passed may be called "ended": a rejected login whose
  // window is still open would otherwise be reported as ending on a day
  // that has not happened yet.
  if (state === "expired") {
    const end = new Date(info.sessionExpiresAt);
    if (isNaN(end)) return;
    const over = end.getTime() <= Date.now();
    sessionItem(container, "Login session", "").textContent =
      (over ? "ended " : "until ") + fullDate(end);
    return;
  }

  if (info.sessionExpiresAt) {
    const end = new Date(info.sessionExpiresAt);
    const item = sessionItem(container, "Login session", isNaN(end) ? "" : "until " + fullDate(end));
    countdown(item, info.sessionExpiresAt, (ms) => `${formatLeft(ms)} left`);
  }
  if (info.accessExpiresAt) {
    // Named for what it does, because this is the short clock that makes
    // people think they are about to be signed out. They aren't: Claude
    // Code renews this one on its own.
    const item = sessionItem(container, "Access token", "renews on its own");
    countdown(item, info.accessExpiresAt, (ms) => (ms > 0 ? `${formatLeft(ms)} left` : "renewing"));
  }
}

function sessionItem(container, label, detail) {
  const wrap = document.createElement("span");
  wrap.className = "session-item";
  const name = document.createElement("span");
  name.className = "detail";
  name.textContent = label + " ";
  const value = document.createElement("b");
  wrap.append(name, value);
  if (detail) {
    const tail = document.createElement("span");
    tail.className = "detail";
    tail.textContent = " " + detail;
    wrap.append(tail);
  }
  container.appendChild(wrap);
  return value;
}

function fullDate(date) {
  return date.toLocaleDateString([], { month: "short", day: "numeric" });
}

async function copyToClipboard(text, button) {
  const original = button.textContent;
  try {
    await navigator.clipboard.writeText(text);
    button.textContent = "Copied";
  } catch (_) {
    button.textContent = "Copy failed";
  }
  setTimeout(() => {
    button.textContent = original;
  }, 1500);
}

async function removeAccount(account, isDefault) {
  const question = isDefault
    ? `Stop showing "${account.name}" here? Your ~/.claude login is left completely untouched — ccam just forgets about it.`
    : `Remove "${account.name}"? This deletes its local login state.`;
  if (!confirm(question)) return;
  try {
    await api(`/api/accounts/${account.id}`, { method: "DELETE" });
  } catch (err) {
    alert("Could not remove account: " + err.message);
  }
  // Refresh either way: some failures (e.g. alias sync) happen after
  // the account itself was already removed, and leaving the old list
  // on screen would contradict what the server now holds.
  refreshAccounts();
}

document.getElementById("add-account-btn").addEventListener("click", () => {
  addNameInput.value = "";
  addDialog.showModal();
});
document.getElementById("add-cancel").addEventListener("click", () => addDialog.close());

document.getElementById("refresh-btn").addEventListener("click", async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  refreshedLabel.textContent = "updating…";
  try {
    await loadAccounts(true);
  } catch (err) {
    refreshedLabel.textContent = "could not refresh: " + err.message;
  } finally {
    btn.disabled = false;
  }
});

addForm.addEventListener("submit", async (e) => {
  // Not method="dialog": the dialog must stay open until the request
  // resolves, or a failure alerts over a dismissed dialog and throws
  // away what the user typed.
  e.preventDefault();
  const name = addNameInput.value.trim();
  if (!name) {
    alert("Name cannot be empty.");
    return;
  }
  let account;
  try {
    account = await api("/api/accounts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
  } catch (err) {
    refreshAccounts();
    alert("Could not create account: " + err.message);
    return;
  }
  addDialog.close();
  refreshAccounts();
  startLogin(account);
});

document.getElementById("rename-cancel").addEventListener("click", () => renameDialog.close());
renameForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const id = renameIdInput.value;
  const name = renameNameInput.value.trim();
  if (!name) {
    alert("Name cannot be empty.");
    return;
  }
  try {
    await api(`/api/accounts/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
  } catch (err) {
    refreshAccounts();
    alert("Could not rename account: " + err.message);
    return;
  }
  renameDialog.close();
  refreshAccounts();
});

// --- login flow -------------------------------------------------------

let activeEventSource = null;
let activeLoginAccountId = null;
let loginFinished = false;

function startLogin(account) {
  activeLoginAccountId = account.id;
  loginFinished = false;
  loginStatus.textContent = "Starting login…";
  loginUrlBox.hidden = true;
  loginCodeForm.hidden = true;
  loginCodeInput.value = "";
  if (!loginDialog.open) loginDialog.showModal();

  api(`/api/accounts/${account.id}/login`, { method: "POST" })
    .then(() => openLoginStream(account.id))
    .catch((err) => {
      loginStatus.textContent = "Could not start login: " + err.message;
    });
}

function openLoginStream(accountId) {
  // The POST that precedes this is async, so a slow start for account A
  // can resolve after the user already closed that dialog and started
  // account B. Without this guard A's stream would replace B's, and the
  // dialog — showing B — would report A's cancelled session as a lost
  // connection while B's URL never appeared.
  if (accountId !== activeLoginAccountId) return;
  if (activeEventSource) activeEventSource.close();
  const es = new EventSource(`/api/accounts/${accountId}/login/events`);
  activeEventSource = es;

  es.addEventListener("message", (e) => {
    let event;
    try {
      event = JSON.parse(e.data);
    } catch (_) {
      return;
    }
    handleLoginEvent(event);
  });

  es.addEventListener("error", () => {
    // EventSource retries transient drops on its own; only a CLOSED
    // stream is final. Staying silent here is what used to leave the
    // dialog spinning forever when the server went away.
    if (es.readyState === EventSource.CLOSED && !loginFinished) {
      loginStatus.textContent = "Lost the connection to ccam. Close this and try again.";
    }
  });
}

function handleLoginEvent(event) {
  switch (event.type) {
    case "url":
      loginStatus.textContent = "Open this URL to finish signing in:";
      loginUrlBox.hidden = false;
      loginCodeForm.hidden = false;
      loginUrlLink.href = event.url;
      loginUrlLink.textContent = event.url;
      break;
    case "linked":
      finishLogin("Connected. This account is ready to use.");
      break;
    case "timeout":
      finishLogin("Timed out waiting for the login to finish. You can try again.");
      break;
    case "failed":
      finishLogin("Login failed: " + (event.message || "unknown error"));
      break;
  }
}

function finishLogin(message) {
  loginFinished = true;
  loginStatus.textContent = message;
  loginUrlBox.hidden = true;
  loginCodeForm.hidden = true;
  if (activeEventSource) activeEventSource.close();
  refreshAccounts();
}

loginCodeForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const code = loginCodeInput.value.trim();
  if (!code || !activeLoginAccountId) return;
  try {
    await api(`/api/accounts/${activeLoginAccountId}/login/code`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code }),
    });
    loginStatus.textContent = "Code submitted, finishing sign-in…";
    loginCodeInput.value = "";
  } catch (err) {
    loginStatus.textContent = "Could not submit that code: " + err.message;
  }
});

document.getElementById("login-copy").addEventListener("click", (e) => {
  if (loginUrlLink.href) copyToClipboard(loginUrlLink.href, e.currentTarget);
});
document.getElementById("login-close").addEventListener("click", () => loginDialog.close());

// Closing the dialog — by button, Escape, or anything else — must also
// end the login on the server. Otherwise an abandoned attempt leaves a
// `claude` process running until it times out.
loginDialog.addEventListener("close", () => {
  if (activeEventSource) {
    activeEventSource.close();
    activeEventSource = null;
  }
  if (activeLoginAccountId && !loginFinished) {
    cancelLogin(activeLoginAccountId);
  }
  activeLoginAccountId = null;
});

// Closing a tab (or a headless browser at the end of a test) can tear
// the page down before a plain fetch is flushed, leaving the `claude`
// process running until its timeout. sendBeacon exists precisely for
// this: the browser guarantees delivery after the page is gone.
function cancelLogin(accountId) {
  const path = `/api/accounts/${accountId}/login/cancel`;
  if (navigator.sendBeacon && navigator.sendBeacon(path, new Blob([], { type: "text/plain" }))) {
    return;
  }
  api(path, { method: "POST", keepalive: true }).catch(() => {});
}

// Also cancel when the whole page goes away, not just the dialog.
window.addEventListener("pagehide", () => {
  if (activeLoginAccountId && !loginFinished) cancelLogin(activeLoginAccountId);
});

loadAccounts().catch((err) => {
  accountsList.textContent = "Could not load accounts: " + err.message;
});
