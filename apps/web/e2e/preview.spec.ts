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

test("a lost HTTP response is reconciled through a real owned command receipt without a second write", async ({ page, request: rawRequest }) => {
  const request = redactedRequests(rawRequest);
  const tenant = randomUUID();
  const key = randomUUID().replaceAll("-", "");
  const app = `app.receipt_${key}`;
  const owner = provision(tenant, `m9r-receipt-${key}`, "owner");
  const headers = { Authorization: `Bearer ${owner}` };
  const metadata = JSON.parse(fs.readFileSync(path.join(root, "contracts/examples/contacts-draft-v1.json"), "utf8"));
  metadata.app_id = app;
  expect((await request.post(`http://127.0.0.1:8080/v1/tenants/${tenant}/metadata/${app}/versions`,
    { headers, data: { expected_revision: 0, document: metadata } })).status()).toBe(201);
  await connect(page, owner, tenant, app);
  let writes = 0;
  let commandKey = "";
  await page.route("**/entities/entity.contact/commands", async (route) => {
    writes++;
    commandKey = route.request().postDataJSON().command_key;
    const committed = await route.fetch().catch(() => { throw new Error("Fault fixture request failed"); });
    expect(committed.status()).toBe(200);
    await route.abort("connectionreset");
  });
  await page.getByLabel("Name", { exact: true }).fill("Reconciled once");
  await page.getByRole("button", { name: "Create record", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("outcome is uncertain");
  await expect(page.getByRole("button", { name: "Create record", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Reload records" }).click();
  await expect(page.getByRole("cell", { name: "Reconciled once", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create record", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Check command receipt" }).click();
  await expect(page.getByText("Command committed · version 1.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Create record", exact: true })).toBeEnabled();
  expect(writes).toBe(1);
  const receipt = await request.get(`http://127.0.0.1:8080/v1/tenants/${tenant}/preview/${app}/commands/${commandKey}`, { headers });
  expect(receipt.status()).toBe(200);
  expect((await receipt.json()).state).toBe("COMMITTED");
  const records = await request.post(`http://127.0.0.1:8080/v1/tenants/${tenant}/preview/${app}/versions/1/entities/entity.contact/query`,
    { headers, data: { projection: ["field.contact.name"], limit: 100 } });
  expect(records.status()).toBe(200);
  expect((await records.json()).records).toHaveLength(1);
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

test("limited editor sees masked records and patches a public field without replacing secrets", async ({ page, request: rawRequest }) => {
  const request = redactedRequests(rawRequest);
  const tenant = randomUUID();
  const key = randomUUID().replaceAll("-", "");
  const app = `app.mask_${key}`;
  const owner = provision(tenant, `mask-owner-${key}`, "owner");
  const editor = provision(tenant, `mask-editor-${key}`, "viewer");
  const headers = { Authorization: `Bearer ${owner}` };
  const apiRoot = `http://127.0.0.1:8080/v1/tenants/${tenant}`;
  const metadata = JSON.parse(fs.readFileSync(path.join(root, "contracts/examples/contacts-draft-v1.json"), "utf8"));
  metadata.app_id = app;
  expect((await request.post(`${apiRoot}/metadata/${app}/versions`, { headers, data: { expected_revision: 0, document: metadata } })).status()).toBe(201);
  const base = `${apiRoot}/preview/${app}/versions/1/entities/entity.contact`;
  const created = await request.post(`${base}/commands`, { headers, data: { operation: "create", values: { "field.contact.name": "PRIVATE browser secret", "field.contact.balance": { amount: "12.30", currency: "USD" }, "field.contact.status": "active" } } });
  expect(created.status()).toBe(200);
  const record = await created.json();
  const members = await (await request.get(`${apiRoot}/members`, { headers })).json();
  const user = members.members.find((m: { role: string }) => m.role === "viewer").user_id;
  const resource = { app_id: app, entity_id: "entity.contact", field_id: "*" };
  const definition = { schema_version: "tenant-capabilities-v1", name: "limited_editor", grants: [
    { effect: "ALLOW", actions: ["tenant.access"], resource: { app_id: "*", entity_id: "*", field_id: "*" } },
    { effect: "ALLOW", actions: ["preview.read", "preview.write", "commands.reconcile"], resource: { ...resource, entity_id: "*" } },
    { effect: "ALLOW", actions: ["data.row.read", "data.row.write"], resource },
    { effect: "ALLOW", actions: ["data.entity.read", "data.entity.write", "data.field.read", "data.field.write"], resource, scope: { kind: "ALL" } },
    ...["field.contact.name", "field.contact.balance"].map((field_id) => ({ effect: "DENY", actions: ["data.field.read", "data.field.write"], resource: { ...resource, field_id } })),
  ] };
  expect((await request.post(`${apiRoot}/roles`, { headers, data: { operation_id: randomUUID(), expected_version: 0, active: true, definition } })).status()).toBe(200);
  expect((await request.post(`${apiRoot}/members/${user}/role`, { headers, data: { role: "limited_editor", expected_role: "viewer" } })).status()).toBe(200);
  await connect(page, editor, tenant, app);
  await expect(page.getByText("PRIVATE browser secret", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Open record" }).click();
  await expect(page.getByRole("textbox", { name: "Name", exact: true })).toHaveCount(0);
  await expect(page.getByText("Name: Restricted", { exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Status", exact: true }).selectOption("inactive");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Record saved · version 2.")).toBeVisible();
  const stored = await (await request.post(`${base}/query`, { headers, data: { record_id: record.id, projection: ["field.contact.name", "field.contact.balance", "field.contact.status"], limit: 1 } })).json();
  expect(stored.records[0].values["field.contact.name"]).toBe("PRIVATE browser secret");
  expect(stored.records[0].values["field.contact.balance"]).toEqual({ amount: "12.3", currency: "USD" });
  expect(stored.records[0].values["field.contact.status"]).toBe("inactive");
});

test("governed team and record administration uses real versions, rejects conflicts and denies viewers", async ({ page, request: rawRequest }) => {
  let duplicateKey = false;
  page.on("console", (message) => { if (message.type() === "error" && message.text().includes("same key")) duplicateKey = true; });
  const request = redactedRequests(rawRequest);
  const tenant = randomUUID(); const key = randomUUID().replaceAll("-", ""); const app = `app.admin_${key}`;
  const owner = provision(tenant, `admin-owner-${key}`, "owner");
  const viewer = provision(tenant, `admin-viewer-${key}`, "viewer");
  const headers = { Authorization: `Bearer ${owner}` }; const api = `http://127.0.0.1:8080/v1/tenants/${tenant}`;
  const metadata = JSON.parse(fs.readFileSync(path.join(root, "contracts/examples/contacts-draft-v1.json"), "utf8")); metadata.app_id = app;
  expect((await request.post(`${api}/metadata/${app}/versions`, { headers, data: { expected_revision: 0, document: metadata } })).status()).toBe(201);
  const members = await (await request.get(`${api}/members`, { headers })).json();
  const user = members.members.find((m: { role: string }) => m.role === "viewer").user_id;
  const dataRoot = `${api}/preview/${app}/versions/1/entities/entity.contact`;
  const made = await request.post(`${dataRoot}/commands`, { headers, data: { operation: "create", values: { "field.contact.name": "Admin browser record" } } }); expect(made.status()).toBe(200); const record = await made.json();
  await connect(page, owner, tenant, app);
  await page.getByRole("button", { name: "Manage teams", exact: true }).click();
  await page.getByLabel("New team name").fill("Finance"); await page.getByRole("button", { name: "Create team", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Finance", exact: true })).toBeVisible();
  const teams = (await (await request.get(`${api}/teams`, { headers })).json()).teams;
  const team = teams.find((t: { name: string }) => t.name === "Finance"); expect(team.version).toBe(1);
  await page.getByLabel("Team name", { exact: true }).fill("Finance operations"); await page.getByRole("button", { name: "Save team", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Finance operations", exact: true })).toBeVisible();
  await page.getByLabel("Member user ID").fill(user); await page.getByRole("button", { name: "Save membership", exact: true }).click();
  await expect(page.getByRole("button", { name: `Deactivate member ${user}`, exact: true })).toBeVisible();
  await page.getByRole("button", { name: `Deactivate member ${user}`, exact: true }).click();
  await expect(page.getByRole("button", { name: `Activate member ${user}`, exact: true })).toBeVisible();
  await page.getByLabel("Parent team ID").fill(team.id); await page.getByRole("button", { name: "Save team", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Check the IDs");
  await page.getByRole("button", { name: "Reload selected team", exact: true }).click();
  await expect(page.getByLabel("Parent team ID")).toHaveValue("");
  expect((await request.post(`${api}/teams`, { headers, data: { id: team.id, name: "Concurrent finance", expected_version: 2 } })).status()).toBe(200);
  await page.getByLabel("Team name", { exact: true }).fill("Stale edit"); await page.getByRole("button", { name: "Save team", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("state changed");
  await page.getByRole("button", { name: "Reload selected team", exact: true }).click();
  await expect(page.getByLabel("Team name", { exact: true })).toHaveValue("Concurrent finance");
  await page.getByRole("button", { name: "Close teams", exact: true }).click();
  await page.getByRole("button", { name: "Open record", exact: true }).click();
  await page.getByRole("button", { name: "Manage ownership", exact: true }).click();
  await page.getByLabel("Owner team ID", { exact: true }).fill(team.id);
  await page.getByRole("checkbox").check(); await page.getByRole("button", { name: "Transfer ownership", exact: true }).click();
  await expect(page.getByText("Access change saved · version 2.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Back to records", exact: true }).click();
  await page.getByRole("button", { name: "Open record", exact: true }).click(); await page.getByRole("button", { name: "Manage sharing", exact: true }).click();
  await page.getByLabel("Recipient user ID").fill(user); await page.getByRole("button", { name: "Save share", exact: true }).click();
  await expect(page.getByRole("button", { name: `Revoke share for ${user}`, exact: true })).toBeVisible();
  let access = await (await request.get(`${dataRoot}/records/${record.id}/access`, { headers })).json();
  expect(access.version).toBe(3); expect(access.owner_team_id).toBe(team.id); expect(access.shares).toEqual([{ user_id: user, can_write: false }]);
  await page.getByRole("button", { name: `Revoke share for ${user}`, exact: true }).click(); await expect(page.getByText("Access change saved · version 4.")).toBeVisible();
  access = await (await request.get(`${dataRoot}/records/${record.id}/access`, { headers })).json(); expect(access.shares ?? []).toHaveLength(0);
  await connect(page, viewer, tenant, app);
  await page.getByRole("button", { name: "Manage teams", exact: true }).click(); await expect(page.getByRole("alert")).toContainText("permissions"); await expect(page.getByLabel("New team name")).toHaveCount(0);
  await page.getByRole("button", { name: "Close teams", exact: true }).click(); await page.getByRole("button", { name: "Open record", exact: true }).click();
  await page.getByRole("button", { name: "Manage sharing", exact: true }).click(); await expect(page.getByRole("alert")).toContainText("permissions"); await expect(page.getByLabel("Recipient user ID")).toHaveCount(0);
  await page.getByRole("button", { name: "Back to records", exact: true }).click(); await page.getByRole("button", { name: "Open record", exact: true }).click(); await page.getByRole("button", { name: "Manage ownership", exact: true }).click();
  await page.getByLabel("Owner user ID").fill(user); await page.getByRole("checkbox").check(); await page.getByRole("button", { name: "Transfer ownership", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("permissions");
  expect((await (await request.get(`${dataRoot}/records/${record.id}/access`, { headers })).json()).version).toBe(4);
  expect(duplicateKey).toBe(false);
});

test("lost administration responses preserve IDs through navigation and are reviewed without another POST", async ({ page, request: rawRequest }) => {
  const request = redactedRequests(rawRequest); const tenant = randomUUID(); const key = randomUUID().replaceAll("-", ""); const app = `app.admin_loss_${key}`;
  const owner = provision(tenant, `loss-owner-${key}`, "owner"); provision(tenant, `loss-viewer-${key}`, "viewer");
  const headers = { Authorization: `Bearer ${owner}` }; const api = `http://127.0.0.1:8080/v1/tenants/${tenant}`;
  const metadata = JSON.parse(fs.readFileSync(path.join(root, "contracts/examples/contacts-draft-v1.json"), "utf8")); metadata.app_id = app;
  expect((await request.post(`${api}/metadata/${app}/versions`, { headers, data: { expected_revision: 0, document: metadata } })).status()).toBe(201);
  const session = await (await request.get(`${api}/members`, { headers })).json(); const user = session.members.find((m: { role: string }) => m.role === "viewer").user_id;
  const entity = `${api}/preview/${app}/versions/1/entities/entity.contact`;
  const made = await request.post(`${entity}/commands`, { headers, data: { operation: "create", values: { "field.contact.name": "Recovery record" } } }); expect(made.status()).toBe(200); const record = await made.json();
  await connect(page, owner, tenant, app);
  let teamWrites = 0; let creation = "";
  await page.route(`**/v1/tenants/${tenant}/teams`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    teamWrites++; creation = route.request().postDataJSON().id;
    const result = await route.fetch().catch(() => { throw new Error("Administration fault fixture failed"); }); expect(result.status()).toBe(200); await route.abort("connectionreset");
  });
  await page.getByRole("button", { name: "Manage teams", exact: true }).click(); await page.getByLabel("New team name").fill("Recover once"); await page.getByRole("button", { name: "Create team", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("result is uncertain");
  await page.getByRole("link", { name: "Overview", exact: true }).click(); await page.getByRole("link", { name: "Workspace access", exact: true }).click();
  await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toBeVisible();
  await expect(page.getByLabel("API access token")).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Pending administration changes" })).toContainText(creation);
  await page.getByLabel("Workspace", { exact: true }).selectOption(tenant); await page.getByRole("button", { name: "Manage teams", exact: true }).click();
  await expect(page.getByRole("button", { name: "Create team", exact: true })).toBeDisabled(); await page.getByRole("button", { name: `Inspect team ${creation}`, exact: true }).click();
  await expect(page.getByRole("heading", { name: "Recover once", exact: true })).toBeVisible(); await page.getByRole("button", { name: "Use reviewed team state", exact: true }).click();
  expect(teamWrites).toBe(1); const teams = (await (await request.get(`${api}/teams`, { headers })).json()).teams; expect(teams.filter((t: { id: string }) => t.id === creation)).toHaveLength(1);
  await page.getByRole("button", { name: "Close teams", exact: true }).click(); await page.getByLabel("Application ID").fill(app); await page.getByRole("button", { name: "Load preview", exact: true }).click(); await page.getByRole("button", { name: "Open record", exact: true }).click(); await page.getByRole("button", { name: "Manage sharing", exact: true }).click();
  let accessWrites = 0;
  await page.route(`**/records/${record.id}/access`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    accessWrites++; const result = await route.fetch().catch(() => { throw new Error("Administration fault fixture failed"); }); expect(result.status()).toBe(200); await route.abort("connectionreset");
  });
  await page.getByLabel("Recipient user ID").fill(user); await page.getByRole("button", { name: "Save share", exact: true }).click();
  await expect(page.getByText(/An access change has an uncertain outcome/)).toBeVisible(); await expect(page.getByRole("button", { name: "Use reviewed record state", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Reload record access", exact: true }).click(); await expect(page.getByRole("button", { name: `Revoke share for ${user}`, exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Use reviewed record state", exact: true }).click(); expect(accessWrites).toBe(1);
  const access = await (await request.get(`${entity}/records/${record.id}/access`, { headers })).json(); expect(access.version).toBe(2); expect(access.shares).toEqual([{ user_id: user, can_write: false }]);
});
