import { useState } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { RecordAccessPanel } from "./RecordAccessPanel";
import { TeamsPanel } from "./TeamsPanel";
import type { AdministrationIntent } from "./Recovery";
import { AdministrationOutcomeUnknown, administrationClient } from "../api/administration";
import { previewClient } from "../api/preview";

const tenant = "11111111-1111-1111-1111-111111111111";
const record = "22222222-2222-2222-2222-222222222222";
const owner = "33333333-3333-3333-3333-333333333333";
const target = "44444444-4444-4444-4444-444444444444";
const team = "55555555-5555-5555-5555-555555555555";
const response = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status });
const entity = { id: "entity.contact", label: "Contact", fields: [{ id: "field.contact.id", label: "ID", type: "string", required: true, core_id: true }] };
const access = { record_id: record, version: 8, owner_user_id: owner, shares: [] };
const client = () => previewClient("/api", "synthetic-token", tenant, "app.contacts", 1);

function RecordHarness({ initial = {}, mode = "shares" }: { initial?: Record<string, AdministrationIntent>; mode?: "owner" | "shares" }) {
  const [pending, setPending] = useState(initial);
  const [c] = useState(client);
  return <RecordAccessPanel client={c} token="synthetic-token" tenant={tenant} entity={entity} record={record} scope="scope" mode={mode}
    onBusy={() => {}} onInvalidated={() => {}} onClose={() => {}} recovery={{ pending, busy: () => {}, remember: (scope, value) => setPending((old) => { const next = { ...old }; if (value) next[scope] = value; else delete next[scope]; return next; }) }} />;
}

it("independent: an unlisted recipient in a truncated read cannot release an unknown share/unshare", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ ...access, shares: [{ user_id: owner, can_write: false }], shares_truncated: true }));
  render(<RecordHarness initial={{ scope: { status: "unknown", scope: "scope", kind: "record.unshare", tenant, resource: record, target } }} />);
  await screen.findByText(/Only the first 256 shares/);
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeDisabled();
});

it("independent: a displayed recipient in a truncated read can be reviewed without inferring absence", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ ...access, shares: [{ user_id: target, can_write: false }], shares_truncated: true }));
  render(<RecordHarness initial={{ scope: { status: "unknown", scope: "scope", kind: "record.share", tenant, resource: record, target } }} />);
  await screen.findByText(/Only the first 256 shares/);
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeEnabled();
  expect(screen.getByText(target, { selector: "code" })).toBeInTheDocument();
});

it("independent: response mismatches and rejected transport never turn a dispatched membership into success", async () => {
  for (const reply of [{ user_id: owner, active: false }, { user_id: target, active: true }]) {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(response(reply));
    await expect(administrationClient("/api", "synthetic", tenant, transport).setMember(team, target, false, true)).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
    expect(transport).toHaveBeenCalledTimes(1);
  }
});

it("independent: a mismatching record owner witness is uncertain and no second POST is sent", async () => {
  const transport = vi.fn<typeof fetch>().mockResolvedValue(response({ ...access, version: 9, owner_user_id: owner }));
  await expect(previewClient("/api", "synthetic", tenant, "app.contacts", 1, transport).changeAccess(entity.id, record,
    { operation: "owner", expected_version: 8, user_id: target, team_id: team })).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
  expect(transport).toHaveBeenCalledTimes(1);
});

it("independent: record reloads serialize and replace the snapshot only after a completed read", async () => {
  let resolveOld: ((r: Response) => void) | undefined;
  let reads = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
    reads++;
    if (reads === 1) return response(access);
    if (reads === 2) return new Promise<Response>((resolve) => { resolveOld = resolve; });
    return response({ ...access, version: 10 });
  });
  render(<RecordHarness />);
  await screen.findByText(/Version 8/);
  // Reload is disabled while in flight. A mode remount is the public cancellation boundary;
  // direct component state cannot dispatch a concurrent second reload through the UI.
  await userEvent.click(screen.getByRole("button", { name: "Reload record access" }));
  expect(screen.getByRole("button", { name: "Reload record access" })).toBeDisabled();
  await act(async () => resolveOld?.(response({ ...access, version: 9 })));
  await screen.findByText(/Version 9/);
  await userEvent.click(screen.getByRole("button", { name: "Reload record access" }));
  await screen.findByText(/Version 10/);
});

