"use strict";

const $ = (selector) => document.querySelector(selector);
const message = $("#message");

function show(text, isError = false) {
  message.textContent = text;
  message.classList.toggle("error", isError);
}

function toBuffer(value) {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(base64 + "=".repeat((4 - (base64.length % 4)) % 4));
  return Uint8Array.from(binary, (char) => char.charCodeAt(0)).buffer;
}

function toBase64URL(buffer) {
  let binary = "";
  for (const byte of new Uint8Array(buffer)) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

async function post(path, body = {}) {
  const response = await fetch(path, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || "Request failed.");
  return data;
}

function credentialJSON(credential) {
  const response = credential.response;
  const json = {
    id: credential.id,
    rawId: toBase64URL(credential.rawId),
    type: credential.type,
    clientExtensionResults: {},
    response: { clientDataJSON: toBase64URL(response.clientDataJSON) },
  };
  if (credential.authenticatorAttachment) json.authenticatorAttachment = credential.authenticatorAttachment;
  if (response.attestationObject) {
    json.response.attestationObject = toBase64URL(response.attestationObject);
    json.response.transports = response.getTransports ? response.getTransports() : [];
  } else {
    json.response.authenticatorData = toBase64URL(response.authenticatorData);
    json.response.signature = toBase64URL(response.signature);
    if (response.userHandle) json.response.userHandle = toBase64URL(response.userHandle);
  }
  return json;
}

async function signIn() {
  const begin = await post("/auth/login/begin");
  const publicKey = begin.options.publicKey;
  publicKey.challenge = toBuffer(publicKey.challenge);
  if (publicKey.allowCredentials) {
    publicKey.allowCredentials = publicKey.allowCredentials.map((item) => ({ ...item, id: toBuffer(item.id) }));
  }
  const credential = await navigator.credentials.get({ publicKey });
  await post("/auth/login/finish", { ceremony: begin.ceremony, credential: credentialJSON(credential) });
}

async function register(body) {
  const begin = await post("/auth/register/begin", body);
  const publicKey = begin.options.publicKey;
  publicKey.challenge = toBuffer(publicKey.challenge);
  publicKey.user.id = toBuffer(publicKey.user.id);
  if (publicKey.excludeCredentials) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map((item) => ({ ...item, id: toBuffer(item.id) }));
  }
  const credential = await navigator.credentials.create({ publicKey });
  return post("/auth/register/finish", { ceremony: begin.ceremony, credential: credentialJSON(credential) });
}

function describe(error) {
  if (error && error.name === "NotAllowedError") return "The passkey prompt was cancelled or timed out.";
  if (error && error.name === "InvalidStateError") return "This device already has a passkey for this app.";
  return (error && error.message) || "Something went wrong.";
}

async function busy(button, task) {
  button.disabled = true;
  show("");
  try {
    await task();
  } catch (error) {
    show(describe(error), true);
  } finally {
    button.disabled = false;
  }
}

async function render() {
  const response = await fetch("/auth/status");
  const status = await response.json();
  if (status.authenticated && !new URLSearchParams(location.search).has("account")) {
    location.replace("/");
    return;
  }
  $("#signin").hidden = status.authenticated;
  $("#account").hidden = !status.authenticated;
  $("#setup").hidden = status.authenticated;
  $("#setup").open = !status.registered;
  if (status.authenticated && status.passkey) {
    $("#account-text").textContent = `You are signed in with the passkey “${status.passkey}”.`;
  }
}

function init() {
  if (!window.PublicKeyCredential) {
    show("This browser does not support passkeys.", true);
  }

  $("#signin-button").addEventListener("click", (event) =>
    busy(event.currentTarget, async () => {
      await signIn();
      location.replace("/");
    }),
  );

  $("#setup-form").addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    busy(event.submitter, async () => {
      await register({ setupCode: form.get("setupCode"), name: form.get("name") });
      location.replace("/");
    });
  });

  $("#add-form").addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    busy(event.submitter, async () => {
      const result = await register({ name: form.get("name") });
      event.target.reset();
      show(`Added passkey “${result.name}”.`);
    });
  });

  $("#logout-button").addEventListener("click", (event) =>
    busy(event.currentTarget, async () => {
      await post("/auth/logout");
      location.replace("/login");
    }),
  );

  render().catch((error) => show(describe(error), true));
}

init();
