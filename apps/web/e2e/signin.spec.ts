import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import path from "node:path";
const root = path.resolve("../..");
function provision(tenant: string, subject: string, role: string, active = true) {
  try {
    execFileSync("node", ["scripts/with-env.mjs", "./bin/iam", "-issuer", "http://localhost:8080/dev-issuer", "-subject", subject,
      "-name", `Browser login ${role}`, "-tenant", tenant, "-tenant-name", "OIDC browser workspace", "-role", role,
      "-user-status", active ? "active" : "disabled", "-membership-status", "active", "-operator", "r5f-browser-fixture"], { cwd: root, stdio: "pipe" });
  } catch { throw new Error("Could not provision the isolated browser identity fixture."); }
}
function identity() {
  const tenant = randomUUID(); const subject = "browser-viewer-" + randomUUID();
  provision(tenant, "browser-owner-" + randomUUID(), "owner"); provision(tenant, subject, "viewer"); return { tenant, subject };
}
async function signIn(page: Page, subject: string, mode = "valid") {
  await page.goto("/access"); await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Test identity provider", exact: true })).toBeVisible();
  await page.getByLabel("Test identity subject", { exact: true }).fill(subject);
  await page.getByLabel("Response mode", { exact: true }).selectOption(mode);
  await page.getByRole("button", { name: "Continue with test provider", exact: true }).click();
}
for (const mode of ["valid", "within_skew"]) {
  test(`real OIDC ${mode} flow verifies a viewer, scrubs credentials and survives SPA navigation`, async ({ page, request }) => {
    const fixture = identity(); let referrerLeak = false; let consoleLeak = false;
    page.on("request", (r) => { if (/[?&](code|state)=/.test(r.headers()["referer"] ?? "")) referrerLeak = true; });
    page.on("console", (message) => { if (/PRIVATE|[A-Za-z0-9_-]{32,}\.[A-Za-z0-9_-]{32,}\.[A-Za-z0-9_-]{32,}/.test(message.text())) consoleLeak = true; });
    const before = await (await request.get("http://127.0.0.1:8090/stats")).json();
    await signIn(page, fixture.subject, mode);
    await expect(page.getByRole("heading", { name: "Browser login viewer", exact: true })).toBeVisible();
    await expect.poll(() => new URL(page.url()).pathname).toBe("/access");
    expect(await page.evaluate(() => location.search === "" && location.hash === "")).toBe(true);
    await page.getByLabel("Workspace", { exact: true }).selectOption(fixture.tenant);
    await expect(page.getByText("Access verified · viewer", { exact: true })).toBeVisible();
    await expect(page.getByText("Access verified · owner", { exact: true })).toHaveCount(0);
    await page.getByRole("link", { name: "Overview", exact: true }).click(); await page.getByRole("link", { name: "Workspace access", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Browser login viewer", exact: true })).toBeVisible();
    expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
    const after = await (await request.get("http://127.0.0.1:8090/stats")).json();
    expect(after.exchanges - before.exchanges).toBe(1); expect(after.issued - before.issued).toBe(1); expect(referrerLeak).toBe(false); expect(consoleLeak).toBe(false);
    await page.getByRole("button", { name: "Disconnect", exact: true }).click(); await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Browser login viewer", exact: true })).toHaveCount(0);
  });
}
for (const mode of ["nonce", "id_future", "provider_error"]) {
  test(`OIDC ${mode} response cannot establish a connection or retry its code`, async ({ page, request }) => {
    const fixture = identity(); const before = await (await request.get("http://127.0.0.1:8090/stats")).json();
    await signIn(page, fixture.subject, mode);
    await expect(page.getByRole("alert")).toContainText("Sign-in was not completed");
    await expect(page.getByLabel("Workspace", { exact: true })).toHaveCount(0);
    expect(await page.evaluate(() => location.search === "" && location.hash === "" && !document.body.textContent?.includes("PRIVATE"))).toBe(true);
    const after = await (await request.get("http://127.0.0.1:8090/stats")).json(); expect(after.exchanges - before.exchanges).toBe(1);
  });
}
test("a signed provider identity is not enrolled automatically", async ({ page }) => {
  await signIn(page, "unregistered-" + randomUUID());
  await expect(page.getByRole("alert")).toContainText("account has no access"); await expect(page.getByLabel("Workspace", { exact: true })).toHaveCount(0);
});
test("live identity revocation on an administration read clears the exact current connection", async ({ page }) => {
  const fixture = identity(); await signIn(page, fixture.subject);
  await expect(page.getByRole("heading", { name: "Browser login viewer", exact: true })).toBeVisible();
  await page.getByLabel("Workspace", { exact: true }).selectOption(fixture.tenant); await expect(page.getByText("Access verified · viewer", { exact: true })).toBeVisible();
  provision(fixture.tenant, fixture.subject, "viewer", false);
  await page.getByRole("button", { name: "Manage teams", exact: true }).click();
  await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Browser login viewer", exact: true })).toHaveCount(0);
  await expect(page.getByLabel("Workspace", { exact: true })).toHaveCount(0);
});
