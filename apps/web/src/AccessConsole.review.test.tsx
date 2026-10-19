import { createHash, webcrypto } from "node:crypto";
import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AccessConsole } from "./AccessConsole";
import { AuthProvider } from "./auth/Session";
import { RecoveryProvider } from "./administration/Recovery";
import { roleLimits, roleSchema, roleWireJSON } from "./administration/roleContracts";

const tenant = "11111111-1111-1111-1111-111111111111", actor = "22222222-2222-2222-2222-222222222222";
const operation = "33333333-3333-3333-3333-333333333333", otherActor = "44444444-4444-4444-4444-444444444444";
const definition = { schema_version: roleSchema, name: "review_role", grants: [] };
const role = { name: definition.name, version: 2, hash: createHash("sha256").update(roleWireJSON(definition)).digest("hex"), active: true, bootstrap: false, operation_id: operation, definition };
const contract = { schema_version: roleSchema, name: definition.name, root_protected: false, limits: roleLimits, actions: [{ name: "tenant.access", allow_supported: true, scope_required: false }] };
const reply = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
function Harness() {
  const [open, setOpen] = useState(true);
  return <RecoveryProvider><AuthProvider loadOptions={false}><button onClick={() => setOpen(!open)}>Navigate review route</button>{open && <AccessConsole/>}</AuthProvider></RecoveryProvider>;
}
function fixture(post: (path: string, init: RequestInit) => Response | Promise<Response>, access?: () => Response) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
    const path = String(url);
    if (init?.method === "POST") return post(path, init);
    if (path.endsWith("/session")) {
      const other = new Headers(init?.headers).get("Authorization") === "Bearer review-other";
      return reply({ user: { id: other ? otherActor : actor, name: other ? "Review other" : "Review owner" }, tenants: [{ id: tenant, name: "Review workspace" }] });
    }
    if (path.endsWith("/access")) return access?.() ?? reply({ tenant_id: tenant, roles: ["owner"], decision: { allow: true, decision_id: "review-decision", policy_version: "review-policy" } });
    if (path.includes("role-contract")) return reply(contract);
    if (path.endsWith("/roles")) return reply({ roles: [role] });
    if (path.includes("/members/")) return reply({ user_id: actor, role: "owner", active: true });
    if (path.includes("/roles/")) return reply(role);
    return reply({}, 403);
  });
}
async function connect(token = "review-owner") { await userEvent.type(screen.getByLabelText("API access token"), token); await userEvent.click(screen.getByRole("button", { name: /^Connect$/ })); await screen.findByLabelText("Workspace"); }
async function openRoles() { await userEvent.selectOptions(screen.getByLabelText("Workspace"), tenant); await userEvent.click(await screen.findByRole("button", { name: "Manage roles" })); await screen.findByRole("button", { name: "Inspect review_role" }); }
beforeEach(() => vi.stubGlobal("crypto", webcrypto));
afterEach(() => vi.unstubAllGlobals());

it("review: retains an unknown publication across routes/accounts and hides it from another actor", async () => {
  let writes = 0;
  fixture(async () => { writes++; throw new Error("synthetic lost response"); });
  render(<Harness/>); await connect(); await openRoles();
  await userEvent.click(screen.getByRole("button", { name: "Inspect review_role" })); await screen.findByRole("form", { name: "Publish role" });
  await userEvent.click(screen.getByLabelText("I reviewed the grants, scopes and impact on all assigned members")); await userEvent.click(screen.getByRole("button", { name: "Publish role version" })); await screen.findByText(/result is uncertain/);
  await userEvent.click(screen.getByRole("button", { name: "Navigate review route" })); await userEvent.click(screen.getByRole("button", { name: "Navigate review route" }));
  expect(await screen.findByRole("region", { name: "Pending administration changes" })).toHaveTextContent("review_role");
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" })); await connect("review-other");
  expect(screen.queryByRole("region", { name: "Pending administration changes" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" })); await connect();
  expect(await screen.findByRole("region", { name: "Pending administration changes" })).toHaveTextContent("review_role");
  await openRoles(); await userEvent.click(screen.getByRole("button", { name: "Inspect role review_role" })); await screen.findByRole("form", { name: "Publish role" });
  expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled(); expect(writes).toBe(1);
});

it("review: known self-assignment reloads live workspace authority and retains success when access denies", async () => {
  let reads = 0, writes = 0;
  fixture(async (_path, init) => { writes++; const body = JSON.parse(String(init.body)); return reply({ assigned: true, user_id: actor, ...body }); }, () => ++reads === 1 ? reply({ tenant_id: tenant, roles: ["owner"], decision: { allow: true, decision_id: "review-decision", policy_version: "review-policy" } }) : reply({}, 403));
  render(<Harness/>); await connect(); await openRoles();
  const assignment = within(screen.getByRole("region", { name: "Role assignment" }));
  await userEvent.type(assignment.getByLabelText("Assignment member ID"), actor); await userEvent.type(assignment.getByLabelText("Desired role name"), definition.name);
  await userEvent.click(assignment.getByRole("button", { name: "Inspect member and role" })); await assignment.findByText(/Current role owner/);
  await userEvent.click(assignment.getByLabelText(/I reviewed this assignment/)); await userEvent.click(assignment.getByRole("button", { name: "Assign role" }));
  await screen.findByText("Tenant access was denied or the service is unavailable.");
  expect(screen.getByText(/Your role assignment was saved/)).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Role administration" })).not.toBeInTheDocument();
  expect(reads).toBe(2); expect(writes).toBe(1);
});
