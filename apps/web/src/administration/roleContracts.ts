import { object, uuid } from "../preview/contracts";

export const roleName = (v: unknown): v is string => typeof v === "string" && /^[a-z][a-z0-9_]{0,63}$/.test(v);
const actionName = (v: unknown): v is string => typeof v === "string" && /^[a-z]+(?:\.[a-z]+){1,2}$/.test(v);
const symbol = (v: unknown, prefix: string) => typeof v === "string" && v.length <= 160 && (v === "*" || new RegExp(`^${prefix}\\.[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$`).test(v));
const keys = (v: Record<string, unknown>, allowed: string[]) => Object.keys(v).every((k) => allowed.includes(k));
const bytes = (v: string) => new TextEncoder().encode(v).length;
// The V1 HTTP decoder re-encodes strict JSON maps with Go encoding/json before
// storing RawMessage bytes. Match that existing wire profile, including UTF-8
// key order and HTML/line-separator escapes; do not hash insertion-order JSON.
export function roleWireJSON(value: unknown): string {
  if (value === null || typeof value === "boolean" || typeof value === "number" && Number.isSafeInteger(value)) return JSON.stringify(value);
  const quote = (text: string) => {
    plainValue(text);
    return JSON.stringify(text).replace(/[<>&\u2028\u2029]/g, (ch) => `\\u${ch.charCodeAt(0).toString(16).padStart(4, "0")}`);
  };
  if (typeof value === "string") return quote(value);
  if (Array.isArray(value)) return `[${value.map(roleWireJSON).join(",")}]`;
  if (object(value)) {
    const encoder = new TextEncoder();
    const order = (a: string, b: string) => { const left = encoder.encode(a), right = encoder.encode(b); for (let i = 0; i < Math.min(left.length, right.length); i++) if (left[i] !== right[i]) return left[i] - right[i]; return left.length - right.length; };
    return `{${Object.keys(value).sort(order).map((key) => `${quote(key)}:${roleWireJSON(value[key])}`).join(",")}}`;
  }
  throw new Error("Invalid role JSON value");
}
export const roleSchema = "tenant-capabilities-v1";
export const conditionOperators = ["eq", "neq", "gt", "gte", "lt", "lte", "contains", "is_null", "in", "not_in"] as const;
export const scopeKinds = ["ALL", "NONE", "OWNER", "TEAM", "SHARE", "CONDITION", "AND", "OR"] as const;
export type ScopeKind = typeof scopeKinds[number];
export interface RoleScope { kind: ScopeKind; condition?: { field: string; op: string; value?: unknown }; children?: RoleScope[] }
export interface RoleGrant { effect: "ALLOW" | "DENY"; actions: string[]; resource: { app_id: string; entity_id: string; field_id: string }; scope?: RoleScope }
export interface RoleDefinition { schema_version: typeof roleSchema; name: string; grants: RoleGrant[] }
export interface RoleSummary { name: string; version: number; hash: string; active: boolean; bootstrap: boolean; operation_id: string }
export interface RoleVersion extends RoleSummary { definition: RoleDefinition }
export interface RoleMember { user_id: string; role: string; active: boolean }
export const roleLimits = { definition_bytes: 65536, grants: 64, actions_per_grant: 64, scope_depth: 6, scope_nodes: 32, scope_children: 8, condition_bytes: 1024, scopes_per_action: 8, nodes_per_action: 63 } as const;
export interface RoleContract { schema_version: typeof roleSchema; name: string; root_protected: boolean; actions: { name: string; allow_supported: boolean; scope_required: boolean }[]; limits: typeof roleLimits }

