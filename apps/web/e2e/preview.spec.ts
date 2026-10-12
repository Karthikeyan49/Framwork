import { test, expect, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { enterAccessToken, redactedRequests } from "./credentials";

const root = path.resolve("../..");
function provision(tenant: string, subject: string, role: string) {
  execFileSync(
    "node",
    [
      "scripts/with-env.mjs",
      "./bin/iam",
      "-issuer",
      "http://localhost:8080/dev-issuer",
      "-subject",
      subject,
      "-name",
      `Browser ${role}`,
      "-tenant",
      tenant,
      "-tenant-name",
      "Browser preview",
      "-role",
      role,
      "-user-status",
      "active",
      "-membership-status",
      "active",
      "-operator",
      "m6-browser-test",
    ],
    { cwd: root, stdio: "pipe" },
  );
  return execFileSync(
    "node",
    [
      "scripts/with-env.mjs",
      "node",
      "scripts/dev-identity.mjs",
      "token",
      subject,
    ],
    { cwd: root, encoding: "utf8" },
  ).trim();
}
async function connect(
  page: Page,
  token: string,
  tenant: string,
  app: string,
  heading = "Contact editor",
) {
  await page.goto("/access");
  await enterAccessToken(page, token);
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await page.getByLabel("Workspace", { exact: true }).selectOption(tenant);
  await page.getByLabel("Application ID").fill(app);
  await page.getByRole("button", { name: "Load preview" }).click();
  await expect(page.getByRole("heading", { name: heading })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Reload records" }),
  ).toBeEnabled();
}
test("compiled dynamic UI supports real CRUD, stale-version recovery, pagination and read-only policy", async ({
  page,
  request: rawRequest,
  browser,
}) => {
  const request = redactedRequests(rawRequest);
  const tenant = randomUUID();
  const key = randomUUID().replaceAll("-", "");
  const app = `app.browser_${key}`;
  const owner = provision(tenant, `m6-owner-${key}`, "owner");
  const viewer = provision(tenant, `m6-viewer-${key}`, "viewer");
  const headers = { Authorization: `Bearer ${owner}` };
  const metadata = JSON.parse(
    fs.readFileSync(
      path.join(root, "contracts/examples/contacts-draft-v1.json"),
      "utf8",
    ),
  );
  metadata.app_id = app;
  const version = await request.post(
    `http://127.0.0.1:8080/v1/tenants/${tenant}/metadata/${app}/versions`,
    { headers, data: { expected_revision: 0, document: metadata } },
  );
  expect(version.status()).toBe(201);
  const base = `http://127.0.0.1:8080/v1/tenants/${tenant}/preview/${app}/versions/1/entities/entity.contact`;
  await connect(page, owner, tenant, app);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page
    .getByLabel("Name", { exact: true })
    .fill('<script>alert("untrusted")</script>');
  await page.getByLabel("Status", { exact: true }).selectOption("active");
  await page.getByLabel("Balance", { exact: false }).fill("12.30");
  await page.getByLabel("Joined", { exact: false }).fill("2026-10-11");
  await page
    .getByRole("button", { name: "Create record", exact: true })
    .click();
  await expect(page.getByText("Record saved · version 1.")).toBeVisible();
  await expect(
    page.getByRole("cell", {
      name: '<script>alert("untrusted")</script>',
      exact: true,
    }),
  ).toBeVisible();
  expect(await page.locator("main script").count()).toBe(0);
  await page.getByRole("button", { name: "Open record" }).click();
  await page.getByLabel("Name", { exact: true }).fill("Updated in browser");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Record saved · version 2.")).toBeVisible();
  await page.getByRole("button", { name: "Open record" }).click();
  const list = await request.post(base + "/query", {
    headers,
    data: { projection: ["field.contact.id", "field.contact.name"], limit: 20 },
  });
  const row = (await list.json()).records[0];
  const concurrent = await request.post(base + "/commands", {
    headers,
    data: {
      operation: "update",
      id: row.id,
      expected_version: 2,
      values: { "field.contact.name": "Updated elsewhere" },
    },
  });
  expect(concurrent.status()).toBe(200);
  await page.getByLabel("Name", { exact: true }).fill("Stale edit");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("alert")).toContainText("This record changed");
  await page.getByRole("button", { name: "Reload records" }).click();
  await expect(
    page.getByRole("cell", { name: "Updated elsewhere", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Open record" }).click();
  await page
    .getByRole("button", { name: "Delete record", exact: true })
    .click();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(page.getByText("Record deleted.")).toBeVisible();
  await expect(page.getByText("No records yet.")).toBeVisible();
  for (let i = 0; i < 21; i++) {
    const response = await request.post(base + "/commands", {
      headers,
      data: {
        operation: "create",
        values: { "field.contact.name": `Page ${i}` },
      },
    });
    expect(response.status()).toBe(200);
  }
  await page.getByRole("button", { name: "Reload records" }).click();
  await expect(page.getByRole("button", { name: "Open record" })).toHaveCount(
    20,
  );
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page.getByRole("button", { name: "Open record" })).toHaveCount(
    1,
  );
  await expect(page.getByRole("button", { name: "Next page" })).toBeDisabled();
  const context = await browser.newContext();
  const read = await context.newPage();
  await connect(read, viewer, tenant, app);
  await expect(read.getByText("Read-only access")).toBeVisible();
  await expect(
    read.getByRole("button", { name: "Create record", exact: true }),
  ).toBeDisabled();
  const denied = await request.post(base + "/commands", {
    headers: { Authorization: `Bearer ${viewer}` },
    data: { operation: "create", values: { "field.contact.name": "Denied" } },
  });
  expect(denied.status()).toBe(403);
  await read.getByRole("button", { name: "Disconnect" }).click();
  await expect(
    read.getByRole("heading", { name: "Application preview" }),
  ).toHaveCount(0);
  await context.close();
  await page.getByLabel("Draft revision").fill("2");
  await expect(
    page.getByRole("heading", { name: "Contact editor" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Load preview" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
});

test("credential entry failure redacts the value from error output", async ({
  page,
}) => {
  await page.setContent(
    '<label>API access token<input type="password" disabled /></label>',
  );
  let safe = "";
  try {
    await enterAccessToken(page, "synthetic-public-marker", 100);
  } catch (e) {
    safe = e instanceof Error ? (e.stack ?? e.message) : "";
  }
  expect(safe).toContain("Could not enter the test access token");
  expect(safe).not.toContain("synthetic-public-marker");
  expect(safe).not.toContain("fill(");
});

test("authenticated fixture failure redacts header values", async ({
  request,
}) => {
  let safe = "";
  try {
    await redactedRequests(request).post("http://127.0.0.1:1", {
      headers: { Authorization: "Bearer synthetic-public-marker" },
      timeout: 100,
    });
  } catch (e) {
    safe = e instanceof Error ? (e.stack ?? e.message) : "";
  }
  expect(safe).toContain("Authenticated fixture request failed");
  expect(safe).not.toContain("synthetic-public-marker");
  expect(safe).not.toContain("Authorization");
});

test("long metadata headings and record badges stay inside a mobile viewport", async ({
  page,
  request: rawRequest,
}) => {
  const request = redactedRequests(rawRequest);
  const tenant = randomUUID();
  const key = randomUUID().replaceAll("-", "");
  const app = `app.mobile_${key}`;
  const owner = provision(tenant, `m6r-mobile-${key}`, "owner");
  const headers = { Authorization: `Bearer ${owner}` };
  const metadata = JSON.parse(
    fs.readFileSync(
      path.join(root, "contracts/examples/contacts-draft-v1.json"),
      "utf8",
    ),
  );
  metadata.app_id = app;
  const definition = metadata.definitions.find(
    (d: { kind: string }) => d.kind === "page",
  );
  definition.label = "W".repeat(200);
  definition.page.components.unshift({
    type: "status_badge",
    field_ref: "field.contact.name",
    label: "Current name",
  });
  expect(
    (
      await request.post(
        `http://127.0.0.1:8080/v1/tenants/${tenant}/metadata/${app}/versions`,
        { headers, data: { expected_revision: 0, document: metadata } },
      )
    ).status(),
  ).toBe(201);
  expect(
    (
      await request.post(
        `http://127.0.0.1:8080/v1/tenants/${tenant}/preview/${app}/versions/1/entities/entity.contact/commands`,
        {
          headers,
          data: {
            operation: "create",
            values: {
              "field.contact.name": "x".repeat(4096),
              "field.contact.status": "active",
            },
          },
        },
      )
    ).status(),
  ).toBe(200);
  await page.setViewportSize({ width: 390, height: 844 });
  await connect(page, owner, tenant, app, definition.label);
  await page.getByRole("button", { name: "Open record", exact: true }).click();
  await expect(
    page.getByText("Current name: " + "x".repeat(4096), { exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
