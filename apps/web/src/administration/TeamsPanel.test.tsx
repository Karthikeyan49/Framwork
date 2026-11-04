import { useState } from "react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { TeamsPanel } from "./TeamsPanel";
import type { AdministrationIntent } from "./Recovery";
const tenant = "11111111-1111-1111-1111-111111111111";
const id = "22222222-2222-2222-2222-222222222222";
const user = "33333333-3333-3333-3333-333333333333";
const other = "44444444-4444-4444-4444-444444444444";
const reply = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
const original = { id, name: "Finance", version: 3, members: [{ user_id: user, active: false }] };
function Harness({ workspace = tenant }: { workspace?: string }) {
  const [pending, setPending] = useState<Record<string, AdministrationIntent>>({});
  return <TeamsPanel key={workspace} token="synthetic" tenant={workspace} recovery={{ pending, remember: (scope, value) => setPending((old) => { const next = { ...old }; if (value) next[scope] = value; else delete next[scope]; return next; }), busy: () => {} }} />;
}
async function open() { await userEvent.click(await screen.findByRole("button", { name: "Manage Finance" })); await screen.findByRole("form", { name: "Edit team" }); }
it("uses exact team versions and false/null/true membership witnesses from fresh reads", async () => {
  let current = structuredClone(original); const bodies: Record<string, unknown>[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    if (options?.method === "POST") {
      const body = JSON.parse(String(options.body)); bodies.push(body);
      if (String(url).includes("/members/")) {
        const target = String(url).split("/").at(-1)!;
        current.members = [...current.members.filter((m) => m.user_id !== target), { user_id: target, active: body.active }];
        return reply({ user_id: target, active: body.active });
      }
      current = { ...current, name: body.name, version: body.expected_version + 1 };
      return reply(current);
    }
    return reply(String(url).endsWith("/teams") ? { teams: [current] } : current);
  });
  render(<Harness />); await open();
  await userEvent.clear(screen.getByLabelText("Team name")); await userEvent.type(screen.getByLabelText("Team name"), "Accounts");
  await userEvent.click(screen.getByRole("button", { name: "Save team" }));
  await screen.findByRole("heading", { name: "Accounts" });
  expect(bodies[0]).toMatchObject({ id, name: "Accounts", expected_version: 3, parent_id: "" });
  await userEvent.click(screen.getByRole("button", { name: `Activate member ${user}` }));
  await screen.findByRole("button", { name: `Deactivate member ${user}` });
  expect(bodies[1]).toEqual({ active: true, expected_active: false });
  await userEvent.click(screen.getByRole("button", { name: `Deactivate member ${user}` }));
  await screen.findByRole("button", { name: `Activate member ${user}` });
  expect(bodies[2]).toEqual({ active: false, expected_active: true });
  await userEvent.type(screen.getByLabelText("Member user ID"), other);
  await userEvent.click(screen.getByRole("button", { name: "Save membership" }));
  await screen.findByRole("button", { name: `Deactivate member ${other}` });
  expect(bodies[3]).toEqual({ active: true, expected_active: null });
});
it("clears stale edit data on conflict and requires an explicit reload", async () => {
  let version = 3; let writes = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    if (options?.method === "POST") { writes++; version = 4; return reply("private conflict details", 409); }
    const t = { ...original, version, name: version === 4 ? "Changed elsewhere" : "Finance" };
    return reply(String(url).endsWith("/teams") ? { teams: [t] } : t);
  });
  render(<Harness />); await open();
  await userEvent.type(screen.getByLabelText("Team name"), " edit");
  await userEvent.click(screen.getByRole("button", { name: "Save team" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("state changed");
  expect(screen.queryByRole("form", { name: "Edit team" })).not.toBeInTheDocument();
  expect(screen.queryByText("private conflict details")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Reload selected team" }));
  expect(await screen.findByLabelText("Team name")).toHaveValue("Changed elsewhere"); expect(writes).toBe(1);
});
it("preserves an unknown creation UUID, blocks another create and requires a fresh reviewed state", async () => {
  let created = ""; let name = ""; let writes = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    if (options?.method === "POST") { writes++; const body = JSON.parse(String(options.body)); created = body.id; name = body.name; throw new TypeError("private connection error"); }
    return reply(String(url).endsWith("/teams") ? { teams: [] } : { id: created, name, version: 1, members: [] });
  });
  render(<Harness />);
  await userEvent.type(await screen.findByLabelText("New team name"), "New finance");
  await userEvent.click(screen.getByRole("button", { name: "Create team" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("result is uncertain");
  const pending = screen.getByRole("region", { name: "Pending team changes" });
  expect(pending).toHaveTextContent(created); expect(pending).not.toHaveTextContent("New finance");
  await userEvent.click(screen.getByRole("button", { name: "Reload team list" }));
  expect(await screen.findByRole("button", { name: "Create team" })).toBeDisabled();
  await userEvent.click(within(pending).getByRole("button", { name: `Inspect team ${created}` }));
  expect(await screen.findByRole("button", { name: "Save team" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "Use reviewed team state" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Pending team changes" })).not.toBeInTheDocument()); expect(writes).toBe(1);
});
it("does not disclose team data or write controls after read denial", async () => {
  const transport = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("private database text", { status: 403 }));
  render(<Harness />);
  expect(await screen.findByRole("alert")).toHaveTextContent("permissions");
  expect(screen.queryByLabelText("New team name")).not.toBeInTheDocument(); expect(screen.queryByText("private database text")).not.toBeInTheDocument(); expect(transport).toHaveBeenCalledTimes(1);
});
it("ignores a delayed old-workspace read even when transport ignores abort", async () => {
  let delayed: ((r: Response) => void) | undefined;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    if (String(url).includes(other)) return reply({ teams: [{ ...original, id: other, name: "Sales" }] });
    return new Promise<Response>((resolve) => { delayed = resolve; });
  });
  const view = render(<Harness />);
  view.rerender(<Harness workspace={other} />);
  await screen.findByRole("button", { name: "Manage Sales" });
  delayed?.(reply({ teams: [{ ...original, name: "Old stale result" }] }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Manage Old stale result" })).not.toBeInTheDocument());
  expect(screen.getByRole("button", { name: "Manage Sales" })).toBeInTheDocument();
});
