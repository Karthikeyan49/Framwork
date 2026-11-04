import { useEffect, useMemo, useRef, useState } from "react";
import { APIError } from "../api/client";
import { administrationMessage } from "../api/administration";
import { rolesClient, type RolesClient } from "../api/roles";
import { useConnectionTransport } from "../auth/Session";
import { config } from "../config";
import { uuid } from "../preview/contracts";
import type { AdministrationIntent, AdministrationPort } from "./Recovery";
import { compileDraft, draftGrants, RoleEditor, type GrantDraft } from "./RoleEditor";
import { prepareRolePublication, roleDefinitionHash, roleName, type RoleContract, type RoleMember, type RoleSummary, type RoleVersion } from "./roleContracts";
import { useChange } from "./useChange";

export const roleScope = (tenant: string, name: string) => `role:${tenant}:${name}`;
export const assignmentScope = (tenant: string, user: string) => `role-member:${tenant}:${user}`;
export function publicationMatches(intent: AdministrationIntent, value: RoleVersion) {
  return intent.kind === "role.publish" && intent.resource === value.name && intent.operationID === value.operation_id && intent.expectedVersion !== undefined && intent.expectedVersion + 1 === value.version && intent.expectedHash === value.hash && intent.expectedActive === value.active && !value.bootstrap;
}
export function RolesPanel({ token, tenant, actor, recovery, blocked, onSelfAssignment }: { token: string; tenant: string; actor: string; recovery: AdministrationPort; blocked: boolean; onSelfAssignment: () => void }) {
  const transport = useConnectionTransport(token);
  const client = useMemo(() => rolesClient(config.apiBaseURL, token, tenant, transport), [token, tenant, transport]);
  const [selection, setSelection] = useState(""); const [name, setName] = useState("");
  const pending = Object.values(recovery.pending).filter((p) => p.tenant === tenant && p.kind.startsWith("role."));
  return <section className="card administration" aria-label="Role administration"><h2>Roles and assignments</h2><p>Tenant roles are live server policy. Publishing a version changes access for every assigned member. Token role claims do not grant access.</p>
    {pending.length > 0 && <section aria-label="Pending role changes"><h3>Changes requiring review</h3>{pending.map((p) => <p key={p.scope}>{p.kind} · <code>{p.resource}</code>{p.operationID && <> · Operation <code>{p.operationID}</code> · Expected hash <code>{p.expectedHash}</code></>}{p.kind === "role.publish" && <button type="button" disabled={blocked} onClick={() => { setName(p.resource); setSelection(p.resource); }}>Inspect role {p.resource}</button>}</p>)}</section>}
    <RoleDirectory client={client} disabled={blocked} select={(value) => { setSelection(value); setName(value); }}/>
    <form aria-label="Inspect role" onSubmit={(e) => { e.preventDefault(); if (!blocked && roleName(name)) setSelection(name); }}><label>Role name<input value={name} maxLength={64} pattern="[a-z][a-z0-9_]{0,63}" required disabled={blocked} onChange={(e) => setName(e.target.value)}/></label><button disabled={blocked || !roleName(name)}>Inspect or prepare role</button></form>
    {selection && <RoleDefinitionPanel key={selection} name={selection} client={client} tenant={tenant} recovery={recovery} blocked={blocked}/>}
    <RoleAssignmentPanel client={client} tenant={tenant} actor={actor} recovery={recovery} blocked={blocked} onSelfAssignment={onSelfAssignment}/>
  </section>;
}
function RoleDirectory({ client, disabled, select }: { client: RolesClient; disabled: boolean; select: (name: string) => void }) {
  const [page, setPage] = useState<{ client: RolesClient; roles: RoleSummary[]; next: string }>(); const [after, setAfter] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [settled, setSettled] = useState<{ client: RolesClient; after: string; attempt: number; error: string }>();
  useEffect(() => {
    const controller = new AbortController();
    client.list(after, controller.signal).then((value) => { if (!controller.signal.aborted) { setPage({ client, ...value }); setSettled({ client, after, attempt, error: "" }); } }).catch((e) => { if (!controller.signal.aborted) setSettled({ client, after, attempt, error: administrationMessage(e) }); });
    return () => controller.abort();
  }, [client, after, attempt]);
  const loading = settled?.client !== client || settled.after !== after || settled.attempt !== attempt;
  const error = loading ? "" : settled.error;
  const current = !loading && !error && page?.client === client ? page : undefined;
  return <section aria-label="Role directory"><h3>Current roles</h3><button type="button" disabled={disabled || loading} onClick={() => { setAfter(""); setAttempt((n) => n + 1); }}>Reload role directory</button>{loading && <p role="status">Loading roles…</p>}{error && <p role="alert">{error}</p>}{current && <><div className="table-scroll"><table><caption>Role directory page</caption><thead><tr><th>Role</th><th>Version</th><th>State</th><th>Selection</th></tr></thead><tbody>{current.roles.map((role) => <tr key={role.name}><td>{role.name}</td><td>{role.version}</td><td>{role.active ? "Active" : "Inactive"} · {role.bootstrap ? "Native bootstrap" : "Configured"}</td><td><button type="button" disabled={disabled} onClick={() => select(role.name)}>Inspect {role.name}</button></td></tr>)}</tbody></table></div>{!current.roles.length && <p>No roles on this page.</p>}{current.next && <button type="button" disabled={disabled || loading} onClick={() => setAfter(current.next)}>Next role page</button>}<p>Each page contains at most 100 roles. Use an exact role name to inspect any role.</p></>}</section>;
}
interface RoleSnapshot { client: RolesClient; contract: RoleContract; role?: RoleVersion }
async function readRole(client: RolesClient, name: string, signal: AbortSignal): Promise<RoleSnapshot> {
  const contract = await client.contract(name, signal);
  let role: RoleVersion | undefined;
  try { role = await client.current(name, signal); } catch (e) { if (!(e instanceof APIError && e.status === 404)) throw e; }
  return { client, contract, role };
}
function RoleDefinitionPanel({ name, client, tenant, recovery, blocked }: { name: string; client: RolesClient; tenant: string; recovery: AdministrationPort; blocked: boolean }) {
  const [snapshot, setSnapshot] = useState<RoleSnapshot>(); const [grants, setGrants] = useState<GrantDraft[]>([]); const [active, setActive] = useState(false);
  const [confirmed, setConfirmed] = useState(false); const [bootstrapConfirmed, setBootstrapConfirmed] = useState(false);
  const [loading, setLoading] = useState(true); const [preparation, setPreparation] = useState<{ client: RolesClient; key: string }>(); const [error, setError] = useState(""); const [notice, setNotice] = useState("");
  const request = useRef<AbortController | undefined>(undefined); const lifetime = useRef<AbortController | undefined>(undefined); const preparingLock = useRef<{ client: RolesClient; key: string } | undefined>(undefined);
  const change = useChange(recovery, client); const scope = roleScope(tenant, name); const pending = recovery.pending[scope];
  const current = snapshot?.client === client ? snapshot : undefined;
  const preparing = preparation?.client === client;
  const disabled = blocked || change.busy || preparing || loading;
  async function load() {
    request.current?.abort(); const controller = new AbortController(); request.current = controller;
    setLoading(true); setSnapshot(undefined); setConfirmed(false); setBootstrapConfirmed(false); setError(""); change.clearError();
    try {
      const value = await readRole(client, name, controller.signal);
      if (!controller.signal.aborted) { setSnapshot(value); setGrants(value.role ? draftGrants(value.role.definition) : []); setActive(value.role?.active ?? false); }
    } catch (e) { if (!controller.signal.aborted) setError(administrationMessage(e)); }
    finally { if (!controller.signal.aborted) setLoading(false); }
  }
  useEffect(() => {
    const controller = new AbortController(), alive = new AbortController(); lifetime.current = alive; request.current = controller;
    readRole(client, name, controller.signal).then((value) => { if (!controller.signal.aborted) { setSnapshot(value); setGrants(value.role ? draftGrants(value.role.definition) : []); setActive(value.role?.active ?? false); setConfirmed(false); setBootstrapConfirmed(false); setError(""); } }).catch((e) => { if (!controller.signal.aborted) setError(administrationMessage(e)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => { alive.abort(); controller.abort(); request.current?.abort(); };
  }, [client, name]);
  async function publish() {
    if (!current || disabled || pending || !confirmed || current.contract.root_protected || current.role?.bootstrap && !bootstrapConfirmed || preparingLock.current?.client === client) return;
    const alive = lifetime.current!; const captured = current; const key = crypto.randomUUID();
    preparingLock.current = { client, key }; setPreparation({ client, key }); setError(""); setNotice(""); recovery.busy(key, true);
    const releasePreparation = () => recovery.busy(key, false);
    alive.signal.addEventListener("abort", releasePreparation, { once: true });
    try {
      const prepared = await prepareRolePublication(compileDraft(name, grants, current.contract), current.contract, current.role?.version ?? 0, active);
      if (alive.signal.aborted) return;
      const result = await change.run({ scope, kind: "role.publish", tenant, resource: name, expectedVersion: prepared.expectedVersion, operationID: prepared.operationID, expectedHash: prepared.hash, expectedActive: prepared.active }, (signal) => client.publish(prepared, signal));
      if (!result || alive.signal.aborted) return;
      setSnapshot(undefined); setConfirmed(false); setBootstrapConfirmed(false);
      if (result.ok) { setNotice(`Role ${name} version ${result.value.version} saved. Reload to review the current head before another publication.`); }
    } catch (e) { if (!alive.signal.aborted) { setSnapshot(captured); setError(e instanceof Error ? e.message : "Invalid role definition"); } }
    finally {
      alive.signal.removeEventListener("abort", releasePreparation); releasePreparation();
      if (preparingLock.current?.key === key) preparingLock.current = undefined;
      if (!alive.signal.aborted) setPreparation((old) => old?.key === key ? undefined : old);
    }
  }
  async function receipt(intent: AdministrationIntent) {
    if (!intent.operationID || disabled) return;
    request.current?.abort(); const controller = new AbortController(); request.current = controller; setLoading(true); setError(""); setSnapshot(undefined);
    try {
      const value = await client.operation(intent.operationID, controller.signal);
      if (!publicationMatches(intent, value) || await roleDefinitionHash(value.definition) !== intent.expectedHash) throw new Error("Publication receipt does not match the recorded intent");
      if (!controller.signal.aborted) { recovery.remember(intent.scope); setNotice(`Publication committed as version ${value.version}. Its receipt does not assert the current head; reload the role.`); }
    } catch (e) { if (!controller.signal.aborted) setError(e instanceof APIError ? administrationMessage(e) + " The publication outcome remains unproven." : "The receipt could not be verified. The publication outcome remains unproven."); }
    finally { if (!controller.signal.aborted) setLoading(false); }
  }
  let validation = "";
  if (current && !current.contract.root_protected) { try { compileDraft(name, grants, current.contract); } catch (e) { validation = e instanceof Error ? e.message : "Invalid role definition"; } }
  return <section aria-label="Selected role"><h3>Role {name}</h3><button type="button" disabled={disabled} onClick={() => void load()}>Reload selected role</button>{loading && <p role="status">Loading role…</p>}{error && <p role="alert">{error}</p>}{change.error && <p role="alert">{change.error}</p>}{notice && <p role="status">{notice}</p>}
    {pending && <div><p role="alert">This publication has an uncertain outcome. Do not repeat it automatically.</p><p>Operation <code>{pending.operationID}</code> · Expected hash <code>{pending.expectedHash}</code></p><button type="button" disabled={disabled || pending.status === "dispatched"} onClick={() => void receipt(pending)}>Check publication receipt</button><button type="button" disabled={disabled || pending.status === "dispatched" || !current} onClick={() => { recovery.remember(scope); setConfirmed(false); setBootstrapConfirmed(false); setNotice("Current role state reviewed. The earlier outcome remains unproven; only publish a new intent if needed."); }}>Use reviewed role state</button></div>}
    {current && <>{current.role ? <p>Observed version {current.role.version} · {current.role.active ? "Active" : "Inactive"} · Hash <code>{current.role.hash}</code></p> : <p>No role exists at the authorized point read. A new role will use expected version 0.</p>}
      {current.contract.root_protected ? <p>The owner role is protected and read-only. SQL preserves the last active owner.</p> : <form aria-label="Publish role" onSubmit={(e) => { e.preventDefault(); void publish(); }}><fieldset disabled={disabled || !!pending}><legend>Role publication</legend>{current.role?.bootstrap && <p role="note">Native bootstrap role: its empty stored grants currently use built-in permissions. Publishing replaces those permissions for every assigned member; empty configured grants deny all.</p>}
        <RoleEditor value={grants} contract={current.contract} disabled={disabled || !!pending} onChange={(value) => { setGrants(value); setConfirmed(false); setBootstrapConfirmed(false); }}/><label><input type="checkbox" checked={active} onChange={(e) => { setActive(e.target.checked); setConfirmed(false); setBootstrapConfirmed(false); }}/>Role active</label>{validation && <p role="alert">{validation}</p>}
        {current.role?.bootstrap && <label><input type="checkbox" checked={bootstrapConfirmed} onChange={(e) => setBootstrapConfirmed(e.target.checked)}/>I understand this replaces native bootstrap permissions</label>}<label><input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)}/>I reviewed the grants, scopes and impact on all assigned members</label><button disabled={disabled || !!pending || !!validation || !confirmed || !!current.role?.bootstrap && !bootstrapConfirmed}>{preparing || change.busy ? "Publishing…" : "Publish role version"}</button></fieldset></form>}
    </>}
  </section>;
}
export function RoleAssignmentPanel({ client, tenant, actor, recovery, blocked, onSelfAssignment }: { client: RolesClient; tenant: string; actor: string; recovery: AdministrationPort; blocked: boolean; onSelfAssignment: () => void }) {
  const [user, setUser] = useState(""); const [desired, setDesired] = useState(""); const [observed, setObserved] = useState<{ client: RolesClient; member: RoleMember; role?: RoleVersion }>();
  const [loading, setLoading] = useState(false); const [error, setError] = useState(""); const [notice, setNotice] = useState(""); const [confirmed, setConfirmed] = useState(false);
  const request = useRef<AbortController | undefined>(undefined); const lifetime = useRef<AbortController | undefined>(undefined); const change = useChange(recovery, client);
  const current = observed?.client === client && observed.member.user_id === user && (!observed.role || observed.role.name === desired) ? observed : undefined;
  const pending = uuid(user) ? recovery.pending[assignmentScope(tenant, user)] : undefined; const disabled = blocked || change.busy || loading;
  useEffect(() => { const controller = new AbortController(); lifetime.current = controller; return () => { controller.abort(); request.current?.abort(); }; }, [client]);
  function invalidate() { request.current?.abort(); setLoading(false); setObserved(undefined); setConfirmed(false); setError(""); change.clearError(); }
  async function inspect() {
    if (disabled || !uuid(user) || desired && !roleName(desired)) return;
    request.current?.abort(); const controller = new AbortController(); request.current = controller; setLoading(true); setObserved(undefined); setConfirmed(false); setError(""); change.clearError();
    try {
      const member = await client.member(user, controller.signal); const role = desired ? await client.current(desired, controller.signal) : undefined;
      if (!controller.signal.aborted) setObserved({ client, member, role });
    } catch (e) { if (!controller.signal.aborted) setError(administrationMessage(e)); }
    finally { if (!controller.signal.aborted) setLoading(false); }
  }
  async function assign() {
    if (disabled || pending || !current?.role?.active || !current.member.active || !confirmed || current.member.role === current.role.name) return;
    const captured = current, alive = lifetime.current!; setError(""); setNotice("");
    const result = await change.run({ scope: assignmentScope(tenant, user), kind: "role.assign", tenant, resource: user, target: captured.role!.name, expectedRole: captured.member.role }, (signal) => client.assign(captured.member.user_id, captured.role!.name, captured.member.role, signal));
    if (!result || alive.signal.aborted) return;
    setObserved(undefined); setConfirmed(false);
    if (result.ok) { setNotice("Role assignment saved. Reload current membership before any further change."); if (result.value.user_id === actor) onSelfAssignment(); }
  }
  const pendingAssignments = Object.values(recovery.pending).filter((p) => p.tenant === tenant && p.kind === "role.assign");
  return <section aria-label="Role assignment"><h3>Member role assignment</h3><p>Inspect the exact member ID, including members outside the first directory page. Assignment checks the observed role label; a label that changed away and back is not detected.</p>
    {pendingAssignments.map((p) => <p key={p.scope}>Uncertain assignment · <code>{p.resource}</code> → {p.target}<button type="button" disabled={disabled} onClick={() => { invalidate(); setUser(p.resource); setDesired(p.target ?? ""); }}>Inspect member {p.resource}</button></p>)}
    <form aria-label="Inspect role assignment" onSubmit={(e) => { e.preventDefault(); void inspect(); }}><label>Assignment member ID<input value={user} required maxLength={36} disabled={disabled} onChange={(e) => { invalidate(); setUser(e.target.value); }}/></label><label>Desired role name<input value={desired} maxLength={64} disabled={disabled} onChange={(e) => { invalidate(); setDesired(e.target.value); }}/></label><button disabled={disabled || !uuid(user) || !!desired && !roleName(desired)}>Inspect member and role</button></form>
    {loading && <p role="status">Loading assignment state…</p>}{error && <p role="alert">{error}</p>}{change.error && <p role="alert">{change.error}</p>}{notice && <p role="status">{notice}</p>}
    {current && <><p>Member <code>{current.member.user_id}</code> · Current role {current.member.role} · Membership {current.member.active ? "active" : "inactive"}</p>{current.role && <p>Desired role {current.role.name} · version {current.role.version} · {current.role.active ? "active" : "inactive"}. Its definition can change under live policy.</p>}{pending && <><p role="alert">The previous assignment outcome remains unproven. Review the fresh membership before starting a new intent.</p><button type="button" disabled={disabled || pending.status === "dispatched"} onClick={() => { recovery.remember(pending.scope); setConfirmed(false); setNotice("Current membership reviewed. The earlier outcome is not proven; make a new assignment only if needed."); }}>Use reviewed membership state</button></>}
      {user === actor && <p role="note">This changes your own role and may remove your administrative or workspace access.</p>}{desired === "owner" && <p role="note">The owner role grants protected administrative authority.</p>}<label><input type="checkbox" checked={confirmed} disabled={disabled || !!pending} onChange={(e) => setConfirmed(e.target.checked)}/>I reviewed this assignment and its access impact{user === actor ? ", including my own access" : ""}</label><button type="button" disabled={disabled || !!pending || !confirmed || !current.member.active || !current.role?.active || current.member.role === current.role.name} onClick={() => void assign()}>Assign role</button>
    </>}
  </section>;
}
