import { describe, expect, it, vi } from "vitest";
import { APIError } from "../api/client";
import { administrationClient, AdministrationOutcomeUnknown } from "../api/administration";
import { previewClient } from "../api/preview";
import { decodeAccess, decodeTeamDetail, decodeTeams } from "./contracts";
const tenant = "11111111-1111-1111-1111-111111111111";
const team = "22222222-2222-2222-2222-222222222222";
const user = "33333333-3333-3333-3333-333333333333";
const record = "44444444-4444-4444-4444-444444444444";
const response = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status });
const entry = { id: team, name: "Finance", version: 2 };
const access = { record_id: record, version: 2, owner_user_id: user };

describe("governed administration contracts", () => {
  it("accepts bounded empty/legacy-optional lists and preserves false membership state", () => {
    expect(decodeTeams({ teams: [] })).toEqual([]);
    expect(decodeTeamDetail({ ...entry, members: [{ user_id: user, active: false }] }, team).members[0].active).toBe(false);
    expect(decodeAccess(access, record)).toEqual({ ...access, owner_team_id: "", shares: [], shares_truncated: false });
  });
  it.each([
    { teams: [entry, entry] }, { teams: Array(257).fill(entry) }, { teams: [{ ...entry, parent_id: "../foreign" }] },
    { teams: [{ ...entry, version: 0 }] }, { teams: [{ ...entry, name: "Unsafe\u0001name" }] }, { teams: [{ ...entry, name: " " }] },
  ])("rejects malformed/duplicate/over-budget hierarchy: %j", (value) => expect(() => decodeTeams(value)).toThrow());
  it("rejects wrong-resource and over-budget membership/share responses", () => {
    expect(() => decodeTeamDetail({ ...entry, members: [] }, user)).toThrow();
    for (const members of [[{ user_id: user, active: false }, { user_id: user, active: true }], Array(1001).fill({ user_id: user, active: true })])
      expect(() => decodeTeamDetail({ ...entry, members }, team)).toThrow();
    for (const shares of [[{ user_id: user, can_write: "false" }], Array(257).fill({ user_id: user, can_write: false }), [{ user_id: user, can_write: false }, { user_id: user, can_write: true }]])
      expect(() => decodeAccess({ ...access, shares }, record)).toThrow();
    expect(() => decodeAccess(access, user)).toThrow();
  });
  it("renders names as data and retains a truncated-share witness", () => {
    expect(decodeTeams({ teams: [{ ...entry, name: "<script>data</script>" }] })[0].name).toContain("<script>");
    expect(decodeAccess({ ...access, shares_truncated: true }, record).shares_truncated).toBe(true);
  });
  it("binds writes to tenant/target and exact version without leaking credentials in URLs", async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(response(entry));
    const c = administrationClient("/api", "synthetic-secret", tenant, transport);
    await c.saveTeam({ id: team, name: "Finance", parent_id: "" }, 1);
    const [url, init] = transport.mock.calls[0];
    expect(url).toBe(`/api/v1/tenants/${tenant}/teams`);
    expect(init).toMatchObject({ credentials: "omit", method: "POST", signal: expect.any(AbortSignal) });
    expect(init?.headers).toMatchObject({ Authorization: "Bearer synthetic-secret" });
    expect(JSON.parse(String(init?.body))).toEqual({ id: team, name: "Finance", parent_id: "", expected_version: 1 });
    await expect(c.team("../teams")).rejects.toThrow();
    expect(transport).toHaveBeenCalledTimes(1);
  });
  it.each([true, false, null])("sends an observed %j membership witness without coercion", async (expected) => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(response({ user_id: user, active: !expected }));
    await administrationClient("/api", "token", tenant, transport).setMember(team, user, !expected, expected);
    expect(JSON.parse(String(transport.mock.calls[0][1]?.body))).toEqual({ active: !expected, expected_active: expected });
  });
  it.each([400, 401, 403, 404, 409])("preserves definitive %i rejection and redacts the body", async (status) => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response("private driver value", { status }));
    const error = await administrationClient("/api", "token", tenant, transport).saveTeam({ id: team, name: "Finance", parent_id: "" }, 1).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(APIError); expect(String(error)).not.toContain("private driver"); expect(transport).toHaveBeenCalledTimes(1);
  });
  it.each(["network", "503", "invalid-json", "wrong-id", "wrong-version"])("retains uncertainty after %s, without retry", async (fault) => {
    const transport = vi.fn<typeof fetch>().mockImplementation(async () => {
      if (fault === "network") throw new TypeError("private transport value");
      if (fault === "503") return new Response("private error", { status: 503 });
      if (fault === "invalid-json") return new Response("invalid");
      return response({ ...entry, ...(fault === "wrong-id" ? { id: user } : { version: 8 }) });
    });
    await expect(administrationClient("/api", "token", tenant, transport).saveTeam({ id: team, name: "Finance", parent_id: "" }, 1)).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
    expect(transport).toHaveBeenCalledTimes(1);
  });
  it("binds record access to an exact record/version and keeps owner-only reads separate", async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(response({ records: [{ id: record, version: 1, owner_user_id: user, values: {} }], next: "" })).mockResolvedValueOnce(response(access)).mockResolvedValueOnce(response({ ...access, version: 5 }));
    const c = previewClient("/api", "token", tenant, "app.contacts", 1, transport);
    expect((await c.record("entity.contact", record, ["field.contact.id"])).owner_user_id).toBe(user);
    expect(String(transport.mock.calls[0][0])).toMatch(/\/query$/);
    expect(JSON.parse(String(transport.mock.calls[0][1]?.body)).record_id).toBe(record);
    expect((await c.access("entity.contact", record)).record_id).toBe(record);
    await expect(c.changeAccess("entity.contact", record, { operation: "share", user_id: user, expected_version: 2, can_write: false })).rejects.toBeInstanceOf(AdministrationOutcomeUnknown);
    expect(transport).toHaveBeenCalledTimes(3);
  });
});
