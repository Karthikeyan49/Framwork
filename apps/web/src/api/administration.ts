import { APIError } from "./client";
import { uuid } from "../preview/contracts";
import { decodeMember, decodeMembers, decodeTeam, decodeTeamDetail, decodeTeams, type Team } from "../administration/contracts";

export class AdministrationOutcomeUnknown extends Error {
  constructor() { super("Administration outcome requires review"); this.name = "AdministrationOutcomeUnknown"; }
}
export async function administrationMutation<T>(send: () => Promise<unknown>, decode: (v: unknown) => T): Promise<T> {
  try { return decode(await send()); }
  catch (e) {
    if (e instanceof APIError && e.status >= 400 && e.status < 500) throw e;
    throw new AdministrationOutcomeUnknown();
  }
}
export function administrationClient(base: string, token: string, tenant: string, transport: typeof fetch = fetch) {
  if (!uuid(tenant)) throw new Error("Invalid workspace");
  const root = `${base}/v1/tenants/${tenant}`;
  function id(value: string) { if (!uuid(value)) throw new Error("Invalid identifier"); return value; }
  async function request(path: string, body?: unknown, signal?: AbortSignal): Promise<unknown> {
    const response = await transport(root + path, {
      method: body === undefined ? "GET" : "POST", credentials: "omit",
      headers: { Accept: "application/json", Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000),
    });
    if (!response.ok) throw new APIError(response.status, response.headers.get("X-Request-ID") ?? undefined);
    return response.json();
  }
  return {
    teams: async (signal?: AbortSignal) => decodeTeams(await request("/teams", undefined, signal)),
    team: async (team: string, signal?: AbortSignal) => decodeTeamDetail(await request(`/teams/${id(team)}`, undefined, signal), team),
    members: async (signal?: AbortSignal) => decodeMembers(await request("/members", undefined, signal)),
    saveTeam: async (team: Omit<Team, "version">, expected: number, signal?: AbortSignal) => {
      id(team.id); if (team.parent_id) id(team.parent_id);
      if (!Number.isSafeInteger(expected) || expected < 0 || expected >= 1e9) throw new Error("Invalid version");
      return administrationMutation(() => request("/teams", { ...team, expected_version: expected }, signal), (v) => {
        const saved = decodeTeam(v, team.id);
        if (saved.version !== expected + 1 || saved.name !== team.name || saved.parent_id !== team.parent_id) throw new Error("Team witness mismatch");
        return saved;
      });
    },
    setMember: async (team: string, user: string, active: boolean, expected: boolean | null, signal?: AbortSignal) => {
      const path = `/teams/${id(team)}/members/${id(user)}`;
      return administrationMutation(() => request(path, { active, expected_active: expected }, signal), (v) => {
        const saved = decodeMember(v);
        if (saved.user_id !== user || saved.active !== active) throw new Error("Membership witness mismatch");
        return saved;
      });
    },
  };
}
export function administrationMessage(e: unknown) {
  if (e instanceof AdministrationOutcomeUnknown) return "The result is uncertain. Reload and review the current state before making a new change. Do not repeat the previous request.";
  if (e instanceof APIError) {
    if (e.status === 401) return "Your session expired. Disconnect and connect again.";
    if (e.status === 403) return "Your permissions do not allow this operation.";
    if (e.status === 404) return "This resource is unavailable. Check your selection and access.";
    if (e.status === 409) return "The state changed or a limit was reached. Reload and review before making a new change.";
    if (e.status === 400) return "Check the IDs, name and requested change, then reload before trying again.";
  }
  return "Current state could not be verified. Check the service and reload.";
}
