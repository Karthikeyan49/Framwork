import { createHash, webcrypto } from "node:crypto";
import { useState } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { RolesPanel, roleScope } from "./RolesPanel";
import type { AdministrationIntent } from "./Recovery";
import { roleLimits, roleSchema, roleWireJSON } from "./roleContracts";

const tenant = "11111111-1111-1111-1111-111111111111";
const actor = "22222222-2222-2222-2222-222222222222";
const operation = "33333333-3333-3333-3333-333333333333";
const definition = { schema_version: roleSchema, name: "review_role", grants: [] };
const hash = createHash("sha256").update(roleWireJSON(definition)).digest("hex");
const role = { name: definition.name, version: 2, hash, active: true, bootstrap: false, operation_id: operation, definition };
const contract = { schema_version: roleSchema, name: definition.name, root_protected: false, limits: roleLimits, actions: [{ name: "tenant.access", allow_supported: true, scope_required: false }] };
const reply = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });

function Harness({ initial = {}, token = "review-connection" }: { initial?: Record<string, AdministrationIntent>; token?: string }) {
  const [pending, setPending] = useState(initial), [open, setOpen] = useState(true);
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  return <><button onClick={() => setOpen(!open)}>Toggle review panel</button><output aria-label="Review journal">{JSON.stringify(pending)}</output><output aria-label="Review busy">{String(Object.values(busy).some(Boolean))}</output>{open && <RolesPanel token={token} tenant={tenant} actor={actor} blocked={Object.values(busy).some(Boolean)} onSelfAssignment={() => {}} recovery={{ pending, remember: (scope, intent) => setPending((old) => { const next = { ...old }; if (intent) next[scope] = intent; else delete next[scope]; return next; }), busy: (key, value) => setBusy((old) => ({ ...old, [key]: value })) }}/>}</>;
}
function transport(custom?: (path: string, init?: RequestInit) => Response | Promise<Response> | undefined) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
    const path = String(url), answer = custom?.(path, init);
    if (answer) return answer;
    if (path.includes("role-contract")) return reply(contract);
    if (path.endsWith("/roles")) return reply({ roles: [role] });
    return reply(role);
  });
}
async function selectRole() { await userEvent.click(await screen.findByRole("button", { name: "Inspect review_role" })); await screen.findByRole("form", { name: "Publish role" }); }
async function publish() { await userEvent.click(screen.getByLabelText("I reviewed the grants, scopes and impact on all assigned members")); await userEvent.click(screen.getByRole("button", { name: "Publish role version" })); }
function saved(body: string) {
  const request = JSON.parse(body);
  return { ...role, definition: request.definition, active: request.active, hash: createHash("sha256").update(roleWireJSON(request.definition)).digest("hex"), version: request.expected_version + 1, operation_id: request.operation_id };
}
beforeEach(() => vi.stubGlobal("crypto", webcrypto));
afterEach(() => vi.unstubAllGlobals());

it("review: does not accept a changed receipt definition under unchanged hash witnesses", async () => {
  const intent: AdministrationIntent = { scope: roleScope(tenant, definition.name), tenant, resource: definition.name, kind: "role.publish", status: "unknown", operationID: operation, expectedVersion: 1, expectedHash: hash, expectedActive: true };
  const changed = { ...role, definition: { ...definition, grants: [{ effect: "ALLOW", actions: ["tenant.access"], resource: { app_id: "*", entity_id: "*", field_id: "*" } }] } };
  transport((path) => path.includes("/operations/") ? reply(changed) : undefined);
  render(<Harness initial={{ [intent.scope]: intent }}/>); await selectRole();
  await userEvent.click(screen.getByRole("button", { name: "Check publication receipt" }));
  await screen.findByText(/receipt could not be verified/);
  expect(screen.getByLabelText("Review journal")).toHaveTextContent(operation);
  expect(screen.queryByText(/Publication committed as version/)).not.toBeInTheDocument();
});

