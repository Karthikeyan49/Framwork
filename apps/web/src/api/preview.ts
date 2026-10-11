import { APIError } from "./client";
import {
  decodePreview,
  decodeResult,
  decodeRow,
  type Row,
  uuid,
} from "../preview/contracts";

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
  ): Promise<unknown> {
    const response = await transport(path + suffix, {
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
    command: async (
      entity: string,
      operation: "create" | "update" | "delete",
      values: Record<string, unknown>,
      row?: Row,
      signal?: AbortSignal,
    ) =>
      decodeRow(
        await request(
          entityPath(entity) + "/commands",
          {
            operation,
            ...(row ? { id: row.id, expected_version: row.version } : {}),
            ...(operation !== "delete" ? { values } : {}),
          },
          signal,
        ),
      ),
  };
}