it("independent: abort after dispatch retains unknown intent without leaking response text or retrying", async () => {
  let rejected: (() => void) | undefined;
  let writes = 0;
  const remembered = vi.fn();
  const busy = vi.fn();
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, options) => {
    if (options?.method !== "POST") return response(access);
    writes++;
    return new Promise<Response>((_resolve, reject) => {
      rejected = () => reject(new Error("PRIVATE transport text"));
      options.signal?.addEventListener("abort", rejected);
    });
  });
  const c = client();
  const view = render(<RecordAccessPanel client={c} token="synthetic-token" tenant={tenant} entity={entity} record={record} scope="scope" mode="shares"
    onBusy={() => {}} onInvalidated={() => {}} onClose={() => {}} recovery={{ pending: {}, remember: remembered, busy }} />);
  await userEvent.type(await screen.findByLabelText("Recipient user ID"), target);
  await userEvent.click(screen.getByRole("button", { name: "Save share" }));
  await waitFor(() => expect(writes).toBe(1));
  view.unmount();
  await act(async () => rejected?.());
  await waitFor(() => expect(remembered).toHaveBeenLastCalledWith("scope", expect.objectContaining({ status: "unknown", target })));
  expect(remembered.mock.calls.flat().join(" ")).not.toContain("PRIVATE");
  expect(writes).toBe(1);
  expect(busy).toHaveBeenLastCalledWith(expect.any(String), false);
});

it("independent: a late exact successful witness after unmount releases only its own intent", async () => {
  let resolvePost: ((r: Response) => void) | undefined;
  const remembered = vi.fn();
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, options) => {
    if (options?.method !== "POST") return response(access);
    return new Promise<Response>((resolve) => { resolvePost = resolve; });
  });
  const c = client();
  const view = render(<RecordAccessPanel client={c} token="synthetic-token" tenant={tenant} entity={entity} record={record} scope="scope" mode="shares"
    onBusy={() => {}} onInvalidated={() => {}} onClose={() => {}} recovery={{ pending: {}, remember: remembered, busy: () => {} }} />);
  await userEvent.type(await screen.findByLabelText("Recipient user ID"), target);
  await userEvent.click(screen.getByRole("button", { name: "Save share" }));
  view.unmount();
  await act(async () => resolvePost?.(response({ ...access, version: 9 })));
  await waitFor(() => expect(remembered).toHaveBeenLastCalledWith("scope"));
  expect(remembered).toHaveBeenCalledTimes(2);
});

it("independent: malformed 200 team detail never offers editable controls or clears pending state", async () => {
  const pending = { scope: `team:${tenant}:${team}`, status: "unknown" as const, tenant, resource: team, kind: "team.member" as const, target };
  const remember = vi.fn();
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => response(String(url).endsWith("/teams") ? { teams: [{ id: team, name: "Finance", version: 1 }] } : { id: team, name: "Finance", version: 1, members: [{ user_id: target, active: true }, { user_id: target, active: false }] }));
  render(<TeamsPanel token="synthetic" tenant={tenant} recovery={{ pending: { [pending.scope]: pending }, remember, busy: () => {} }} />);
  await userEvent.click(await screen.findByRole("button", { name: `Inspect team ${team}` }));
  expect(await screen.findByRole("alert")).toHaveTextContent("could not be verified");
  expect(screen.queryByRole("button", { name: "Use reviewed team state" })).not.toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Edit team" })).not.toBeInTheDocument();
  expect(remember).not.toHaveBeenCalled();
});

it.each(["record.share", "record.unshare"] as const)("independent: a complete list proves current absence for explicit review of %s", async (kind) => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ ...access, shares: [], shares_truncated: false }));
  render(<RecordHarness initial={{ scope: { status: "unknown", scope: "scope", kind, tenant, resource: record, target } }} />);
  await screen.findByText("No explicit shares.");
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeEnabled();
  await userEvent.click(screen.getByRole("button", { name: "Use reviewed record state" }));
  expect(screen.getByText(/earlier request's outcome is not proven/)).toBeInTheDocument();
  expect(screen.queryByText(/An access change has an uncertain outcome/)).not.toBeInTheDocument();
});

it("independent: an unknown share absent from a truncated window remains blocked with a clear explanation", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ ...access, shares: [], shares_truncated: true }));
  render(<RecordHarness initial={{ scope: { status: "unknown", scope: "scope", kind: "record.share", tenant, resource: record, target } }} />);
  await screen.findByText(/pending recipient is outside this share window/);
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeDisabled();
});

it("independent: even a complete read cannot dismiss an intent with no valid target", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ ...access, shares: [], shares_truncated: false }));
  render(<RecordHarness initial={{ scope: { status: "unknown", scope: "scope", kind: "record.share", tenant, resource: record } }} />);
  await screen.findByText("No explicit shares.");
  expect(screen.getByRole("button", { name: "Use reviewed record state" })).toBeDisabled();
});