it("review: a receipt hash finishing after unmount cannot clear its pending intent", async () => {
  const intent: AdministrationIntent = { scope: roleScope(tenant, definition.name), tenant, resource: definition.name, kind: "role.publish", status: "unknown", operationID: operation, expectedVersion: 1, expectedHash: hash, expectedActive: true };
  let release: ((v: ArrayBuffer) => void) | undefined;
  const digest = vi.spyOn(webcrypto.subtle, "digest").mockImplementation(() => new Promise<ArrayBuffer>((resolve) => { release = resolve; }));
  transport((path) => path.includes("/operations/") ? reply(role) : undefined);
  render(<Harness initial={{ [intent.scope]: intent }}/>); await selectRole();
  await userEvent.click(screen.getByRole("button", { name: "Check publication receipt" })); await waitFor(() => expect(release).toBeDefined());
  await userEvent.click(screen.getByRole("button", { name: "Toggle review panel" }));
  const bytes = new Uint8Array(createHash("sha256").update(roleWireJSON(definition)).digest());
  await act(async () => release?.(bytes.buffer));
  expect(screen.getByLabelText("Review journal")).toHaveTextContent(operation);
  expect(screen.queryByText(/Publication committed as version/)).not.toBeInTheDocument();
  digest.mockRestore();
  await userEvent.click(screen.getByRole("button", { name: "Toggle review panel" })); await selectRole();
  await userEvent.click(screen.getByRole("button", { name: "Check publication receipt" })); await screen.findByText(/Publication committed as version 2/);
  expect(screen.getByLabelText("Review journal")).toHaveTextContent("{}");
});

it("review: cancels asynchronous hash preparation on panel unmount before dispatch", async () => {
  let release: ((v: ArrayBuffer) => void) | undefined;
  const digest = vi.spyOn(webcrypto.subtle, "digest").mockImplementation(() => new Promise<ArrayBuffer>((resolve) => { release = resolve; }));
  const fetch = transport(); render(<Harness/>); await selectRole(); await publish();
  await waitFor(() => expect(release).toBeDefined());
  await userEvent.click(screen.getByRole("button", { name: "Toggle review panel" }));
  await act(async () => release?.(new Uint8Array(32).buffer));
  await waitFor(() => expect(screen.getByLabelText("Review busy")).toHaveTextContent("false"));
  expect(screen.getByLabelText("Review journal")).toHaveTextContent("{}");
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(0);
  digest.mockRestore();
});

it("review: preserves an aborted dispatched publication and never retries it on remount", async () => {
  let rejectPost: ((e: Error) => void) | undefined; let writes = 0;
  transport((_path, init) => init?.method === "POST" ? new Promise<Response>((_resolve, reject) => { writes++; rejectPost = reject; }) : undefined);
  render(<Harness/>); await selectRole(); await publish(); await waitFor(() => expect(writes).toBe(1));
  await userEvent.click(screen.getByRole("button", { name: "Toggle review panel" }));
  await act(async () => rejectPost?.(new Error("synthetic transport abort")));
  expect(screen.getByLabelText("Review journal")).toHaveTextContent('"status":"unknown"');
  await userEvent.click(screen.getByRole("button", { name: "Toggle review panel" }));
  await selectRole(); expect(screen.getByRole("button", { name: "Publish role version" })).toBeDisabled();
  expect(writes).toBe(1);
});

it("review: aborts old preparation when the connection changes", async () => {
  let release: ((v: ArrayBuffer) => void) | undefined;
  const digest = vi.spyOn(webcrypto.subtle, "digest").mockImplementation(() => new Promise<ArrayBuffer>((resolve) => { release = resolve; }));
  const fetch = transport(); const view = render(<Harness/>); await selectRole(); await publish();
  await waitFor(() => expect(release).toBeDefined()); view.rerender(<Harness token="review-new-connection"/>);
  await act(async () => release?.(new Uint8Array(32).buffer));
  await waitFor(() => expect(screen.getByLabelText("Review busy")).toHaveTextContent("false"));
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(0);
  expect(screen.getByLabelText("Review journal")).toHaveTextContent("{}"); digest.mockRestore();
  await screen.findByRole("form", { name: "Publish role" });
  expect(screen.getByLabelText("Role active")).toBeEnabled();
  expect(screen.getByRole("button", { name: "Publish role version" })).toBeInTheDocument();
});

