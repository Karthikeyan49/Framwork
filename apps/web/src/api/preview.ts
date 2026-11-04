import { APIError } from "./client";
import { administrationMutation } from "./administration";
import { decodeAccess, version } from "../administration/contracts";
import {
  decodePreview,
  decodeResult,
  decodeRow,
  type Row,
  uuid,
} from "../preview/contracts";

export class CommandOutcomeUnknown extends Error {
  constructor(readonly commandKey: string) {
    super("Command outcome requires reconciliation");
    this.name = "CommandOutcomeUnknown";
  }
}

export type CommandReceipt = {
  state: "UNKNOWN" | "COMMITTED";
  command_key: string;
  record_id?: string;
  record_version?: number;
};

export function previewClient(
  base: string,
  token: string,
  tenant: string,
  app: string,
  revision: number,
  transport: typeof fetch = fetch,
) {
  if (
    !uuid(tenant) ||
    !/^app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$/.test(app) ||
    app.length > 160 ||
    !Number.isSafeInteger(revision) ||
    revision < 1 ||
    revision > 1e9
  )
    throw new Error("Invalid selection");
  const path = `${base}/v1/tenants/${tenant}/preview/${app}/versions/${revision}`;
  async function request(
    suffix: string,
    body: unknown,
    signal?: AbortSignal,
    prefix = path,
  ): Promise<unknown> {
    const response = await transport(prefix + suffix, {
      method: body === undefined ? "GET" : "POST",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${token}`,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      credentials: "omit",
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: signal
        ? AbortSignal.any([signal, AbortSignal.timeout(5000)])
        : AbortSignal.timeout(5000),
    });
    if (!response.ok)
      throw new APIError(
        response.status,
        response.headers.get("X-Request-ID") ?? undefined,
      );
    return response.json();
  }
  function entityPath(entity: string) {
    if (
      !/^entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$/.test(entity) ||
      entity.length > 160
    )
      throw new Error("Invalid entity");
    return `/entities/${entity}`;
  }
  return {
    record: async (entity: string, id: string, projection: string[], signal?: AbortSignal) => {
      if (!uuid(id)) throw new Error("Invalid record");
      const result = decodeResult(await request(entityPath(entity) + "/query", { record_id: id, projection, limit: 1 }, signal));
      if (result.records.length !== 1 || result.records[0].id !== id) throw new APIError(404);
      return result.records[0];
    },
    access: async (entity: string, id: string, signal?: AbortSignal) => {
      if (!uuid(id)) throw new Error("Invalid record");
      return decodeAccess(await request(entityPath(entity) + `/records/${id}/access`, undefined, signal), id);
    },
    changeAccess: async (entity: string, id: string, command: {
      operation: "owner" | "share" | "unshare"; expected_version: number; user_id: string; team_id?: string; can_write?: boolean;
    }, signal?: AbortSignal) => {
      if (!uuid(id) || !uuid(command.user_id) || !version(command.expected_version) || command.team_id && !uuid(command.team_id)) throw new Error("Invalid access change");
      const suffix = entityPath(entity) + `/records/${id}/access`;
      return administrationMutation(() => request(suffix, command, signal), (v) => {
        const result = decodeAccess(v, id);
        if (result.version !== command.expected_version + 1 || command.operation === "owner" &&
          (result.owner_user_id !== command.user_id || result.owner_team_id !== (command.team_id ?? ""))) throw new Error("Access witness mismatch");
        return result;
      });
    },
    descriptor: async (signal?: AbortSignal) =>
      decodePreview(await request("", undefined, signal), app, revision),
    query: async (
      entity: string,
      projection: string[],
      after = "",
      signal?: AbortSignal,
    ) =>
      decodeResult(
        await request(
          entityPath(entity) + "/query",
          { projection, limit: 20, ...(after ? { after } : {}) },
          signal,
        ),
      ),
    receipt: async (key: string, signal?: AbortSignal): Promise<CommandReceipt> => {
      if (!uuid(key)) throw new Error("Invalid command key");
      const value = await request(
        `/commands/${key}`,
        undefined,
        signal,
        `${base}/v1/tenants/${tenant}/preview/${app}`,
      );
      if (!value || typeof value !== "object") throw new Error("Invalid receipt");
      const r = value as CommandReceipt;
      if (r.command_key !== key ||
        (r.state !== "UNKNOWN" && r.state !== "COMMITTED") ||
        (r.state === "COMMITTED" && (!uuid(r.record_id) ||
          !Number.isSafeInteger(r.record_version) || r.record_version! < 1)))
        throw new Error("Invalid receipt");
      return r;
    },
    command: async (
      entity: string,
      operation: "create" | "update" | "patch" | "delete",
      values: Record<string, unknown>,
      row?: Row,
      signal?: AbortSignal,
      key = crypto.randomUUID(),
    ) => {
      if (!uuid(key)) throw new Error("Invalid command key");
      try {
        return decodeRow(await request(
          entityPath(entity) + "/commands",
          {
            command_key: key,
            operation,
            ...(row ? { id: row.id, expected_version: row.version } : {}),
            ...(operation !== "delete" ? { values } : {}),
          },
          signal,
        ));
      } catch (e) {
        if (!(e instanceof APIError) || e.status >= 500)
          throw new CommandOutcomeUnknown(key);
        throw e;
      }
    },
  };
}
