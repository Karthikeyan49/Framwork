import { useEffect, useMemo, useRef, useState } from "react";
import { administrationClient, administrationMessage } from "../api/administration";
import { config } from "../config";
import { uuid } from "../preview/contracts";
import type { Team, TeamDetail } from "./contracts";
import type { AdministrationPort } from "./Recovery";
import { useChange } from "./useChange";
import { MemberSuggestions } from "./MemberSuggestions";
import { useConnectionTransport } from "../auth/Session";

export const teamScope = (tenant: string, id: string) => `team:${tenant}:${id}`;
export function TeamsPanel({ token, tenant, recovery }: { token: string; tenant: string; recovery: AdministrationPort }) {
  const transport = useConnectionTransport(token);
  const client = useMemo(() => administrationClient(config.apiBaseURL, token, tenant, transport), [token, tenant, transport]);
  const [list, setList] = useState<{ client: typeof client; teams: Team[] }>();
  const [detail, setDetail] = useState<{ client: typeof client; team: TeamDetail }>();
  const [selected, setSelected] = useState("");
  const [name, setName] = useState(""); const [parent, setParent] = useState("");
  const [newName, setNewName] = useState(""); const [newParent, setNewParent] = useState("");
  const [user, setUser] = useState(""); const [active, setActive] = useState(true);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(""); const [notice, setNotice] = useState("");
  const [reviewable, setReviewable] = useState("");
  const request = useRef<AbortController | undefined>(undefined);
  const change = useChange(recovery);
  const teams = list?.client === client ? list.teams : undefined;
  const team = detail?.client === client ? detail.team : undefined;
  const pending = team ? recovery.pending[teamScope(tenant, team.id)] : undefined;
  const pendingTeams = Object.values(recovery.pending).filter((p) => p.tenant === tenant && p.kind.startsWith("team."));
  const disabled = loading || change.busy;
  async function load(id = "") {
    if (change.busy) return;
    change.clearError();
    request.current?.abort(); const controller = new AbortController(); request.current = controller;
    setLoading(true); setError(""); setDetail(undefined); setReviewable(""); setName(""); setParent(""); setUser(""); setActive(true);
    try {
      if (id) {
        const value = await client.team(id, controller.signal);
        if (!controller.signal.aborted) { setDetail({ client, team: value }); setName(value.name); setParent(value.parent_id); setReviewable(teamScope(tenant, id)); }
      } else {
        setList(undefined);
        const value = await client.teams(controller.signal);
        if (!controller.signal.aborted) setList({ client, teams: value });
      }
    } catch (e) { if (!controller.signal.aborted) setError(administrationMessage(e)); }
    finally { if (!controller.signal.aborted) setLoading(false); }
  }
  useEffect(() => {
    const controller = new AbortController(); request.current = controller;
    client.teams(controller.signal).then((value) => { if (!controller.signal.aborted) setList({ client, teams: value }); })
      .catch((e) => { if (!controller.signal.aborted) setError(administrationMessage(e)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => { controller.abort(); request.current?.abort(); };
  }, [client]);
  async function save(create: boolean) {
    const current = team;
    if (disabled || create && pendingTeams.some((p) => p.kind === "team.create") || !create && (!current || pending)) return;
    const id = create ? crypto.randomUUID() : current!.id;
    const expected = create ? 0 : current!.version;
    const body = { id, name: create ? newName : name, parent_id: create ? newParent : parent };
    setError(""); setNotice(""); setReviewable("");
    const result = await change.run({ scope: teamScope(tenant, id), tenant, resource: id, kind: create ? "team.create" : "team.save", expectedVersion: expected }, (signal) => client.saveTeam(body, expected, signal));
    if (!result) return;
    setDetail(undefined); setList(undefined);
    if (result.ok) { setNotice("Team saved. Reload the team list to see the updated hierarchy."); setNewName(""); setNewParent(""); setSelected(id); await load(id); }
  }
  async function membership(member: string, desired: boolean) {
    if (!team || disabled || pending || !uuid(member)) return;
    const current = team.members.find((m) => m.user_id === member);
    if (current?.active === desired) return;
    setError(""); setNotice(""); setReviewable("");
    const result = await change.run({ scope: teamScope(tenant, team.id), kind: "team.member", tenant, resource: team.id, target: member }, (signal) => client.setMember(team.id, member, desired, current?.active ?? null, signal));
    if (!result) return;
    setDetail(undefined);
    if (result.ok) { setNotice("Membership saved."); await load(team.id); }
  }
  return <section className="card administration" aria-label="Team administration">
    <h2>Teams</h2><p>Create teams, arrange their hierarchy and manage direct memberships. Teams are retained and cannot be deleted.</p>
    <button type="button" disabled={disabled} onClick={() => { setSelected(""); void load(); }}>Reload team list</button>
    {loading && <p role="status">Loading teams…</p>}
    {error && <p role="alert">{error}</p>}{change.error && <p role="alert">{change.error}</p>}{notice && <p role="status">{notice}</p>}
    {pendingTeams.length > 0 && <section aria-label="Pending team changes"><h3>Changes requiring review</h3>{pendingTeams.map((p) => <p key={p.scope}>{p.kind} · <code>{p.resource}</code>{p.target && <> · Member <code>{p.target}</code></>} <button type="button" disabled={disabled} onClick={() => { setSelected(p.resource); void load(p.resource); }}>Inspect team {p.resource}</button></p>)}</section>}
    {teams && <>
      <div className="table-scroll"><table><caption>Workspace team hierarchy</caption><thead><tr><th scope="col">Team</th><th scope="col">Parent</th><th scope="col">Version</th><th scope="col">Selection</th></tr></thead><tbody>{teams.map((t) => <tr key={t.id}><td>{t.name}<small>{t.id}</small></td><td>{teams.find((p) => p.id === t.parent_id)?.name ?? (t.parent_id || "Root")}</td><td>{t.version}</td><td><button type="button" disabled={disabled} onClick={() => { setSelected(t.id); setNotice(""); void load(t.id); }}>Manage {t.name}</button></td></tr>)}</tbody></table>{teams.length === 0 && <p>No teams yet.</p>}</div>
      <form aria-label="Create team" onSubmit={(e) => { e.preventDefault(); void save(true); }}><h3>Create team</h3><label>New team name<input required maxLength={120} value={newName} disabled={disabled} onChange={(e) => setNewName(e.target.value)} /></label><label>New team parent<select value={newParent} disabled={disabled} onChange={(e) => setNewParent(e.target.value)}><option value="">Root</option>{teams.map((t) => <option value={t.id} key={t.id}>{t.name}</option>)}</select></label><button disabled={disabled || !newName.trim() || pendingTeams.some((p) => p.kind === "team.create")}>Create team</button></form>
    </>}
    {selected && <button type="button" disabled={disabled} onClick={() => void load(selected)}>Reload selected team</button>}
    {team && <section aria-label="Selected team"><h3>{team.name}</h3><p>Team ID <code>{team.id}</code> · version {team.version}</p>
      {pending && <div>{pending.status === "dispatched" ? <p role="status">A team change is in progress. Wait for its result before another change.</p> : <p role="alert">A change has an uncertain outcome. Inspect the team and memberships below before starting a new change.</p>}<button type="button" disabled={disabled || pending.status === "dispatched" || reviewable !== pending.scope} onClick={() => { recovery.remember(pending.scope); setNotice("Current state reviewed. The earlier request's outcome is not proven; make a new change only if needed."); }}>Use reviewed team state</button></div>}
      <form aria-label="Edit team" onSubmit={(e) => { e.preventDefault(); void save(false); }}><label>Team name<input required maxLength={120} value={name} disabled={disabled || !!pending} onChange={(e) => setName(e.target.value)} /></label><label>Parent team ID<input value={parent} disabled={disabled || !!pending} pattern="[0-9a-f-]{36}" onChange={(e) => setParent(e.target.value)} placeholder="Leave empty for a root team" /></label><button disabled={disabled || !!pending || name === team.name && parent === team.parent_id}>Save team</button></form>
      <h3>Direct memberships</h3><p>Inactive rows are retained. Hierarchy and membership changes can affect access to records in descendant teams.</p>
      <ul>{team.members.map((m) => <li key={m.user_id}><code>{m.user_id}</code> · {m.active ? "Active" : "Inactive"} <button type="button" disabled={disabled || !!pending} onClick={() => void membership(m.user_id, !m.active)}>{m.active ? "Deactivate" : "Activate"} member {m.user_id}</button></li>)}</ul>
      {team.members.length === 0 && <p>No direct memberships.</p>}
      <form aria-label="Set team membership" onSubmit={(e) => { e.preventDefault(); void membership(user, active); }}><label>Member user ID<input required pattern="[0-9a-f-]{36}" value={user} disabled={disabled || !!pending} onChange={(e) => setUser(e.target.value)} /></label><label>Membership state<select value={String(active)} disabled={disabled || !!pending} onChange={(e) => setActive(e.target.value === "true")}><option value="true">Active</option><option value="false">Inactive</option></select></label><button disabled={disabled || !!pending || !uuid(user) || team.members.find((m) => m.user_id === user)?.active === active}>Save membership</button></form>
    </section>}
    <MemberSuggestions client={client} />
  </section>;
}
