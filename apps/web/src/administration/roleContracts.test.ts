import { webcrypto } from "node:crypto";
import { afterEach, expect, it, vi } from "vitest";
import { decodeRoleContract, decodeRoleDefinition, decodeRoleMember, decodeRolePage, decodeRoleScope, decodeRoleVersion, parseConditionValue, prepareRolePublication, roleLimits, roleSchema, type RoleContract, type RoleDefinition } from "./roleContracts";
import { compileDraft } from "./RoleEditor";
import { rolesClient } from "../api/roles";
import { AdministrationOutcomeUnknown } from "../api/administration";
import { APIError } from "../api/client";
const tenant = "11111111-1111-1111-1111-111111111111", user = "22222222-2222-2222-2222-222222222222", operation = "33333333-3333-3333-3333-333333333333";
const definition: RoleDefinition = { schema_version: roleSchema, name: "finance_manager", grants: [] };
const witness = { name: definition.name, definition, version: 2, hash: "a".repeat(64), active: true, bootstrap: false, operation_id: operation };
const contract: RoleContract = { schema_version: roleSchema, name: definition.name, root_protected: false, actions: [{ name: "tenant.access", allow_supported: true, scope_required: false }, { name: "data.entity.read", allow_supported: true, scope_required: true }, { name: "roles.assign", allow_supported: false, scope_required: false }], limits: roleLimits };
const reply = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
afterEach(() => vi.unstubAllGlobals());
it("rejects changed budgets, mismatched names, duplicate capabilities and unsupported authority", () => {
  expect(decodeRoleContract(contract, definition.name)).toEqual(contract);
  for (const value of [{ ...contract, limits: { ...roleLimits, scope_depth: 7 } }, { ...contract, actions: [contract.actions[0], contract.actions[0]] }, { ...contract, root_protected: true }, { ...contract, name: "elsewhere" }]) expect(() => decodeRoleContract(value, definition.name)).toThrow();
  expect(() => decodeRoleDefinition({ ...definition, grants: [{ effect: "ALLOW", actions: ["roles.assign"], resource: { app_id: "*", entity_id: "*", field_id: "*" } }] }, definition.name, contract)).toThrow();
});
it("losslessly parses exact condition values and refuses ambiguous JSON instead of rounding", () => {
  expect(parseConditionValue('{"money":"9007199254740993.01","ok":[1,true,null,"☀"]}')).toEqual({ money: "9007199254740993.01", ok: [1, true, null, "☀"] });
  for (const value of ['9007199254740993', '1.1', '1e2', '01', '-01', 'true false', '{"a":1,"a":2}', '{"a":1,"\\u0061":2}', '"\\ud800"', '[1,]', '{"a":}', '"' + 'x'.repeat(1024) + '"']) expect(() => parseConditionValue(value)).toThrow();
  const ownProto = parseConditionValue('{"__proto__":{"safe":true}}') as object;
  expect(Object.hasOwn(ownProto, '__proto__')).toBe(true);
  expect(Object.getOwnPropertyDescriptor(ownProto, '__proto__')?.value).toEqual({ safe: true });
  expect({}).not.toHaveProperty('safe');
});
it("bounds recursive scopes, rejects trusted authority and preserves is_null semantics", () => {
  const condition = { kind: "CONDITION", condition: { field: "field.invoice.amount", op: "eq", value: "100.00" } };
  expect(decodeRoleScope({ kind: "AND", children: [{ kind: "OWNER" }, condition] })).toMatchObject({ kind: "AND", children: [{ kind: "OWNER" }, condition] });
  let deep: unknown = { kind: "NONE" }; for (let i = 0; i < 7; i++) deep = { kind: "OR", children: [deep] };
  for (const value of [deep, { kind: "TEAM", teams: [tenant] }, { kind: "SHARE", write_share: true }, { kind: "AND", children: [] }, { kind: "OR", children: Array(9).fill({ kind: "NONE" }) }, { kind: "CONDITION", condition: { field: "field.a", op: "is_null", value: null } }]) expect(() => decodeRoleScope(value)).toThrow();
  const resource = { app_id: "*", entity_id: "*", field_id: "*" };
  expect(() => compileDraft(definition.name, [{ effect: "ALLOW", actions: ["data.entity.read"], resource, scope: { kind: "CONDITION", field: "field.a", op: "eq", value: "1.5" } }], contract)).toThrow();
  expect(() => decodeRoleDefinition({ ...definition, grants: Array(9).fill({ effect: "ALLOW", actions: ["data.entity.read"], resource, scope: { kind: "OWNER" } }) }, definition.name, contract)).toThrow();
});
it("checks page order/continuation, root/native distinctions and exact member witnesses", () => {
  expect(decodeRolePage({ roles: [witness] }).roles).toHaveLength(1);
  for (const value of [{ roles: [witness, witness] }, { roles: [witness], next: witness.name }, { roles: [{ ...witness, hash: "bad" }] }]) expect(() => decodeRolePage(value)).toThrow();
  expect(() => decodeRolePage({ roles: [witness] }, witness.name)).toThrow();
  expect(() => decodeRoleVersion({ ...witness, name: "owner", bootstrap: false, definition: { ...definition, name: "owner" } })).toThrow();
  expect(() => decodeRoleVersion({ ...witness, bootstrap: true })).toThrow();
  expect(() => decodeRoleMember({ user_id: operation, active: true, role: "owner" }, user)).toThrow();
});
it("binds publication to exact bytes, next version, active flag and operation without POST retry", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const prepared = await prepareRolePublication(definition, contract, 2, false);
  const expected = { ...witness, version: 3, active: false, hash: prepared.hash, operation_id: prepared.operationID };
  const transport = vi.fn<typeof fetch>().mockResolvedValue(reply(expected));
  const client = rolesClient("/api", "synthetic", tenant, transport);
  expect(await client.publish(prepared)).toEqual(expected);
  const body = JSON.parse(String(transport.mock.calls[0][1]?.body));
  expect(body).toEqual({ operation_id: prepared.operationID, expected_version: 2, active: false, definition });
  for (const change of [{ hash: "b".repeat(64) }, { version: 4 }, { active: true }, { operation_id: operation }, { definition: { ...definition, name: "wrong" } }]) {
    transport.mockResolvedValueOnce(reply({ ...expected, ...change }));
    await expect(client.publish(prepared)).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
  }
  expect(transport).toHaveBeenCalledTimes(6);
});
it("treats 4xx as rejection and every malformed/transport success as uncertain", async () => {
  const transport = vi.fn<typeof fetch>(); const client = rolesClient("/api", "synthetic", tenant, transport);
  for (const value of [{ assigned: true }, { assigned: true, user_id: user, role: "member", expected_role: "owner" }]) {
    transport.mockResolvedValueOnce(reply(value)); await expect(client.assign(user, "finance_manager", "owner")).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
  }
  transport.mockResolvedValueOnce(reply("PRIVATE", 409)); await expect(client.assign(user, "finance_manager", "owner")).rejects.toBeInstanceOf(APIError);
  transport.mockRejectedValueOnce(new Error("PRIVATE")); await expect(client.assign(user, "finance_manager", "owner")).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
  transport.mockResolvedValueOnce(new Response('x'.repeat(524289), { headers: { "Content-Type": "application/json" } })); await expect(client.assign(user, "finance_manager", "owner")).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
  expect(transport).toHaveBeenCalledTimes(5);
});
it("uses bounded tenant point paths and refuses a wrong receipt operation", async () => {
  const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(reply({ roles: [witness] })).mockResolvedValueOnce(reply({ user_id: user, role: "member", active: true })).mockResolvedValueOnce(reply(witness));
  const client = rolesClient("/api", "synthetic", tenant, transport);
  await client.list("admin"); await client.member(user);
  await expect(client.operation(user)).rejects.toThrow("witness mismatch");
  expect(transport.mock.calls[0][0]).toBe(`/api/v1/tenants/${tenant}/roles?after=admin`);
  expect(transport.mock.calls[1][0]).toBe(`/api/v1/tenants/${tenant}/members/${user}`);
  await expect(client.current("../../roles")).rejects.toThrow(); expect(transport).toHaveBeenCalledTimes(3);
});

it("matches the actual Go HTTP hash for UTF-8/integer keys and HTML/line-separator escaping", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const fixture = await import("../../../../contracts/examples/role-editor-wire-v1.json");
  const d = decodeRoleDefinition(fixture.definition, "wire_fixture");
  const c = { ...contract, name: "wire_fixture" };
  const prepared = await prepareRolePublication(d, c, 0, true);
  expect(prepared.hash).toBe(fixture.expected_http_hash);
});
