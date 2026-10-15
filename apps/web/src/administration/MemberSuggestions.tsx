import { useEffect, useId, useRef, useState } from "react";
import { administrationMessage, type administrationClient } from "../api/administration";

type Client = ReturnType<typeof administrationClient>;
export function MemberSuggestions({ client }: { client: Client }) {
  const id = useId();
  const [loaded, setLoaded] = useState<{ client: Client; members: Awaited<ReturnType<Client["members"]>> }>();
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => request.current?.abort(), [client]);
  async function load() {
    request.current?.abort(); const controller = new AbortController(); request.current = controller;
    setBusy(true); setStatus(""); setLoaded(undefined);
    try {
      const members = await client.members(controller.signal);
      if (!controller.signal.aborted) { setLoaded({ client, members }); setStatus("Showing up to 100 workspace member IDs. Enter an ID for anyone not listed."); }
    } catch (e) { if (!controller.signal.aborted) setStatus(administrationMessage(e)); }
    finally { if (!controller.signal.aborted) setBusy(false); }
  }
  return <div className="member-suggestions">
    <button type="button" disabled={busy} onClick={() => void load()}>{busy ? "Loading member suggestions…" : "Load member suggestions"}</button>
    {status && <p role="status">{status}</p>}
    {loaded?.client === client && <ul aria-label="Workspace member suggestions" id={id}>{loaded.members.map((m) => <li key={m.user_id}><code>{m.user_id}</code> · {m.role} · {m.active ? "Active" : "Inactive"}</li>)}</ul>}
  </div>;
}
