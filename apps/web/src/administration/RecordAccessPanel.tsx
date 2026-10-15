import { useEffect, useMemo, useRef, useState } from "react";
import { administrationClient, administrationMessage } from "../api/administration";
import type { previewClient } from "../api/preview";
import { config } from "../config";
import { uuid, type Entity } from "../preview/contracts";
import type { RecordAccess } from "./contracts";
import type { AdministrationPort } from "./Recovery";
import { useChange } from "./useChange";
import { MemberSuggestions } from "./MemberSuggestions";

type Client = ReturnType<typeof previewClient>;
export const recordScope = (tenant: string, app: string, revision: number, hash: string, entity: string, id: string) => `record:${tenant}:${app}:${revision}:${hash}:${entity}:${id}`;
async function readAccess(client: Client, entity: Entity, record: string, mode: "owner" | "shares", signal: AbortSignal): Promise<RecordAccess> {
  if (mode === "shares") return client.access(entity.id, record, signal);
  const projection = [entity.fields.find((f) => f.core_id)?.id ?? entity.fields[0].id];
  const row = await client.record(entity.id, record, projection, signal);
  if (!uuid(row.owner_user_id)) throw new Error("Ownership header missing");
  return { record_id: row.id, version: row.version, owner_user_id: row.owner_user_id, owner_team_id: row.owner_team_id ?? "", shares: [], shares_truncated: false };
}
export function RecordAccessPanel({ client, token, tenant, entity, record, scope, mode, recovery, onBusy, onInvalidated, onClose }: {
  client: Client; token: string; tenant: string; entity: Entity; record: string; scope: string; mode: "owner" | "shares";
  recovery: AdministrationPort; onBusy: (busy: boolean) => void; onInvalidated: () => void; onClose: () => void;
}) {
  const directory = useMemo(() => administrationClient(config.apiBaseURL, token, tenant), [token, tenant]);
  const [loaded, setLoaded] = useState<{ client: Client; view: RecordAccess }>();
  const [owner, setOwner] = useState(""); const [team, setTeam] = useState("");
  const [recipient, setRecipient] = useState(""); const [canWrite, setCanWrite] = useState(false); const [acknowledged, setAcknowledged] = useState(false);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(""); const [notice, setNotice] = useState(""); const [reviewable, setReviewable] = useState(false);
  const request = useRef<AbortController | undefined>(undefined);
  const change = useChange(recovery);
  const view = loaded?.client === client ? loaded.view : undefined;
  const pending = recovery.pending[scope];
  const disabled = loading || change.busy || !!pending;
  const reviewMatches = !pending || pending.kind === "record.owner" || mode === "shares";
  const shareTargetObservable = !pending || pending.kind === "record.owner" ||
    !!view && uuid(pending.target) && (!view.shares_truncated || view.shares.some((share) => share.user_id === pending.target));
  function accept(value: RecordAccess) {
    setLoaded({ client, view: value }); setOwner(value.owner_user_id); setTeam(value.owner_team_id); setRecipient(""); setCanWrite(false); setAcknowledged(false); setReviewable(true);
  }
  async function reload() {
    change.clearError();
    request.current?.abort(); const controller = new AbortController(); request.current = controller;
    setLoading(true); setError(""); setLoaded(undefined); setReviewable(false); setAcknowledged(false);
    try { const value = await readAccess(client, entity, record, mode, controller.signal); if (!controller.signal.aborted) accept(value); }
    catch (e) { if (!controller.signal.aborted) setError(administrationMessage(e)); }
    finally { if (!controller.signal.aborted) setLoading(false); }
  }
  useEffect(() => {
    const controller = new AbortController(); request.current = controller;
    const read = async () => {
      try {
        const value = await readAccess(client, entity, record, mode, controller.signal);
        if (!controller.signal.aborted) { setLoaded({ client, view: value }); setOwner(value.owner_user_id); setTeam(value.owner_team_id); setReviewable(true); }
      } catch (e) { if (!controller.signal.aborted) setError(administrationMessage(e)); }
      finally { if (!controller.signal.aborted) setLoading(false); }
    };
    void read();
    return () => { controller.abort(); request.current?.abort(); };
  }, [client, entity, mode, record]);
  async function submit(operation: "owner" | "share" | "unshare", user: string) {
    if (!view || disabled || !uuid(user) || operation === "owner" && (!acknowledged || team !== "" && !uuid(team))) return;
    setNotice(""); setError(""); setReviewable(false); onInvalidated(); onBusy(true);
    try {
      const result = await change.run({ scope, tenant, resource: record, target: user, expectedVersion: view.version, kind: `record.${operation}` }, (signal) => client.changeAccess(entity.id, record, {
        operation, expected_version: view.version, user_id: user,
        ...(operation === "owner" ? { team_id: team } : operation === "share" ? { can_write: canWrite } : {}),
      }, signal));
      if (!result) return;
      setLoaded(undefined); setAcknowledged(false);
      if (result.ok) { setNotice(`Access change saved · version ${result.value.version}.`); await reload(); }
    } finally { onBusy(false); }
  }
  return <section className="card administration" aria-label={mode === "owner" ? "Record ownership" : "Record sharing"}>
    <h3>{mode === "owner" ? "Record ownership" : "Record sharing"}</h3><p>Record <code>{record}</code></p>
    <div className="actions"><button type="button" disabled={change.busy || loading} onClick={() => void reload()}>Reload record access</button><button type="button" disabled={change.busy} onClick={onClose}>Back to records</button></div>
    {loading && <p role="status">Loading record access…</p>}
    {error && <p role="alert">{error}</p>}{change.error && <p role="alert">{change.error}</p>}{notice && <p role="status">{notice}</p>}
    {pending && <section>{pending.status === "dispatched" ? <p role="status">An access change is in progress. Wait for its result before another change.</p> : <p role="alert">An access change has an uncertain outcome. Review the current state before a new change. If access is unavailable, ask an authorized administrator to inspect it.</p>}{!reviewMatches && <p>Open record sharing to review this share change.</p>}{reviewMatches && view && !shareTargetObservable && <p>The pending recipient is outside this share window. Keep the change blocked until an authorized read shows that recipient or returns a complete list; ask an authorized administrator for help.</p>}<button type="button" disabled={!view || !reviewable || !reviewMatches || !shareTargetObservable || pending.status === "dispatched" || loading || change.busy} onClick={() => { recovery.remember(scope); setNotice("Current state reviewed. The earlier request's outcome is not proven; make a new change only if needed."); }}>Use reviewed record state</button></section>}
    {view && <><p>Version {view.version} · Owner <code>{view.owner_user_id}</code> · Team <code>{view.owner_team_id || "None"}</code></p>
      {mode === "owner" ? <form aria-label="Change record ownership" onSubmit={(e) => { e.preventDefault(); void submit("owner", owner); }}>
        <label>Owner user ID<input required pattern="[0-9a-f-]{36}" value={owner} disabled={disabled} onChange={(e) => { setOwner(e.target.value); setAcknowledged(false); }} /></label>
        <label>Owner team ID<input pattern="[0-9a-f-]{36}" value={team} disabled={disabled} onChange={(e) => { setTeam(e.target.value); setAcknowledged(false); }} placeholder="Leave empty for no owner team" /></label>
        <label className="check"><input type="checkbox" checked={acknowledged} disabled={disabled} onChange={(e) => setAcknowledged(e.target.checked)} />I understand that changing ownership can remove my access.</label>
        <button disabled={disabled || !acknowledged || !uuid(owner) || owner === view.owner_user_id && team === view.owner_team_id}>Transfer ownership</button>
      </form> : <>
        <p>Sharing does not change the recipient’s workspace permissions.</p>
        {view.shares_truncated && <p role="status">Only the first 256 shares are shown. Other shares may exist; revoke them by user ID and reload. An unlisted user may still have access.</p>}
        <ul aria-label="Explicit record shares">{view.shares.map((s) => <li key={s.user_id}><code>{s.user_id}</code> · {s.can_write ? "Read and write" : "Read only"} <button type="button" disabled={disabled} onClick={() => void submit("unshare", s.user_id)}>Revoke share for {s.user_id}</button></li>)}</ul>
        {view.shares.length === 0 && !view.shares_truncated && <p>No explicit shares.</p>}
        <form aria-label="Grant record share" onSubmit={(e) => { e.preventDefault(); void submit("share", recipient); }}><label>Recipient user ID<input required pattern="[0-9a-f-]{36}" value={recipient} disabled={disabled} onChange={(e) => setRecipient(e.target.value)} /></label><label className="check"><input type="checkbox" checked={canWrite} disabled={disabled} onChange={(e) => setCanWrite(e.target.checked)} />Allow writes through this share</label><button disabled={disabled || !uuid(recipient)}>Save share</button><button type="button" disabled={disabled || !uuid(recipient)} onClick={() => void submit("unshare", recipient)}>Revoke by user ID</button></form>
      </>}
    </>}
    <MemberSuggestions client={directory} />
  </section>;
}
