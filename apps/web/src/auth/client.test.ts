import { expect, it, vi } from "vitest";
import { captureLoginReturn, loginClient } from "./client";
const state = "A".repeat(43);
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
it("scrubs all callback parameters and rejects duplicate, malformed, implicit and error returns", () => {
  const history = { replaceState: vi.fn() };
  const valid = captureLoginReturn({ pathname: "/auth/callback", search: `?code=private-code&state=${state}&iss=https%3A%2F%2Fid.example`, hash: "" }, history);
  expect(valid).toEqual({ kind: "code", code: "private-code", state, issuer: "https://id.example" });
  expect(history.replaceState).toHaveBeenLastCalledWith(null, "", "/auth/callback");
  for (const query of [`?code=a&code=b&state=${state}`, `?code=a&state=${state}&state=${state}`, `?error=access_denied&error_description=PRIVATE&state=${state}`, `?code=a&state=${state}&next=https://other.example`, "?code=&state=bad", `?code=${"x".repeat(4097)}&state=${state}`, `?code=a%00&state=${state}`]) {
    expect(captureLoginReturn({ pathname: "/auth/callback", search: query, hash: "" }, history)).toEqual({ kind: "error" });
    expect(history.replaceState).toHaveBeenLastCalledWith(null, "", "/auth/callback");
  }
  expect(captureLoginReturn({ pathname: "/auth/callback", search: `?code=a&state=${state}`, hash: "#access_token=PRIVATE" }, history)).toEqual({ kind: "error" });
  history.replaceState.mockClear(); expect(captureLoginReturn({ pathname: "/access", search: "", hash: "" }, history)).toBeUndefined(); expect(history.replaceState).not.toHaveBeenCalled();
});
it("uses browser-bound JSON POSTs without a bearer, retry, URL code, or browser storage", async () => {
  const storage = vi.spyOn(Storage.prototype, "setItem");
  const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(response({ enabled: true, manual_token: false }))
    .mockResolvedValueOnce(response({ authorization_url: `https://id.example/authorize?response_type=code&state=${state}&code_challenge_method=S256&code_challenge=${state}&nonce=${state}&client_id=web&scope=openid&redirect_uri=${encodeURIComponent(window.location.origin + "/auth/callback")}` }))
    .mockResolvedValueOnce(response({ access_token: "synthetic.signature.token", token_type: "Bearer" }));
  const client = loginClient("/api", transport);
  expect(await client.options()).toEqual({ enabled: true, manual_token: false });
  expect(await client.start()).toContain("https://id.example/authorize");
  expect(await client.complete({ kind: "code", code: "PRIVATE-code", state, issuer: "https://id.example" })).toBe("synthetic.signature.token");
  expect(transport).toHaveBeenCalledTimes(3);
  for (const call of transport.mock.calls.slice(1)) { expect(call[1]?.credentials).toBe("same-origin"); expect(call[1]?.method).toBe("POST"); expect(call[1]?.headers).not.toHaveProperty("Authorization"); }
  expect(JSON.stringify(transport.mock.calls.map((v) => v[0]))).not.toContain("PRIVATE"); expect(storage).not.toHaveBeenCalled();
});
it("fails cross-origin, malformed or failed flows without repeating completion", async () => {
  const transport = vi.fn<typeof fetch>();
  await expect(loginClient("https://other.example/api", transport).start()).rejects.toThrow("same origin"); expect(transport).not.toHaveBeenCalled();
  for (const value of [{ access_token: "PRIVATE", token_type: "Bearer" }, { access_token: "a.b.c", token_type: "ID" }, { error: "PRIVATE provider response" }]) {
    transport.mockReset().mockResolvedValue(response(value));
    await expect(loginClient("/api", transport).complete({ kind: "code", code: "PRIVATE-code", state, issuer: "" })).rejects.toThrow("Invalid sign-in response"); expect(transport).toHaveBeenCalledTimes(1);
  }
  transport.mockReset().mockResolvedValue(response({ error: "PRIVATE" }, 503));
  await expect(loginClient("/api", transport).complete({ kind: "code", code: "PRIVATE-code", state, issuer: "" })).rejects.not.toThrow("PRIVATE"); expect(transport).toHaveBeenCalledTimes(1);
});
