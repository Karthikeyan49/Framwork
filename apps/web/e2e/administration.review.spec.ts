import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { enterAccessToken, redactedRequests } from "./credentials";
import { decodeResult } from "../src/preview/contracts";

const root = path.resolve("../..");
function provision(tenant: string, subject: string, role: string) {
  execFileSync("node", ["scripts/with-env.mjs", "./bin/iam", "-issuer", "http://localhost:8080/dev-issuer", "-subject", subject,
    "-name", `Independent ${role}`, "-tenant", tenant, "-tenant-name", "Independent administration review", "-role", role,
    "-user-status", "active", "-membership-status", "active", "-operator", "r5c-independent-browser"], { cwd: root, stdio: "pipe" });
  return execFileSync("node", ["scripts/with-env.mjs", "node", "scripts/dev-identity.mjs", "token", subject], { cwd: root, encoding: "utf8" }).trim();
}

test("independent assign-only delegated administrator transfers an OWNER-scoped masked record and loses access", async ({ page, request: rawRequest }) => {
  const request = redactedRequests(rawRequest);
  const tenant = randomUUID(); const suffix = randomUUID().replaceAll("-", ""); const app = `app.review_admin_${suffix}`;
  const ownerToken = provision(tenant, `review-owner-${suffix}`, "owner");
  const delegateToken = provision(tenant, `review-delegate-${suffix}`, "viewer");
  provision(tenant, `review-recipient-${suffix}`, "member");
  const headers = { Authorization: `Bearer ${ownerToken}` };
  const delegated = { Authorization: `Bearer ${delegateToken}` };
  const api = `http://127.0.0.1:8080/v1/tenants/${tenant}`;
  const members = await (await request.get(`${api}/members`, { headers })).json();
  const delegate = members.members.find((m: { role: string }) => m.role === "viewer").user_id;
  const recipient = members.members.find((m: { role: string }) => m.role === "member").user_id;
  const metadata = JSON.parse(fs.readFileSync(path.join(root, "contracts/examples/contacts-draft-v1.json"), "utf8")); metadata.app_id = app;
  expect((await request.post(`${api}/metadata/${app}/versions`, { headers, data: { expected_revision: 0, document: metadata } })).status()).toBe(201);
  const entity = `${api}/preview/${app}/versions/1/entities/entity.contact`;
  const created = await request.post(`${entity}/commands`, { headers, data: { operation: "create", values: { "field.contact.name": "CONFIDENTIAL independent review value", "field.contact.status": "active" } } });
  expect(created.status()).toBe(200); const record = await created.json();
  expect((await request.post(`${entity}/records/${record.id}/access`, { headers, data: { operation: "owner", expected_version: 1, user_id: delegate } })).status()).toBe(200);
  const resource = { app_id: app, entity_id: "entity.contact", field_id: "*" };
  const definition = { schema_version: "tenant-capabilities-v1", name: "independent_assign_only", grants: [
    { effect: "ALLOW", actions: ["tenant.access"], resource: { app_id: "*", entity_id: "*", field_id: "*" } },
    { effect: "ALLOW", actions: ["preview.read"], resource: { ...resource, entity_id: "*" } },
    { effect: "ALLOW", actions: ["data.row.read", "data.row.write", "data.row.assign"], resource },
    { effect: "ALLOW", actions: ["data.entity.read", "data.entity.write"], resource, scope: { kind: "OWNER" } },
    { effect: "ALLOW", actions: ["data.field.read"], resource, scope: { kind: "ALL" } },
    { effect: "DENY", actions: ["data.field.read"], resource: { ...resource, field_id: "field.contact.name" } },
  ] };
  expect((await request.post(`${api}/roles`, { headers, data: { operation_id: randomUUID(), expected_version: 0, active: true, definition } })).status()).toBe(200);
  expect((await request.post(`${api}/members/${delegate}/role`, { headers, data: { role: definition.name, expected_role: "viewer" } })).status()).toBe(200);
  expect((await request.get(`${api}/members`, { headers: delegated })).status()).toBe(403);
  expect((await request.get(`${api}/teams`, { headers: delegated })).status()).toBe(403);
  expect((await request.get(`${entity}/records/${record.id}/access`, { headers: delegated })).status()).toBe(403);
  const before = await request.post(entity + "/query", { headers: delegated, data: { projection: ["field.contact.id", "field.contact.name", "field.contact.balance", "field.contact.status", "field.contact.joined", "field.contact.parent"], limit: 20 } });
  expect(before.status()).toBe(200);
  const visible = await before.json();
  expect(() => decodeResult(visible)).not.toThrow();
  expect(visible.records).toHaveLength(1);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/access"); await enterAccessToken(page, delegateToken);
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await page.getByLabel("Workspace", { exact: true }).selectOption(tenant);
  await page.getByLabel("Application ID").fill(app); await page.getByRole("button", { name: "Load preview", exact: true }).click();
  await page.getByRole("button", { name: "Open record", exact: true }).click();
  await expect(page.getByText("CONFIDENTIAL independent review value", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Manage ownership", exact: true }).click();
  await expect(page.getByLabel("Owner user ID", { exact: true })).toHaveValue(delegate);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole("button", { name: "Load member suggestions", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "permissions" })).toBeVisible();
  await page.getByLabel("Owner user ID", { exact: true }).fill(recipient);
  await page.getByRole("checkbox").check(); await page.getByRole("button", { name: "Transfer ownership", exact: true }).click();
  await expect(page.getByText("Access change saved · version 3.", { exact: true })).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("unavailable");
  await expect(page.getByRole("form", { name: "Change record ownership" })).toHaveCount(0);
  const stored = await (await request.get(`${entity}/records/${record.id}/access`, { headers })).json();
  expect(stored.owner_user_id).toBe(recipient); expect(stored.version).toBe(3);
  const query = await request.post(`${entity}/query`, { headers: delegated, data: { projection: ["field.contact.id"], limit: 20 } });
  expect(query.status()).toBe(200); expect((await query.json()).records).toHaveLength(0);
});