export function decodeRoleContract(v: unknown, name: string): RoleContract {
  if (!object(v) || v.schema_version !== roleSchema || v.name !== name || !roleName(name) || v.root_protected !== (name === "owner") || !object(v.limits) || !Array.isArray(v.actions) || !v.actions.length || v.actions.length > 64) throw new Error("Unsupported role contract");
  if (Object.keys(v.limits).length !== Object.keys(roleLimits).length || Object.entries(roleLimits).some(([key, value]) => (v.limits as Record<string, unknown>)[key] !== value)) throw new Error("Unsupported role limits");
  const actions = v.actions.map((a: unknown) => {
    if (!object(a) || !actionName(a.name) || typeof a.allow_supported !== "boolean" || typeof a.scope_required !== "boolean" || a.scope_required !== (a.name.startsWith("data.entity.") || a.name.startsWith("data.field."))) throw new Error("Invalid role capability");
    return { name: a.name, allow_supported: a.allow_supported, scope_required: a.scope_required };
  });
  if (new Set(actions.map((a) => a.name)).size !== actions.length) throw new Error("Duplicate role capability");
  return { schema_version: roleSchema, name, root_protected: name === "owner", actions, limits: roleLimits };
}

function plainValue(v: unknown, depth = 0, counter = { nodes: 0 }): unknown {
  if (++counter.nodes > 256 || depth > 8) throw new Error("Condition value is too complex");
  if (v === null || typeof v === "boolean") return v;
  if (typeof v === "string" && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v)) return v;
  if (typeof v === "number" && Number.isSafeInteger(v)) return v;
  if (Array.isArray(v)) return v.map((x) => plainValue(x, depth + 1, counter));
  if (object(v)) return Object.fromEntries(Object.entries(v).map(([k, value]) => [k, plainValue(value, depth + 1, counter)]));
  throw new Error("Use exact strings for decimal values and safe whole numbers for integers");
}

