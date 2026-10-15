import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { App } from "../App";
const tenant = "11111111-1111-1111-1111-111111111111";
const otherTenant = "22222222-2222-2222-2222-222222222222";
const actorA = "33333333-3333-3333-3333-333333333333";
const actorB = "44444444-4444-4444-4444-444444444444";
const reply = (v: unknown) => new Response(JSON.stringify(v));
async function connect() {
  await userEvent.type(await screen.findByLabelText("API access token"), "synthetic-private-token");
  await userEvent.click(screen.getByRole("button", { name: "Connect" }));
  await screen.findByLabelText("Workspace");
}
async function workspace(id = tenant) {
  await userEvent.selectOptions(screen.getByLabelText("Workspace"), id);
  await screen.findByText("Access verified · owner");
}
it("retains only actor-owned administration identities through route, account and workspace changes", async () => {
  const errors = vi.spyOn(console, "error");
  let actor = actorA; let creation = ""; let writes = 0; let committed = false;
  const storage = vi.spyOn(Storage.prototype, "setItem");
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    const path = String(url);
    if (path.endsWith("/auth/provider")) return new Response(JSON.stringify({ enabled: false, manual_token: true }));
    if (path.endsWith("/session")) return reply({ user: { id: actor, name: "Connected user" }, tenants: [{ id: tenant, name: "A" }, { id: otherTenant, name: "B" }] });
    if (path.endsWith("/access")) return reply({ tenant_id: path.includes(otherTenant) ? otherTenant : tenant, roles: ["owner"], decision: { allow: true, decision_id: "decision", policy_version: "native" } });
    if (options?.method === "POST") {
      writes++; creation = JSON.parse(String(options.body)).id; committed = true;
      return new Promise<Response>((_resolve, reject) => options?.signal?.addEventListener("abort", () => reject(new DOMException("cancelled", "AbortError"))));
    }
    if (creation && path.endsWith(creation)) return reply({ id: creation, name: "Server team", version: 1, members: [] });
    return reply({ teams: [] });
  });
  render(<MemoryRouter initialEntries={["/access"]}><App /></MemoryRouter>);
  await connect(); await workspace();
  await userEvent.click(screen.getByRole("button", { name: "Manage teams" }));
  await userEvent.type(await screen.findByLabelText("New team name"), "Private team draft");
  await userEvent.click(screen.getByRole("button", { name: "Create team" }));
  await waitFor(() => expect(writes).toBe(1));
  expect(screen.getByLabelText("Workspace")).toBeDisabled();
  const pending = screen.getByRole("region", { name: "Pending administration changes" });
  expect(pending).toHaveTextContent(creation); expect(pending).not.toHaveTextContent("Private team draft");
  await userEvent.click(screen.getByRole("link", { name: "Overview" }));
  await userEvent.click(screen.getByRole("link", { name: "Workspace access" }));
  expect(screen.getByRole("region", { name: "Pending administration changes" })).toHaveTextContent(creation);
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  actor = actorB; await connect();
  expect(screen.queryByRole("region", { name: "Pending administration changes" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  actor = actorA; await connect();
  expect(screen.getByRole("region", { name: "Pending administration changes" })).toHaveTextContent(creation);
  expect(screen.queryByDisplayValue("Private team draft")).not.toBeInTheDocument();
  await workspace(otherTenant); await userEvent.click(screen.getByRole("button", { name: "Manage teams" }));
  await screen.findByText("No teams yet.");
  expect(screen.queryByRole("region", { name: "Pending team changes" })).not.toBeInTheDocument();
  await workspace();
  const teamPending = await screen.findByRole("region", { name: "Pending team changes" });
  await userEvent.click(within(teamPending).getByRole("button", { name: `Inspect team ${creation}` }));
  await screen.findByRole("heading", { name: "Server team" });
  await userEvent.click(screen.getByRole("button", { name: "Use reviewed team state" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Pending administration changes" })).not.toBeInTheDocument());
  expect(committed).toBe(true); expect(writes).toBe(1); expect(storage).not.toHaveBeenCalled();
  expect(errors.mock.calls.filter((args) => args.some((value) => String(value).includes("same key")))).toHaveLength(0);
});
