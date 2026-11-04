import { APIError } from "./client";
import { administrationMutation } from "./administration";
import { uuid } from "../preview/contracts";
import { decodeRoleContract, decodeRoleMember, decodeRolePage, decodeRoleVersion, roleName, roleWireJSON, type PreparedRolePublication } from "../administration/roleContracts";
import { object } from "../preview/contracts";

async function boundedJSON(response: Response) {
  const length = response.headers.get("Content-Length");
  if (!/^application\/json(?:\s*;|$)/i.test(response.headers.get("Content-Type") ?? "") || length && (!/^\d+$/.test(length) || Number(length) > 524288) || !response.body) throw new Error("Invalid role response");
  const reader = response.body.getReader(), chunks: Uint8Array[] = []; let size = 0;
  try {
    while (true) { const part = await reader.read(); if (part.done) break; size += part.value.byteLength; if (size > 524288) { await reader.cancel(); throw new Error("Role response exceeds its size limit"); } chunks.push(part.value); }
  } finally { reader.releaseLock(); }
  const body = new Uint8Array(size); let offset = 0;
  for (const part of chunks) { body.set(part, offset); offset += part.byteLength; }
  return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(body)) as unknown;
}

export function rolesClient(base: string, token: string, tenant: string, transport: typeof fetch = fetch) {
  if (!uuid(tenant)) throw new Error("Invalid workspace");
  const root = `${base}/v1/tenants/${tenant}`;
  const name = (v: string) => { if (!roleName(v)) throw new Error("Invalid role name"); return v; };
  async function request(path: string, signal?: AbortSignal, body?: string): Promise<unknown> {
    const response = await transport(root + path, { method: body === undefined ? "GET" : "POST", credentials: "omit", headers: { Accept: "application/json", Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { "Content-Type": "application/json" }) }, body, signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000) });
    if (!response.ok) throw new APIError(response.status, response.headers.get("X-Request-ID") ?? undefined);
    return boundedJSON(response);
  }
  return {
    list: async (after = "", signal?: AbortSignal) => decodeRolePage(await request("/roles" + (after ? `?after=${name(after)}` : ""), signal), after),
    contract: async (role: string, signal?: AbortSignal) => decodeRoleContract(await request(`/role-contract?name=${name(role)}`, signal), role),
    current: async (role: string, signal?: AbortSignal) => decodeRoleVersion(await request(`/roles/${name(role)}`, signal), role),
    operation: async (id: string, signal?: AbortSignal) => { if (!uuid(id)) throw new Error("Invalid role operation"); const role = decodeRoleVersion(await request(`/roles/operations/${id}`, signal)); if (role.operation_id !== id) throw new Error("Role operation witness mismatch"); return role; },
    member: async (user: string, signal?: AbortSignal) => { if (!uuid(user)) throw new Error("Invalid member ID"); return decodeRoleMember(await request(`/members/${user}`, signal), user); },
    publish: async (prepared: PreparedRolePublication, signal?: AbortSignal) => administrationMutation(() => request("/roles", signal, prepared.body), (v) => {
      const saved = decodeRoleVersion(v, prepared.name);
      if (saved.bootstrap || saved.version !== prepared.expectedVersion + 1 || saved.hash !== prepared.hash || saved.active !== prepared.active || saved.operation_id !== prepared.operationID || roleWireJSON(saved.definition) !== prepared.serializedDefinition) throw new Error("Publication witness mismatch");
      return saved;
    }),
    assign: async (user: string, role: string, expectedRole: string, signal?: AbortSignal) => {
      if (!uuid(user)) throw new Error("Invalid member ID"); name(role); name(expectedRole);
      return administrationMutation(() => request(`/members/${user}/role`, signal, JSON.stringify({ role, expected_role: expectedRole })), (v) => {
        if (!object(v) || v.assigned !== true || v.user_id !== user || v.role !== role || v.expected_role !== expectedRole) throw new Error("Assignment witness mismatch");
        return { user_id: user, role };
      });
    },
  };
}
export type RolesClient = ReturnType<typeof rolesClient>;
