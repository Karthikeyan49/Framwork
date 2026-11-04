import { describe, expect, it } from "vitest";
import { decodeRow } from "./contracts";

const row = { id: "11111111-1111-1111-1111-111111111111", version: 1, values: { "field.contact.status": "active" } };

describe("independent R5d decoder review", () => {
  it("retains legacy rows and distinguishes masked from optional missing fields", () => {
    expect(decodeRow(row)).toEqual(row);
    const access = { masked: ["field.contact.name"], writable: ["field.contact.parent", "field.contact.status"] };
    expect(decodeRow({ ...row, field_access: access }).field_access).toEqual(access);
  });
  it.each([
    { masked: ["field.contact.status"], writable: [] },
    { masked: ["field.contact.name"], writable: ["field.contact.name"] },
    { masked: ["field.contact.name", "field.contact.name"], writable: [] },
    { masked: [], writable: ["field.contact.status", "field.contact.status"] },
    { masked: [null], writable: [] },
    { masked: [], writable: [123] },
    { masked: Array.from({ length: 33 }, (_, n) => `field.contact.a_${n}`), writable: [] },
  ])("rejects contradictory or unbounded field access: %j", (field_access) => {
    expect(() => decodeRow({ ...row, field_access })).toThrow("Invalid field access");
  });
});
