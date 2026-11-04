import { APIError } from "../api/client";

export interface LoginOptions { enabled: boolean; manual_token: boolean }
export type LoginReturn = { kind: "code"; code: string; state: string; issuer: string } | { kind: "error" };
const binding = (v: unknown): v is string => typeof v === "string" && /^[A-Za-z0-9_-]{43}$/.test(v);

// Call before rendering React. Callback credentials must not survive in history,
// referrers, route state, storage or an error message.
export function captureLoginReturn(location: Pick<Location, "pathname" | "search" | "hash">, history: Pick<History, "replaceState">): LoginReturn | undefined {
  if (location.pathname !== "/auth/callback") return;
  const search = location.search; const fragment = location.hash;
  history.replaceState(null, "", "/auth/callback");
  if (search.length > 16384 || fragment !== "") return { kind: "error" };
  const params = new URLSearchParams(search);
  const permitted = new Set(["code", "state", "iss", "session_state", "error", "error_description", "error_uri"]);
  for (const key of params.keys()) if (!permitted.has(key) || params.getAll(key).length !== 1) return { kind: "error" };
  const code = params.get("code"); const state = params.get("state"); const issuer = params.get("iss") ?? "";
  if (params.has("error") || !code || code.length > 4096 || /[\r\n\0]/.test(code) || !binding(state) || issuer.length > 2048) return { kind: "error" };
  return { kind: "code", code, state, issuer };
}

export function loginClient(base: string, transport: typeof fetch = fetch) {
  function sameOrigin() {
    if (new URL(base, window.location.origin).origin !== window.location.origin) throw new Error("Sign-in requires the same origin API gateway");
  }
  async function request(path: string, body?: unknown, signal?: AbortSignal): Promise<unknown> {
    if (body !== undefined) sameOrigin();
    const response = await transport(`${base}/v1/auth/${path}`, {
      method: body === undefined ? "GET" : "POST", credentials: body === undefined ? "omit" : "same-origin",
      redirect: "error",
      headers: { Accept: "application/json", ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(7000)]) : AbortSignal.timeout(7000),
    });
    if (!response.ok) throw new APIError(response.status, response.headers.get("X-Request-ID") ?? undefined);
    return response.json();
  }
  return {
    async options(signal?: AbortSignal): Promise<LoginOptions> {
      const v = await request("provider", undefined, signal);
      if (!v || typeof v !== "object" || !("enabled" in v) || !("manual_token" in v) || typeof v.enabled !== "boolean" || typeof v.manual_token !== "boolean") throw new Error("Invalid sign-in configuration");
      return { enabled: v.enabled, manual_token: v.manual_token };
    },
    async start(signal?: AbortSignal): Promise<string> {
      const v = await request("login/start", {}, signal);
      if (!v || typeof v !== "object" || !("authorization_url" in v) || typeof v.authorization_url !== "string" || v.authorization_url.length > 8192) throw new Error("Invalid sign-in response");
      const url = new URL(v.authorization_url);
      if (url.username || url.password || url.hash || url.protocol !== "https:" && !(url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)) || !binding(url.searchParams.get("state")) || url.searchParams.get("response_type") !== "code" || url.searchParams.get("code_challenge_method") !== "S256") throw new Error("Invalid sign-in response");
      for (const name of ["client_id", "redirect_uri", "response_type", "scope", "state", "nonce", "code_challenge", "code_challenge_method"]) if (url.searchParams.getAll(name).length !== 1) throw new Error("Invalid sign-in response");
      if (!binding(url.searchParams.get("nonce")) || !binding(url.searchParams.get("code_challenge")) || !url.searchParams.get("client_id") || url.searchParams.get("redirect_uri") !== `${window.location.origin}/auth/callback` || !url.searchParams.get("scope")?.split(" ").includes("openid")) throw new Error("Invalid sign-in response");
      return url.toString();
    },
    async complete(callback: LoginReturn): Promise<string> {
      if (callback.kind !== "code") throw new Error("Invalid sign-in return");
      const v = await request("login/complete", { code: callback.code, state: callback.state, ...(callback.issuer ? { issuer: callback.issuer } : {}) });
      if (!v || typeof v !== "object" || !("access_token" in v) || !("token_type" in v) || v.token_type !== "Bearer" || typeof v.access_token !== "string" || v.access_token.length > 8192 || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(v.access_token)) throw new Error("Invalid sign-in response");
      return v.access_token;
    },
  };
}
