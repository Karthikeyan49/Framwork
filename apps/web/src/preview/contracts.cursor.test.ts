import { describe, expect, it } from "vitest";
import { decodeResult } from "./contracts";

describe("long text sort cursors", () => {
  it("accepts the actual escaped JSON shape of a maximum-length text key", () => {
    const cursor = `{"id":"11111111-1111-4111-8111-111111111111","value":"${"\\u0026".repeat(4096)}","fingerprint":"${"a".repeat(64)}"}`;
    const next = btoa(cursor).replace(/=/g, "").replace(/\+/g, "-").replace(/\//g, "_");
    expect(next.length).toBeGreaterThan(2048);
    expect(decodeResult({ records: [], next }).next).toBe(next);
  });
  it("retains short legacy cursors and enforces the encoded budget", () => {
    expect(decodeResult({ records: [], next: "legacy_cursor" }).next).toBe("legacy_cursor");
    expect(() => decodeResult({ records: [], next: "A".repeat(43692) })).toThrow("Invalid records");
  });
});
