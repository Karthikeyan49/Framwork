import { StrictMode } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Link, MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { App } from "../App";
import { AuthProvider, useAuthentication, useConnectionTransport, type Connection } from "./Session";

const actor = "11111111-1111-1111-1111-111111111111";
const callback = { kind: "code" as const, code: "PRIVATE-code", state: "A".repeat(43), issuer: "https://id.example" };
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
const session = { user: { id: actor, name: "Signed-in actor" }, tenants: [] };
it("finishes a callback once under StrictMode and route remount, retaining only a verified in-memory connection", async () => {
  let exchanges = 0; let sessionReads = 0;
  const storage = vi.spyOn(Storage.prototype, "setItem");
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    if (String(url).endsWith("/auth/provider")) return response({ enabled: true, manual_token: false });
    if (String(url).endsWith("/login/complete")) { exchanges++; return response({ access_token: "synthetic.signature.token", token_type: "Bearer" }); }
    if (String(url).endsWith("/session")) { sessionReads++; return response(session); }
    throw new Error("unexpected request");
  });
  render(<StrictMode><MemoryRouter initialEntries={["/auth/callback"]}><Link to="/auth/callback">Reopen sign-in return</Link><App loginReturn={callback} /></MemoryRouter></StrictMode>);
  await screen.findByRole("heading", { name: session.user.name });
  expect(exchanges).toBe(1); expect(sessionReads).toBe(1); expect(screen.queryByLabelText("API access token")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("link", { name: "Overview" })); await userEvent.click(screen.getByRole("link", { name: "Workspace access" }));
  expect(screen.getByRole("heading", { name: session.user.name })).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  await userEvent.click(screen.getByRole("link", { name: "Reopen sign-in return" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("not completed"); expect(exchanges).toBe(1); expect(storage).not.toHaveBeenCalled();
  expect(document.body.textContent).not.toContain("synthetic.signature.token"); expect(document.body.textContent).not.toContain("PRIVATE-code");
});
it("rejects malformed/unregistered identity responses without rendering provider error bodies or granting a workspace", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => String(url).endsWith("/auth/provider") ? response({ enabled: true, manual_token: false }) : String(url).endsWith("/login/complete") ? response({ access_token: "a.b.c", token_type: "Bearer" }) : response({ error: "PRIVATE" }, 401));
  render(<MemoryRouter initialEntries={["/auth/callback"]}><App loginReturn={callback} /></MemoryRouter>);
  expect(await screen.findByRole("alert")).toHaveTextContent("account has no access"); expect(screen.queryByText("PRIVATE")).not.toBeInTheDocument(); expect(screen.queryByLabelText("Workspace")).not.toBeInTheDocument();
});
it("cancels an outstanding callback without letting its late result replace a newer connection", async () => {
  let resolve: ((r: Response) => void) | undefined; let exchanges = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    if (String(url).endsWith("/auth/provider")) return response({ enabled: false, manual_token: true });
    if (String(url).endsWith("/login/complete")) { exchanges++; return new Promise<Response>((done) => { resolve = done; }); }
    return response(session);
  });
  render(<MemoryRouter initialEntries={["/auth/callback"]}><Link to="/auth/callback">Reopen return</Link><App loginReturn={callback} /></MemoryRouter>);
  await waitFor(() => expect(exchanges).toBe(1));
  await userEvent.click(screen.getByRole("link", { name: "Return to workspace access" }));
  await userEvent.type(await screen.findByLabelText("API access token"), "other-manual-token"); await userEvent.click(screen.getByRole("button", { name: "Connect" }));
  await screen.findByRole("heading", { name: session.user.name });
  await act(async () => resolve?.(response({ access_token: "old.signature.token", token_type: "Bearer" })));
  await userEvent.click(screen.getByRole("link", { name: "Reopen return" }));
  await screen.findByRole("alert"); await userEvent.click(screen.getByRole("link", { name: "Return to workspace access" }));
  expect(screen.getByRole("heading", { name: session.user.name })).toBeInTheDocument(); expect(exchanges).toBe(1);
});
it("a stale failure can clear only its original connection object", async () => {
  const first: Connection = { token: "old", session }; const second = { token: "new", session: { ...session, user: { id: actor, name: "New connection" } } };
  function Probe() { const auth = useAuthentication(); return <><button onClick={() => auth.connect(first)}>First</button><button onClick={() => { auth.connect(second); auth.disconnect(first); }}>Replace and expire old</button><p>{auth.connection?.session.user.name}</p><button onClick={() => auth.disconnect(second)}>Expire current</button></>; }
  render(<AuthProvider loadOptions={false}><Probe /></AuthProvider>);
  await userEvent.click(screen.getByRole("button", { name: "First" })); await userEvent.click(screen.getByRole("button", { name: "Replace and expire old" })); expect(screen.getByText("New connection")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Expire current" })); expect(screen.queryByText("New connection")).not.toBeInTheDocument();
});

it("data-request expiration is bound to its bearer and connection; a stale 401 cannot clear a replacement", async () => {
  const first: Connection = { token: "old-token", session }; const second: Connection = { token: "new-token", session: { ...session, user: { id: actor, name: "Replacement" } } };
  let resolveOld: ((v: Response) => void) | undefined; let reads = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => { if (reads++ === 0) return new Promise<Response>((done) => { resolveOld = done; }); return response({}, 401); });
  function Probe() {
    const auth = useAuthentication(); const transport = useConnectionTransport(auth.connection?.token ?? "");
    return <><button onClick={() => auth.connect(first)}>First connection</button><button onClick={() => auth.connect(second)}>Replacement connection</button>
      <button onClick={() => void transport("/api/probe", { headers: { Authorization: `Bearer ${auth.connection?.token}` } })}>Owned request</button>
      <button onClick={() => void transport("/api/probe", { headers: { Authorization: "Bearer unrelated" } })}>Unrelated request</button><p>{auth.connection?.session.user.name}</p></>;
  }
  render(<AuthProvider loadOptions={false}><Probe /></AuthProvider>);
  await userEvent.click(screen.getByRole("button", { name: "First connection" })); await userEvent.click(screen.getByRole("button", { name: "Owned request" }));
  await userEvent.click(screen.getByRole("button", { name: "Replacement connection" })); await act(async () => resolveOld?.(response({}, 401)));
  expect(screen.getByText("Replacement")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Unrelated request" })); expect(screen.getByText("Replacement")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Owned request" })); expect(screen.queryByText("Replacement")).not.toBeInTheDocument();
});
