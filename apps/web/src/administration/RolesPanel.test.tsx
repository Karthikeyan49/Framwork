import { webcrypto, createHash } from "node:crypto";
import { useState } from "react";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { RolesPanel, roleScope } from "./RolesPanel";
import type { AdministrationIntent } from "./Recovery";
import { roleLimits, roleSchema, roleWireJSON } from "./roleContracts";
const tenant = "11111111-1111-1111-1111-111111111111", actor = "22222222-2222-2222-2222-222222222222", target = "33333333-3333-3333-3333-333333333333", operation = "44444444-4444-4444-4444-444444444444";
const definition = { schema_version: roleSchema, name: "finance_manager", grants: [] };
const original = { name: definition.name, version: 2, hash: "a".repeat(64), active: true, bootstrap: false, operation_id: operation, definition };
const resource = { app_id: "*", entity_id: "*", field_id: "*" };
const contract = (name: string) => ({ schema_version: roleSchema, name, root_protected: name === "owner", limits: roleLimits, actions: [{ name: "tenant.access", allow_supported: true, scope_required: false }, { name: "data.entity.read", allow_supported: true, scope_required: true }, { name: "roles.assign", allow_supported: name === "owner", scope_required: false }] });
const reply = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
const saved = (body: string) => { const b = JSON.parse(body); return { name: b.definition.name, version: b.expected_version + 1, hash: createHash("sha256").update(roleWireJSON(b.definition)).digest("hex"), active: b.active, bootstrap: false, operation_id: b.operation_id, definition: b.definition }; };
function fixture(post?: (path: string, body: string, options: RequestInit) => Promise<Response>, get?: (path: string) => Response | Promise<Response> | undefined) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    const path = String(url);
    if (options?.method === "POST") return post ? post(path, String(options.body), options) : reply(saved(String(options.body)));
    const custom = get?.(path); if (custom) return custom;
    if (path.includes("/role-contract?name=")) return reply(contract(path.split("=")[1]));
    if (path.endsWith("/roles")) return reply({ roles: [original] });
    if (path.includes("/members/")) return reply({ user_id: path.split("/").at(-1), role: "member", active: true });
    return reply(original);
  });
}
function Harness({ initial = {}, token = "synthetic", onSelf = () => {} }: { initial?: Record<string, AdministrationIntent>; token?: string; onSelf?: () => void }) {
  const [pending, setPending] = useState(initial), [open, setOpen] = useState(true), [inFlight, setInFlight] = useState<Record<string, boolean>>({});
  return <><button onClick={() => setOpen(!open)}>Toggle panel</button><output aria-label="Recovery journal">{JSON.stringify(pending)}</output>{open && <RolesPanel token={token} tenant={tenant} actor={actor} blocked={Object.values(inFlight).some(Boolean)} onSelfAssignment={onSelf} recovery={{ pending, remember: (scope, intent) => setPending((old) => { const next = { ...old }; if (intent) next[scope] = intent; else delete next[scope]; return next; }), busy: (key, value) => setInFlight((old) => ({ ...old, [key]: value })) }}/>}</>;
}
async function openRole() { await userEvent.click(await screen.findByRole("button", { name: "Inspect finance_manager" })); await screen.findByRole("form", { name: "Publish role" }); }
async function confirmPublish() { await userEvent.click(screen.getByLabelText("I reviewed the grants, scopes and impact on all assigned members")); await userEvent.click(screen.getByRole("button", { name: "Publish role version" })); }
async function inspectAssignment(user = target) { await userEvent.type(screen.getByLabelText("Assignment member ID"), user); await userEvent.type(screen.getByLabelText("Desired role name"), definition.name); await userEvent.click(screen.getByRole("button", { name: "Inspect member and role" })); await screen.findByText(/Current role member/); }
beforeEach(() => vi.stubGlobal("crypto", webcrypto)); afterEach(() => vi.unstubAllGlobals());
it("publishes explicit scope edits with the observed CAS and a bound receipt", async () => {
  const body: string[] = [];
  fixture(async (_path, value) => { body.push(value); return reply(saved(value)); });
  render(<Harness/>); await openRole();
  expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "Add grant" }));
  await userEvent.click(screen.getByText("Grant 1 capabilities (0 selected)"));
  await userEvent.click(screen.getByLabelText("data.entity.read (record scope)"));
  await userEvent.selectOptions(screen.getByLabelText("Grant 1 scope kind"), "OWNER");
  await confirmPublish(); await screen.findByText(/version 3 saved/);
  expect(JSON.parse(body[0])).toMatchObject({ expected_version: 2, active: true, definition: { grants: [{ effect: "ALLOW", actions: ["data.entity.read"], resource, scope: { kind: "OWNER" } }] } });
  expect(screen.getByLabelText("Recovery journal")).toHaveTextContent("{}"); expect(body).toHaveLength(1);
});
it("blocks invalid and lossy condition drafts without silently publishing the previous value", async () => {
  let writes = 0; fixture(async (_p, body) => { writes++; return reply(saved(body)); }); render(<Harness/>); await openRole();
  await userEvent.click(screen.getByRole("button", { name: "Add grant" })); await userEvent.click(screen.getByText("Grant 1 capabilities (0 selected)")); await userEvent.click(screen.getByLabelText("data.entity.read (record scope)"));
  await userEvent.selectOptions(screen.getByLabelText("Grant 1 scope kind"), "CONDITION"); await userEvent.type(screen.getByLabelText("Grant 1 scope field"), "field.invoice.amount");
  await userEvent.type(screen.getByLabelText("Grant 1 scope JSON value"), "9007199254740993"); await userEvent.click(screen.getByLabelText("I reviewed the grants, scopes and impact on all assigned members"));
  expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled(); expect(writes).toBe(0);
});
it("requires separate bootstrap acknowledgement and never edits owner authority", async () => {
  fixture(undefined, (path) => path.endsWith("/roles/admin") ? reply({ ...original, name: "admin", version: 1, bootstrap: true, definition: { ...definition, name: "admin" } }) : path.endsWith("/roles/owner") ? reply({ ...original, name: "owner", version: 1, bootstrap: true, definition: { ...definition, name: "owner" } }) : undefined);
  render(<Harness/>); await userEvent.type(screen.getByLabelText("Role name"), "admin"); await userEvent.click(screen.getByRole("button", { name: "Inspect or prepare role" }));
  await screen.findByText(/Native bootstrap role:/); await userEvent.click(screen.getByLabelText("I reviewed the grants, scopes and impact on all assigned members")); expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled();
  await userEvent.click(screen.getByLabelText("I understand this replaces native bootstrap permissions")); expect(screen.getByRole("button", { name: "Publish role version" })).toBeEnabled();
  await userEvent.clear(screen.getByLabelText("Role name")); await userEvent.type(screen.getByLabelText("Role name"), "owner"); await userEvent.click(screen.getByRole("button", { name: "Inspect or prepare role" })); await screen.findByText(/owner role is protected and read-only/); expect(screen.queryByRole("form", { name: "Publish role" })).not.toBeInTheDocument();
});
it("clears writable state after a rejected CAS without exposing response details or retrying", async () => {
  let writes = 0; fixture(async () => { writes++; return reply("PRIVATE", 409); }); render(<Harness/>); await openRole(); await confirmPublish();
  await screen.findByText(/state changed/); expect(screen.queryByRole("form", { name: "Publish role" })).not.toBeInTheDocument(); expect(screen.queryByText("PRIVATE")).not.toBeInTheDocument(); expect(writes).toBe(1);
  await userEvent.click(screen.getByRole("button", { name: "Reload selected role" })); await screen.findByRole("form", { name: "Publish role" }); expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled();
});
it("reconciles an unknown publication by immutable receipt after navigation and a later head", async () => {
  let receipt: ReturnType<typeof saved> | undefined; let writes = 0;
  fixture(async (_path, body) => { writes++; receipt = saved(body); throw new Error("PRIVATE"); }, (path) => path.includes("/roles/operations/") ? reply(receipt) : undefined);
  render(<Harness/>); await openRole(); await confirmPublish(); await screen.findByText(/result is uncertain/);
  const journal = screen.getByLabelText("Recovery journal"); expect(journal).toHaveTextContent('"expectedHash"'); expect(journal).not.toHaveTextContent('"grants"'); expect(journal).not.toHaveTextContent('synthetic');
  await userEvent.click(screen.getByRole("button", { name: "Toggle panel" })); await userEvent.click(screen.getByRole("button", { name: "Toggle panel" }));
  await userEvent.click(await screen.findByRole("button", { name: "Inspect role finance_manager" })); await screen.findByRole("form", { name: "Publish role" });
  expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled(); await userEvent.click(screen.getByRole("button", { name: "Check publication receipt" })); await screen.findByText(/Publication committed as version 3/); expect(journal).toHaveTextContent("{}"); expect(writes).toBe(1);
});
it("does not turn a denied or absent receipt into proof of rollback", async () => {
  const intent: AdministrationIntent = { scope: roleScope(tenant, definition.name), tenant, resource: definition.name, kind: "role.publish", status: "unknown", operationID: operation, expectedVersion: 2, expectedHash: original.hash, expectedActive: true };
  fixture(undefined, (path) => path.includes("/operations/") ? reply("PRIVATE", 404) : undefined); render(<Harness initial={{ [intent.scope]: intent }}/>);
  await userEvent.click(await screen.findByRole("button", { name: "Inspect role finance_manager" })); await screen.findByRole("form", { name: "Publish role" });
  await userEvent.click(screen.getByRole("button", { name: "Check publication receipt" })); await screen.findByText(/outcome remains unproven/); expect(screen.getByLabelText("Recovery journal")).toHaveTextContent(operation); expect(screen.getByRole("button", { name: "Use reviewed role state" })).toBeDisabled();
});
it("assigns using a fresh point membership and refreshes authority after self-assignment", async () => {
  const onSelf = vi.fn(), bodies: unknown[] = [];
  fixture(async (_path, body) => { const b = JSON.parse(body); bodies.push(b); return reply({ assigned: true, user_id: actor, ...b }); });
  render(<Harness onSelf={onSelf}/>); await inspectAssignment(actor); await screen.findByText(/changes your own role/);
  await userEvent.click(screen.getByLabelText(/I reviewed this assignment/)); await userEvent.click(screen.getByRole("button", { name: "Assign role" })); await screen.findByText(/Role assignment saved/);
  expect(bodies).toEqual([{ role: "finance_manager", expected_role: "member" }]); expect(onSelf).toHaveBeenCalledOnce();
});
it("blocks an unknown assignment until a fresh authorized point read is reviewed", async () => {
  let writes = 0; fixture(async () => { writes++; throw new Error("PRIVATE"); }); render(<Harness/>); await inspectAssignment();
  await userEvent.click(screen.getByLabelText(/I reviewed this assignment/)); await userEvent.click(screen.getByRole("button", { name: "Assign role" })); await screen.findByText(/result is uncertain/);
  expect(screen.queryByRole("button", { name: "Assign role" })).not.toBeInTheDocument(); await userEvent.click(screen.getByRole("button", { name: "Inspect member and role" })); await screen.findByRole("button", { name: "Use reviewed membership state" });
  expect(screen.getByRole("button", { name: "Assign role" })).toBeDisabled(); await userEvent.click(screen.getByRole("button", { name: "Use reviewed membership state" })); expect(screen.getByLabelText("Recovery journal")).toHaveTextContent("{}"); expect(writes).toBe(1);
});
it("ignores old-connection role reads even when the transport ignores abort", async () => {
  let release: ((response: Response) => void) | undefined;
  const transport = fixture(undefined, (path) => path.endsWith("/roles/finance_manager") ? new Promise<Response>((resolve) => { release = resolve; }) : undefined);
  const view = render(<Harness/>); await userEvent.click(await screen.findByRole("button", { name: "Inspect finance_manager" })); await waitFor(() => expect(release).toBeDefined());
  transport.mockImplementation(async (url) => String(url).includes("role-contract") ? reply(contract(definition.name)) : String(url).endsWith("/roles") ? reply({ roles: [original] }) : reply("denied", 403));
  view.rerender(<Harness token="new-synthetic"/>); await waitFor(() => expect(within(screen.getByRole("region", { name: "Selected role" })).getByRole("alert")).toHaveTextContent("permissions"));
  await act(async () => release?.(reply(original))); expect(screen.queryByRole("form", { name: "Publish role" })).not.toBeInTheDocument();
});
it("keeps absent-role creation inactive and refuses to interpret a denied read as absence", async () => {
  fixture(undefined, (path) => path.endsWith("/roles/new_role") ? reply("missing", 404) : undefined); render(<Harness/>); await userEvent.type(screen.getByLabelText("Role name"), "new_role"); await userEvent.click(screen.getByRole("button", { name: "Inspect or prepare role" })); await screen.findByText(/expected version 0/); expect(screen.getByLabelText("Role active")).not.toBeChecked();
});

it("can publish again after an explicit reload without treating read cancellation as panel unmount", async () => {
  let head = { ...original }; let writes = 0;
  fixture(async (_path, body) => { writes++; head = saved(body); return reply(head); }, (path) => path.endsWith("/roles/finance_manager") ? reply(head) : undefined);
  render(<Harness/>); await openRole(); await confirmPublish(); await screen.findByText(/version 3 saved/);
  await userEvent.click(screen.getByRole("button", { name: "Reload selected role" })); await screen.findByText(/Observed version 3/);
  await userEvent.click(screen.getByLabelText("Role active")); await confirmPublish(); await screen.findByText(/version 4 saved/); expect(writes).toBe(2);
});
