import { useState } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { previewClient } from "../api/preview";
import { RecordAccessPanel } from "./RecordAccessPanel";
import type { AdministrationIntent } from "./Recovery";
const tenant = "11111111-1111-1111-1111-111111111111";
const record = "22222222-2222-2222-2222-222222222222";
const owner = "33333333-3333-3333-3333-333333333333";
const user = "44444444-4444-4444-4444-444444444444";
const reply = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
const access = { record_id: record, version: 7, owner_user_id: owner, shares: [{ user_id: user, can_write: false }] };
const entity = { id: "entity.contact", label: "Contact", fields: [{ id: "field.contact.id", label: "ID", type: "string", required: true, core_id: true }] };
const client = () => previewClient("/api", "synthetic", tenant, "app.contacts", 1);
const invalidated = vi.fn();
function Harness({ mode = "shares", initial = {} }: { mode?: "owner" | "shares"; initial?: Record<string, AdministrationIntent> }) {
  const [pending, setPending] = useState(initial); const [c] = useState(client);
  return <RecordAccessPanel key={mode} client={c} token="synthetic" tenant={tenant} entity={entity} record={record} scope="scope" mode={mode} onBusy={() => {}} onInvalidated={invalidated} onClose={() => {}} recovery={{ pending, busy: () => {}, remember: (scope, value) => setPending((old) => { const next = { ...old }; if (value) next[scope] = value; else delete next[scope]; return next; }) }} />;
}
it("assign-only ownership uses record headers, requires acknowledgement, and reports saved before a later read denial", async () => {
  let saved = false; const calls: string[] = []; let body: unknown;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    calls.push(String(url));
    if (options?.method === "POST" && String(url).endsWith("/access")) {
      body = JSON.parse(String(options.body)); saved = true; return reply({ ...access, shares: undefined, version: 8, owner_user_id: user });
    }
    if (saved) return reply("private denial", 403);
    return reply({ records: [{ id: record, version: 7, owner_user_id: owner, values: {} }], next: "" });
  });
  render(<Harness mode="owner" />);
  await userEvent.clear(await screen.findByLabelText("Owner user ID")); await userEvent.type(screen.getByLabelText("Owner user ID"), user);
  expect(screen.getByRole("button", { name: "Transfer ownership" })).toBeDisabled();
  await userEvent.click(screen.getByRole("checkbox")); await userEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
  expect(await screen.findByText("Access change saved · version 8.")).toBeInTheDocument();
  expect(await screen.findByRole("alert")).toHaveTextContent("permissions");
  expect(screen.queryByRole("form", { name: "Change record ownership" })).not.toBeInTheDocument();
  expect(body).toEqual({ operation: "owner", expected_version: 7, user_id: user, team_id: "" });
  expect(calls.filter((p) => p.endsWith("/access"))).toHaveLength(1);
  expect(invalidated).toHaveBeenCalled();
});
it("does not expose ownership/shares when the sharing read is denied", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(reply("private share facts", 403));
  render(<Harness />); expect(await screen.findByRole("alert")).toHaveTextContent("permissions");
  expect(screen.queryByRole("form", { name: "Grant record share" })).not.toBeInTheDocument();
  expect(screen.queryByText(owner)).not.toBeInTheDocument(); expect(screen.queryByText("private share facts")).not.toBeInTheDocument();
});
it("displays truncated shares honestly and permits manual-ID revocation at the observed version", async () => {
  let count = 0; let body: unknown;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, options) => {
    if (options?.method === "POST") { count++; body = JSON.parse(String(options.body)); return reply({ ...access, shares: undefined, version: 8 }); }
    return reply({ ...access, version: count ? 8 : 7, shares_truncated: true });
  });
  render(<Harness />); await screen.findByText(/Only the first 256 shares/);
  await userEvent.type(screen.getByLabelText("Recipient user ID"), tenant);
  await userEvent.click(screen.getByRole("button", { name: "Revoke by user ID" }));
  await screen.findByText("Access change saved · version 8.");
  expect(body).toEqual({ operation: "unshare", expected_version: 7, user_id: tenant }); expect(count).toBe(1);
});
it("keeps an unknown share gated through failed reloads and clears it only after explicit fresh review", async () => {
  let writes = 0; let denied = false;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, options) => {
    if (options?.method === "POST") { writes++; denied = true; throw new TypeError("private socket error"); }
    return denied ? reply("private driver error", 403) : reply(access);
  });
  render(<Harness />); await userEvent.type(await screen.findByLabelText("Recipient user ID"), owner);
  await userEvent.click(screen.getByRole("button", { name: "Save share" }));
  await screen.findByText(/An access change has an uncertain outcome/);
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "Reload record access" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Reload record access" })).toBeEnabled());
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeDisabled();
  denied = false;
  await userEvent.click(screen.getByRole("button", { name: "Reload record access" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeEnabled());
  expect(screen.getByRole("button", { name: "Save share" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "Use reviewed record state" }));
  expect(screen.queryByText(/An access change has an uncertain outcome/)).not.toBeInTheDocument(); expect(writes).toBe(1);
});
it("cannot clear an unknown share by reviewing only ownership headers", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(reply({ records: [{ id: record, version: 7, owner_user_id: owner, values: {} }], next: "" }));
  render(<Harness mode="owner" initial={{ scope: { scope: "scope", kind: "record.share", tenant, resource: record, target: user } }} />);
  await screen.findByLabelText("Owner user ID");
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeDisabled();
  expect(screen.getByText(/Open record sharing to review/)).toBeInTheDocument();
});
