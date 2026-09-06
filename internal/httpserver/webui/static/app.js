// ccam web UI — plain JS, no build step, no framework: this is a small
// enough surface that a bundler would cost more than it saves.
"use strict";

const accountsList = document.getElementById("accounts-list");
const emptyState = document.getElementById("empty-state");
const rowTemplate = document.getElementById("account-row-template");

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

async function loadAccounts() {
  const data = await api("/api/accounts");
  renderAccounts(data.accounts || []);
}

// refreshAccounts is loadAccounts for the places that can't await it
// and must never raise an unhandled rejection.
function refreshAccounts() {
  loadAccounts().catch((err) => {
    loginStatus.textContent = "Could not refresh accounts: " + err.message;
  });
}

function renderAccounts(accounts) {
  accountsList.innerHTML = "";
  emptyState.hidden = accounts.length > 0;

  for (const account of accounts) {
    const node = rowTemplate.content.cloneNode(true);
    const card = node.querySelector(".account-card");
    card.dataset.id = account.id;

    node.querySelector(".account-name").textContent = account.name;

    const badge = node.querySelector(".status-badge");
    badge.textContent = account.status;
    badge.classList.add(account.status === "linked" ? "linked" : "pending");

    node.querySelector(".alias-text").textContent = account.alias;

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
    node.querySelector(".remove-btn").addEventListener("click", () => removeAccount(account));

    accountsList.appendChild(node);
  }
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

async function removeAccount(account) {
  if (!confirm(`Remove "${account.name}"? This deletes its local login state.`)) return;
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
    api(`/api/accounts/${activeLoginAccountId}/login/cancel`, { method: "POST" }).catch(() => {});
  }
  activeLoginAccountId = null;
});

loadAccounts().catch((err) => {
  accountsList.textContent = "Could not load accounts: " + err.message;
});
