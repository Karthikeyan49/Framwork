import { useEffect, useRef, useState } from 'react'
import { APIError, createAPIClient, type Session, type TenantAccess } from './api/client'
import { ApplicationWorkspace } from './preview/Workspace'
import { config } from './config'

export function AccessConsole() {
  const [input, setInput] = useState('')
  const [connection, setConnection] = useState<{ token: string; session: Session }>()
  const [selected, setSelected] = useState('')
  const [access, setAccess] = useState<TenantAccess>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // Recovery contains only owned command identities, never tokens or values.
  // Keep it above the tenant workspace so switching/disconnecting cannot erase
  // an intent whose server outcome is unknown.
  const [commandJournal, setCommandJournal] = useState<Record<string, string>>({})
  const [inFlight, setInFlight] = useState<Record<string, string>>({})
  const actor = connection?.session.user.id ?? ''
  const actorPrefix = actor + ':'
  const pendingCommands = Object.fromEntries(Object.entries(commandJournal)
    .filter(([scope]) => actor !== '' && scope.startsWith(actorPrefix))
    .map(([scope, key]) => [scope.slice(actorPrefix.length), key]))
  const commandBusy = actor !== '' && Object.values(inFlight).includes(actor)
  const login = useRef<AbortController | undefined>(undefined)
  useEffect(() => () => login.current?.abort(), [])
  useEffect(() => {
    if (!connection || !selected) return
    const controller = new AbortController()
    createAPIClient(config.apiBaseURL).access(connection.token, selected, controller.signal).then((value) => {
      if (!controller.signal.aborted) setAccess(value)
    }).catch((e: unknown) => {
      if (controller.signal.aborted) return
      if (e instanceof APIError && e.status === 401) { setConnection(undefined); setSelected(''); setError('Your access token has expired or was revoked. Connect again.') }
      else setError('Tenant access was denied or the service is unavailable.')
    })
    return () => controller.abort()
  }, [connection, selected])
  async function connect() {
    login.current?.abort()
    const controller = new AbortController(); login.current = controller
    const token = input.trim(); setInput(''); setBusy(true); setError('')
    try {
      const session = await createAPIClient(config.apiBaseURL).session(token, controller.signal)
      if (!controller.signal.aborted) { setConnection({ token, session }); setSelected(''); setAccess(undefined) }
    } catch { if (!controller.signal.aborted) setError('Could not authenticate. Check your token and service availability.') }
    finally { if (!controller.signal.aborted) setBusy(false) }
  }
  function disconnect() {
    login.current?.abort(); setConnection(undefined); setAccess(undefined); setSelected(''); setInput(''); setError(''); setBusy(false)
  }
  return <><p className="eyebrow">Identity and tenant access</p><h1>Your workspace access</h1><p className="intro">Connect with an API access token, then choose an authorized workspace. Tokens stay in memory and are cleared when you disconnect or reload.</p><section className="card">
    {!connection ? <form onSubmit={(e) => { e.preventDefault(); void connect() }}><label htmlFor="access-token">API access token</label><input id="access-token" type="password" autoComplete="off" value={input} onChange={(e) => setInput(e.target.value)} required disabled={busy}/><button disabled={busy || !input.trim()}>{busy ? 'Connecting…' : 'Connect'}</button></form> : <><h2>{connection.session.user.name}</h2><label htmlFor="tenant-select">Workspace</label><select id="tenant-select" value={selected} disabled={commandBusy} onChange={(e) => { setSelected(e.target.value); setAccess(undefined); setError('') }}><option value="">Choose a workspace</option>{connection.session.tenants.map((t) => <option value={t.id} key={t.id}>{t.name}</option>)}</select>{connection.session.tenants.length === 0 && <p>No active workspace memberships.</p>}<div aria-live="polite">{selected && !access && !error && <p role="status">Checking workspace access…</p>}{access && <><p>Access verified · {access.roles.join(', ')}</p><p>Policy: {access.decision.policy_version}</p></>}</div><button onClick={disconnect}>Disconnect</button></>}
    {error && <p role="alert">{error}</p>}
  </section>{connection && Object.keys(pendingCommands).length > 0 && <section className="card" aria-label="Stored command recovery keys">
    <h2>Command recovery keys</h2><p>Open the original application and revision to check its receipt before another write. Keep the key before closing this tab.</p>
    <ul>{Object.entries(pendingCommands).map(([scope, key]) => <li key={scope}><code>{key}</code> · Workspace {scope.split(':')[0]} · {scope.split(':')[1]} · revision {scope.split(':')[2]}</li>)}</ul>
  </section>}{connection && access && selected === access.tenant_id && <ApplicationWorkspace key={selected} token={connection.token} tenant={selected}
    pendingCommands={pendingCommands}
    onPendingCommand={(scope, key) => setCommandJournal((old) => { const next = { ...old }; if (key) next[actorPrefix + scope] = key; else delete next[actorPrefix + scope]; return next })}
    onCommandBusy={(key, pending) => setInFlight((old) => { const next = { ...old }; if (pending) next[key] = actor; else delete next[key]; return next })}
  />}</>
}
