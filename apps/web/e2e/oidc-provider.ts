// Executable conformance fixture, never a production authentication service.
// Subjects/malformed grants are deliberately selectable only in this local test process.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";

if (process.env.APP_ENV !== "test") throw new Error("OIDC fixture requires test mode");
const root = path.resolve(import.meta.dirname, "../../..");
const keyPath = path.join(root, ".local/identity/private.pem");
if ((fs.statSync(keyPath).mode & 0o077) !== 0) throw new Error("Fixture key permissions invalid");
const key = crypto.createPrivateKey(fs.readFileSync(keyPath));
const issuer = "http://localhost:8080/dev-issuer";
const redirect = "http://127.0.0.1:5173/auth/callback";
const client = "browser-client";
interface Pending { state: string; nonce: string; challenge: string; expires: number }
interface Grant extends Pending { subject: string; mode: string }
const pending = new Map<string, Pending>(); const grants = new Map<string, Grant>();
let exchanges = 0; let issued = 0;
function signed(typ: string, claims: Record<string, unknown>) {
  const encode = (v: unknown) => Buffer.from(JSON.stringify(v)).toString("base64url");
  const input = `${encode({ alg: "RS256", typ, kid: "local-v1" })}.${encode(claims)}`;
  return `${input}.${crypto.sign("RSA-SHA256", Buffer.from(input), key).toString("base64url")}`;
}
async function body(request: http.IncomingMessage) {
  let raw = ""; for await (const chunk of request) { raw += chunk.toString(); if (raw.length > 8192) throw new Error("fixture body budget"); } return new URLSearchParams(raw);
}
function json(response: http.ServerResponse, status: number, value: unknown) { response.writeHead(status, { "Content-Type": "application/json" }); response.end(JSON.stringify(value)); }
const server = http.createServer(async (request, response) => {
  response.setHeader("Cache-Control", "no-store"); response.setHeader("Referrer-Policy", "no-referrer");
  try {
    const url = new URL(request.url ?? "/", "http://127.0.0.1:8090");
    if (url.pathname === "/ready") return json(response, 200, { status: "ready" });
    if (url.pathname === "/stats") return json(response, 200, { exchanges, issued });
    if (url.pathname === "/authorize" && request.method === "GET") {
      const p = url.searchParams;
      for (const name of ["state", "nonce", "code_challenge", "code_challenge_method", "response_type", "client_id", "redirect_uri", "scope"]) if (p.getAll(name).length !== 1) throw new Error("fixture parameters");
      if (p.get("client_id") !== client || p.get("redirect_uri") !== redirect || p.get("response_type") !== "code" || p.get("scope") !== "openid" || p.get("code_challenge_method") !== "S256" || pending.size > 100) throw new Error("fixture authorization");
      const id = crypto.randomUUID(); pending.set(id, { state: p.get("state")!, nonce: p.get("nonce")!, challenge: p.get("code_challenge")!, expires: Date.now() + 300000 });
      response.setHeader("Content-Type", "text/html; charset=utf-8");
      response.setHeader("Content-Security-Policy", "default-src 'none'; form-action 'self' http://127.0.0.1:5173; base-uri 'none'");
      response.end(`<!doctype html><html><head><meta name="referrer" content="no-referrer"><title>OIDC conformance fixture</title></head><body><h1>Test identity provider</h1><p>Local conformance fixture only.</p><form action="/consent" method="post"><input type="hidden" name="request_id" value="${id}"><label>Test identity subject<input name="subject" required maxlength="255"></label><label for="fixture-mode">Response mode</label><select id="fixture-mode" name="mode"><option value="valid">Valid</option><option value="within_skew">Within clock tolerance</option><option value="nonce">Wrong nonce</option><option value="id_future">ID token outside clock tolerance</option><option value="provider_error">Exchange failure</option></select><button>Continue with test provider</button></form></body></html>`); return;
    }
    if (url.pathname === "/consent" && request.method === "POST") {
      const form = await body(request); const id = form.get("request_id") ?? ""; const entry = pending.get(id); pending.delete(id);
      const subject = form.get("subject") ?? ""; const mode = form.get("mode") ?? "";
      if (!entry || entry.expires < Date.now() || !subject || subject.length > 255 || !["valid", "within_skew", "nonce", "id_future", "provider_error"].includes(mode)) throw new Error("fixture consent");
      const code = crypto.randomBytes(32).toString("base64url"); grants.set(code, { ...entry, subject, mode });
      const location = new URL(redirect); location.searchParams.set("code", code); location.searchParams.set("state", entry.state); location.searchParams.set("iss", issuer);
      response.writeHead(302, { Location: location.toString() }); response.end(); return;
    }
    if (url.pathname === "/token" && request.method === "POST") {
      exchanges++; const form = await body(request); const code = form.get("code") ?? ""; const entry = grants.get(code); grants.delete(code);
      const challenge = crypto.createHash("sha256").update(form.get("code_verifier") ?? "").digest("base64url");
      if (!entry || entry.expires < Date.now() || challenge !== entry.challenge || form.get("client_id") !== client || form.get("redirect_uri") !== redirect || form.get("grant_type") !== "authorization_code") return json(response, 400, { error: "invalid_grant" });
      if (entry.mode === "provider_error") return json(response, 400, { error: "invalid_grant", error_description: "PRIVATE fixture error" });
      const now = Math.floor(Date.now() / 1000); const issuedAt = now + (entry.mode === "within_skew" ? 20 : 0);
      const access = signed("at+jwt", { iss: issuer, aud: "framework-api", sub: entry.subject, iat: issuedAt, nbf: issuedAt, exp: now + 600, roles: ["owner"], tenant_id: "forged" });
      const digest = crypto.createHash("sha256").update(access).digest().subarray(0, 16).toString("base64url");
      const id = signed("JWT", { iss: issuer, aud: client, sub: entry.subject, iat: issuedAt, exp: now + 600, nbf: entry.mode === "id_future" ? now + 90 : issuedAt, nonce: entry.mode === "nonce" ? "incorrect" : entry.nonce, at_hash: digest });
      issued++; return json(response, 200, { access_token: access, token_type: "Bearer", expires_in: 600, id_token: id, refresh_token: "PRIVATE fixture refresh" });
    }
    json(response, 404, { error: "not_found" });
  } catch { json(response, 400, { error: "invalid_fixture_request" }); }
});
const timer = setInterval(() => { const now = Date.now(); for (const [id, v] of pending) if (v.expires < now) pending.delete(id); for (const [id, v] of grants) if (v.expires < now) grants.delete(id); }, 30000); timer.unref();
server.listen(8090, "127.0.0.1", () => process.stdout.write("OIDC conformance fixture ready.\n"));
server.on("error", () => { process.stderr.write("OIDC fixture could not start.\n"); process.exitCode = 1; });
process.on("SIGTERM", () => { clearInterval(timer); server.close(() => process.exit(0)); server.closeAllConnections(); });
