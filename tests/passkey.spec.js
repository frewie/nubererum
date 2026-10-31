// Passkey flows in a real browser against a running container started with
// APP_AUTH=passkey. Run: PASSKEY_CONTAINER=<name> PASSKEY_BASE_URL=http://localhost:3007 \
//   npx playwright test tests/passkey.spec.js
const { test, expect } = require("@playwright/test");
const { execFileSync } = require("node:child_process");

const container = process.env.PASSKEY_CONTAINER;
const baseURL = process.env.PASSKEY_BASE_URL || "http://localhost:3007";

test.skip(!container, "set PASSKEY_CONTAINER to a running passkey-mode container");
test.use({ baseURL });

function docker(...args) {
  return execFileSync("docker", ["exec", container, "nubererum", ...args], { encoding: "utf8" });
}

async function addAuthenticator(page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      transport: "internal",
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  });
  return { cdp, authenticatorId };
}

test("register with a setup code, sign out, sign in, remove", async ({ page }) => {
  const { cdp, authenticatorId } = await addAuthenticator(page);

  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  // A wrong code is rejected.
  await page.getByLabel("Setup code").fill("AAAA-BBBB-CCCC-DDDD");
  await page.getByRole("button", { name: "Register passkey" }).click();
  await expect(page.locator("#message")).toHaveText("The setup code is wrong or expired.");

  const code = docker("passkey-setup").match(/Setup code: (\S+)/)[1];
  await page.getByLabel("Setup code").fill(code);
  await page.getByLabel("Passkey name").fill("Virtual");
  await page.getByRole("button", { name: "Register passkey" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator("#account-link")).toBeVisible();

  const { credentials } = await cdp.send("WebAuthn.getCredentials", { authenticatorId });
  expect(credentials).toHaveLength(1);
  expect(credentials[0].isResidentCredential).toBe(true);

  const cookies = await page.context().cookies();
  const session = cookies.find((cookie) => cookie.name === "__Host-things_session");
  expect(session).toMatchObject({ httpOnly: true, secure: true, sameSite: "Strict", path: "/" });

  // Account page: sign out.
  await page.locator("#account-link").click();
  await expect(page.getByRole("heading", { name: "Signed in" })).toBeVisible();
  await expect(page.locator("#account-text")).toContainText("Virtual");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  expect((await page.request.get("/api/status")).status()).toBe(401);

  // Sign in with the discoverable credential.
  await page.getByRole("button", { name: "Sign in with passkey" }).click();
  await expect(page).toHaveURL(/\/$/);
  expect((await page.request.get("/api/status")).status()).toBe(200);

  // Removing the passkey on the server ends the session.
  const id = docker("passkey-list").split(/\s+/)[0];
  docker("passkey-remove", id);
  expect((await page.request.get("/api/status")).status()).toBe(401);
  expect(docker("passkey-list")).toContain("No passkeys registered.");
});