it("review: a new connection can publish before the old digest settles", async () => {
  let releaseOld: ((v: ArrayBuffer) => void) | undefined;
  const actualDigest = webcrypto.subtle.digest.bind(webcrypto.subtle);
  const digest = vi.spyOn(webcrypto.subtle, "digest").mockImplementation(actualDigest).mockImplementationOnce(() => new Promise<ArrayBuffer>((resolve) => { releaseOld = resolve; }));
  const fetch = transport((_path, init) => init?.method === "POST" ? reply(saved(String(init.body))) : undefined);
  const view = render(<Harness/>); await selectRole(); await publish(); await waitFor(() => expect(releaseOld).toBeDefined());
  view.rerender(<Harness token="review-new-connection"/>);
  await waitFor(() => expect(screen.getByLabelText("Role active")).toBeEnabled());
  expect(screen.getByLabelText("Review busy")).toHaveTextContent("false");
  await publish(); await screen.findByText(/version 3 saved/);
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  await act(async () => releaseOld?.(new Uint8Array(32).buffer));
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  expect(screen.getByLabelText("Review journal")).toHaveTextContent("{}");
  expect(screen.getByLabelText("Review busy")).toHaveTextContent("false");
  const posted = fetch.mock.calls.find(([, init]) => init?.method === "POST");
  expect(new Headers(posted?.[1]?.headers).get("Authorization")).toBe("Bearer review-new-connection");
  digest.mockRestore();
});

it("review: late old digest completion cannot release a newer preparation", async () => {
  const releases: ((v: ArrayBuffer) => void)[] = [];
  const digest = vi.spyOn(webcrypto.subtle, "digest").mockImplementation(() => new Promise<ArrayBuffer>((resolve) => { releases.push(resolve); }));
  const fetch = transport((_path, init) => init?.method === "POST" ? reply(saved(String(init.body))) : undefined);
  const view = render(<Harness/>); await selectRole(); await publish(); await waitFor(() => expect(releases).toHaveLength(1));
  view.rerender(<Harness token="review-new-connection"/>);
  await waitFor(() => expect(screen.getByLabelText("Role active")).toBeEnabled());
  await publish(); await waitFor(() => expect(releases).toHaveLength(2));
  await act(async () => releases[0](new Uint8Array(32).buffer));
  expect(screen.getByLabelText("Review busy")).toHaveTextContent("true");
  expect(screen.getByRole("button", { name: "Publishing…" })).toBeDisabled();
  expect(screen.getByLabelText("Role active")).toBeDisabled();
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(0);
  const bytes = new Uint8Array(createHash("sha256").update(roleWireJSON(definition)).digest());
  await act(async () => releases[1](bytes.buffer));
  await screen.findByText(/version 3 saved/);
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  expect(screen.getByLabelText("Review journal")).toHaveTextContent("{}");
  expect(screen.getByLabelText("Review busy")).toHaveTextContent("false");
  digest.mockRestore();
});

it("review: a forbidden current read never enables implicit creation", async () => {
  transport((path) => path.endsWith("/roles/review_role") ? reply({}, 403) : undefined);
  render(<Harness/>); await userEvent.click(await screen.findByRole("button", { name: "Inspect review_role" }));
  await screen.findByText("Your permissions do not allow this operation.");
  expect(screen.queryByRole("form", { name: "Publish role" })).not.toBeInTheDocument();
  expect(screen.queryByText(/expected version 0/)).not.toBeInTheDocument();
});