// Parse author-entered JSON without silently rounding numbers or accepting
// duplicate object keys. JSON response decoders separately validate safe values.
export function parseConditionValue(raw: string): unknown {
  if (bytes(raw) > roleLimits.condition_bytes) throw new Error("Condition value exceeds its size limit");
  let position = 0; let nodes = 0;
  const space = () => { while (/[\t\n\r ]/.test(raw[position] ?? "!")) position++; };
  function string() {
    // JSON strings exclude raw control bytes and permit only JSON escapes.
    // eslint-disable-next-line no-control-regex
    const match = /^"(?:[^"\\\u0000-\u001f]|\\(?:["\\/bfnrt]|u[\da-fA-F]{4}))*"/.exec(raw.slice(position));
    if (!match) throw new Error("Invalid JSON string");
    position += match[0].length;
    return plainValue(JSON.parse(match[0])) as string;
  }
  function value(depth: number): unknown {
    if (++nodes > 256 || depth > 8) throw new Error("Condition value is too complex");
    space();
    const ch = raw[position];
    if (ch === '"') return string();
    if (ch === "[" || ch === "{") {
      position++; space(); const array: unknown[] = []; const entries: [string, unknown][] = []; const seen = new Set<string>();
      const end = ch === "[" ? "]" : "}";
      if (raw[position] === end) { position++; return ch === "[" ? array : {}; }
      while (true) {
        space(); let key = "";
        if (ch === "{") {
          key = string(); if (seen.has(key)) throw new Error("Duplicate condition key"); seen.add(key);
          space(); if (raw[position++] !== ":") throw new Error("Invalid condition object");
        }
        const child = value(depth + 1);
        if (ch === "[") array.push(child); else entries.push([key, child]);
        space();
        if (raw[position] === end) { position++; return ch === "[" ? array : Object.fromEntries(entries); }
        if (raw[position++] !== ",") throw new Error("Invalid condition JSON");
      }
    }
    for (const word of ["true", "false", "null"]) if (raw.slice(position).startsWith(word)) { position += word.length; return JSON.parse(word) as unknown; }
    const number = /^-?(?:0|[1-9][0-9]*)/.exec(raw.slice(position));
    if (!number || !Number.isSafeInteger(Number(number[0]))) throw new Error("Use safe whole numbers or quoted exact decimals");
    position += number[0].length; return Number(number[0]);
  }
  const parsed = value(0); space();
  if (position !== raw.length) throw new Error("Use one JSON value; decimal values must be quoted strings");
  return parsed;
}

export function decodeRoleScope(v: unknown, depth = 0, counter = { nodes: 0 }): RoleScope {
  if (++counter.nodes > 32 || depth > 6 || !object(v) || !scopeKinds.includes(v.kind as ScopeKind) || !keys(v, ["kind", "condition", "children", "teams", "write_share"]) || v.write_share !== undefined && v.write_share !== false || v.teams != null && (!Array.isArray(v.teams) || v.teams.length)) throw new Error("Invalid role scope");
  const kind = v.kind as ScopeKind;
  if (kind === "AND" || kind === "OR") {
    if (v.condition != null || !Array.isArray(v.children) || !v.children.length || v.children.length > 8) throw new Error("Invalid combined scope");
    return { kind, children: v.children.map((c) => decodeRoleScope(c, depth + 1, counter)) };
  }
  if (v.children != null && (!Array.isArray(v.children) || v.children.length)) throw new Error("Unexpected scope children");
  if (kind === "CONDITION") {
    const c = v.condition;
    if (!object(c) || !keys(c, ["field", "op", "value"]) || !symbol(c.field, "field") || c.field === "*" || !conditionOperators.includes(c.op as typeof conditionOperators[number])) throw new Error("Invalid record condition");
    if ((c.op === "is_null") !== (c.value === undefined)) throw new Error("Invalid condition value");
    const condition: NonNullable<RoleScope["condition"]> = { field: c.field as string, op: c.op as string };
    if (c.value !== undefined) { condition.value = plainValue(c.value); if (bytes(roleWireJSON(condition.value)) > 1024) throw new Error("Condition value exceeds its size limit"); }
    return { kind, condition };
  }
  if (v.condition != null) throw new Error("Unexpected scope condition");
  return { kind };
}

export function decodeRoleDefinition(v: unknown, name: string, contract?: RoleContract): RoleDefinition {
  if (!object(v) || !keys(v, ["schema_version", "name", "grants"]) || v.schema_version !== roleSchema || v.name !== name || !roleName(name) || !Array.isArray(v.grants) || v.grants.length > 64 || contract && contract.name !== name) throw new Error("Invalid role definition");
  const count = new Map<string, { grants: number; nodes: number }>();
  const grants = v.grants.map((g: unknown): RoleGrant => {
    if (!object(g) || !keys(g, ["effect", "actions", "resource", "scope"]) || g.effect !== "ALLOW" && g.effect !== "DENY" || !Array.isArray(g.actions) || !g.actions.length || g.actions.length > 64 || !g.actions.every(actionName) || new Set(g.actions).size !== g.actions.length || !object(g.resource) || !keys(g.resource, ["app_id", "entity_id", "field_id"]) || !symbol(g.resource.app_id, "app") || !symbol(g.resource.entity_id, "entity") || !symbol(g.resource.field_id, "field")) throw new Error("Invalid role grant");
    const nodes = { nodes: 0 };
    const scope = g.scope == null ? undefined : decodeRoleScope(g.scope, 0, nodes);
    for (const action of g.actions as string[]) {
      const info = contract?.actions.find((a) => a.name === action);
      if (contract && (!info || g.effect === "ALLOW" && !info.allow_supported)) throw new Error("Capability is not supported for this role");
      const scoped = action.startsWith("data.entity.") || action.startsWith("data.field.");
      if (scope && !scoped || scoped && g.effect === "ALLOW" && !scope || g.effect === "DENY" && scope && scope.kind !== "ALL") throw new Error("Scope does not match its grant");
      if (g.effect === "ALLOW" && scope) {
        const current = count.get(action) ?? { grants: 0, nodes: 0 }; current.grants++; current.nodes += nodes.nodes; count.set(action, current);
        if (current.grants > 8 || current.nodes > 63) throw new Error("Combined action scopes exceed their limit");
      }
    }
    return { effect: g.effect, actions: [...g.actions] as string[], resource: { app_id: g.resource.app_id as string, entity_id: g.resource.entity_id as string, field_id: g.resource.field_id as string }, ...(scope ? { scope } : {}) };
  });
  const d: RoleDefinition = { schema_version: roleSchema, name, grants };
  if (bytes(roleWireJSON(d)) > 65536) throw new Error("Role definition exceeds its size limit");
  return d;
}

export function decodeRoleSummary(v: unknown, name?: string): RoleSummary {
  if (!object(v) || !roleName(v.name) || name !== undefined && v.name !== name || !Number.isSafeInteger(v.version) || Number(v.version) < 1 || Number(v.version) > 1e9 || typeof v.hash !== "string" || !/^[0-9a-f]{64}$/.test(v.hash) || typeof v.active !== "boolean" || typeof v.bootstrap !== "boolean" || !uuid(v.operation_id) || v.bootstrap && (!['owner', 'admin', 'member', 'viewer'].includes(v.name) || v.version !== 1) || v.name === "owner" && (!v.bootstrap || !v.active || v.version !== 1)) throw new Error("Invalid role witness");
  return { name: v.name, version: Number(v.version), hash: v.hash, active: v.active, bootstrap: v.bootstrap, operation_id: v.operation_id };
}
export function decodeRoleVersion(v: unknown, name?: string): RoleVersion {
  const role = decodeRoleSummary(v, name);
  if (!object(v)) throw new Error("Invalid role response");
  const definition = decodeRoleDefinition(v.definition, role.name);
  if (role.bootstrap && definition.grants.length) throw new Error("Invalid bootstrap definition");
  return { ...role, definition };
}
export function decodeRolePage(v: unknown, after = ""): { roles: RoleSummary[]; next: string } {
  if (!object(v) || !Array.isArray(v.roles) || v.roles.length > 100 || v.next !== undefined && !roleName(v.next)) throw new Error("Invalid role page");
  const roles = v.roles.map((r) => decodeRoleSummary(r));
  if (roles.some((r, i) => r.name <= (i ? roles[i - 1].name : after)) || v.next !== undefined && (roles.length !== 100 || v.next !== roles[99].name)) throw new Error("Invalid role continuation");
  return { roles, next: (v.next as string | undefined) ?? "" };
}
export function decodeRoleMember(v: unknown, user: string): RoleMember {
  if (!object(v) || !uuid(v.user_id) || v.user_id !== user || !roleName(v.role) || typeof v.active !== "boolean") throw new Error("Invalid role membership");
  return { user_id: user, role: v.role, active: v.active };
}

export interface PreparedRolePublication { name: string; operationID: string; expectedVersion: number; active: boolean; hash: string; body: string; serializedDefinition: string }
export async function roleDefinitionHash(definition: RoleDefinition): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(roleWireJSON(definition)));
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}
export async function prepareRolePublication(definition: RoleDefinition, contract: RoleContract, expectedVersion: number, active: boolean): Promise<PreparedRolePublication> {
  if (contract.root_protected || !Number.isSafeInteger(expectedVersion) || expectedVersion < 0 || expectedVersion >= 1e9 || typeof active !== "boolean") throw new Error("Role cannot be published");
  const valid = decodeRoleDefinition(definition, contract.name, contract);
  const serializedDefinition = roleWireJSON(valid), operationID = crypto.randomUUID();
  const hash = await roleDefinitionHash(valid);
  return { name: valid.name, operationID, expectedVersion, active, hash, serializedDefinition, body: JSON.stringify({ operation_id: operationID, expected_version: expectedVersion, active, definition: valid }) };
}
