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
  if (res.status === 204) return null;
  return res.json();
}

async function loadAccounts() {
  const data = await api("/api/accounts");
  renderAccounts(data.accounts || []);
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
    const launchBtn = node.querySelector(".launch-terminal");
    if (account.status !== "linked") {
      connectBtn.hidden = false;
      launchBtn.hidden = true;
    }

    node.querySelector(".copy-alias").addEventListener("click", () => {
      navigator.clipboard?.writeText(account.alias).catch(() => {});
    });
    connectBtn.addEventListener("click", () => startLogin(account));
    launchBtn.addEventListener("click", async () => {
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

async function removeAccount(account) {
  if (!confirm(`Remove "${account.name}"? This deletes its local login state.`)) return;
  try {
    await api(`/api/accounts/${account.id}?confirm=true`, { method: "DELETE" });
    await loadAccounts();
  } catch (err) {
    alert("Could not remove account: " + err.message);
  }
}

document.getElementById("add-account-btn").addEventListener("click", () => {
  addNameInput.value = "";
  addDialog.showModal();
});
document.getElementById("add-cancel").addEventListener("click", () => addDialog.close());

addForm.addEventListener("submit", async () => {
  const name = addNameInput.value.trim();
  if (!name) return;
  try {
    const account = await api("/api/accounts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    await loadAccounts();
    startLogin(account);
  } catch (err) {
    alert("Could not create account: " + err.message);
  }
});

document.getElementById("rename-cancel").addEventListener("click", () => renameDialog.close());
renameForm.addEventListener("submit", async () => {
  const id = renameIdInput.value;
  const name = renameNameInput.value.trim();
  if (!name) return;
  try {
    await api(`/api/accounts/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    await loadAccounts();
  } catch (err) {
    alert("Could not rename account: " + err.message);
  }
});

let activeEventSource = null;

function startLogin(account) {
  loginStatus.textContent = "Starting login…";
  loginUrlBox.hidden = true;
  loginDialog.showModal();

  api(`/api/accounts/${account.id}/login`, { method: "POST" })
    .then(() => {
      if (activeEventSource) activeEventSource.close();
      const es = new EventSource(`/api/accounts/${account.id}/login/events`);
      activeEventSource = es;

      es.addEventListener("message", (e) => {
        const event = JSON.parse(e.data);
        handleLoginEvent(event);
      });
      es.addEventListener("error", () => {
        // The server closes the stream once the login reaches a
        // terminal state; a plain connection drop needs no message.
      });
    })
    .catch((err) => {
      loginStatus.textContent = "Could not start login: " + err.message;
    });
}

function handleLoginEvent(event) {
  switch (event.type) {
    case "url":
      loginStatus.textContent = "Open this URL to finish logging in:";
      loginUrlBox.hidden = false;
      loginUrlLink.href = event.url;
      loginUrlLink.textContent = event.url;
      break;
    case "linked":
      loginStatus.textContent = "Connected! This account is ready to use.";
      loginUrlBox.hidden = true;
      activeEventSource?.close();
      loadAccounts();
      break;
    case "timeout":
      loginStatus.textContent = "Timed out waiting for login. You can try again.";
      activeEventSource?.close();
      loadAccounts();
      break;
    case "failed":
      loginStatus.textContent = "Login failed: " + (event.message || "unknown error");
      activeEventSource?.close();
      loadAccounts();
      break;
  }
}

document.getElementById("login-copy").addEventListener("click", () => {
  if (loginUrlLink.href) navigator.clipboard?.writeText(loginUrlLink.href).catch(() => {});
});
document.getElementById("login-close").addEventListener("click", () => {
  activeEventSource?.close();
  loginDialog.close();
});

loadAccounts().catch((err) => {
  accountsList.textContent = "Could not load accounts: " + err.message;
});
