import { useContext, useEffect, useRef, useState } from 'react'
import { APIError, createAPIClient, type TenantAccess } from './api/client'
import { ApplicationWorkspace } from './preview/Workspace'
import { config } from './config'
import { RecoveryContext, RecoveryProvider, useRecovery, type AdministrationPort } from './administration/Recovery'
import { AuthContext, AuthProvider, useAuthentication } from './auth/Session'
import { ProviderSignIn } from './auth/SignIn'
import { TeamsPanel } from './administration/TeamsPanel'
import { RolesPanel } from './administration/RolesPanel'

export function AccessConsole() {
  const shared = useContext(RecoveryContext)
  const authentication = useContext(AuthContext)
  const console = shared ? <ConnectedAccessConsole /> : <RecoveryProvider><ConnectedAccessConsole /></RecoveryProvider>
  return authentication ? console : <AuthProvider loadOptions={false}>{console}</AuthProvider>
}
function ConnectedAccessConsole() {
  const [input, setInput] = useState('')
  const { connection, connect: authorize, disconnect: clearConnection, options } = useAuthentication()
  const [selected, setSelected] = useState('')
  const [access, setAccess] = useState<TenantAccess>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // Recovery contains only owned command identities, never tokens or values.
  // Keep it above the tenant workspace so switching/disconnecting cannot erase
  // an intent whose server outcome is unknown.
  const { commandJournal, setCommandJournal, inFlight, setInFlight, administrationJournal, setAdministrationJournal } = useRecovery()
  const [teamsOpen, setTeamsOpen] = useState(false)
  const [rolesOpen, setRolesOpen] = useState(false)
  const [accessAttempt, setAccessAttempt] = useState(0)
  const [authorityNotice, setAuthorityNotice] = useState('')
  const actor = connection?.session.user.id ?? ''
  const actorPrefix = actor + ':'
  const pendingCommands = Object.fromEntries(Object.entries(commandJournal)
    .filter(([scope]) => actor !== '' && scope.startsWith(actorPrefix))
    .map(([scope, key]) => [scope.slice(actorPrefix.length), key]))
  const commandBusy = actor !== '' && Object.values(inFlight).includes(actor)
  const administration: AdministrationPort = {
    pending: Object.fromEntries(Object.entries(administrationJournal).filter(([scope]) => actor !== '' && scope.startsWith(actorPrefix)).map(([scope, entry]) => [scope.slice(actorPrefix.length), entry])),
    remember: (scope, intent) => setAdministrationJournal((old) => { const next = { ...old }; if (intent) next[actorPrefix + scope] = intent; else delete next[actorPrefix + scope]; return next }),
    busy: (key, pending) => setInFlight((old) => { const next = { ...old }; if (pending) next[key] = actor; else delete next[key]; return next }),
  }
  const login = useRef<AbortController | undefined>(undefined)
  useEffect(() => () => login.current?.abort(), [])
  useEffect(() => {
    if (!connection || !selected) return
    const controller = new AbortController()
    createAPIClient(config.apiBaseURL).access(connection.token, selected, controller.signal).then((value) => {
      if (!controller.signal.aborted) { setAccess(value); setAuthorityNotice((old) => old ? 'Your role assignment was saved. Workspace access refreshed.' : '') }
    }).catch((e: unknown) => {
      if (controller.signal.aborted) return
      if (e instanceof APIError && e.status === 401) { clearConnection(connection); setSelected(''); setError('Your access token has expired or was revoked. Connect again.') }
      else setError('Tenant access was denied or the service is unavailable.')
    })
    return () => controller.abort()
  }, [connection, selected, clearConnection, accessAttempt])
  async function connect() {
    login.current?.abort()
    const controller = new AbortController(); login.current = controller
    const token = input.trim(); setInput(''); setBusy(true); setError('')
    try {
      const session = await createAPIClient(config.apiBaseURL).session(token, controller.signal)
      if (!controller.signal.aborted) { authorize({ token, session }); setSelected(''); setAccess(undefined) }
    } catch { if (!controller.signal.aborted) setError('Could not authenticate. Check your token and service availability.') }
    finally { if (!controller.signal.aborted) setBusy(false) }
  }
  function disconnect() {
    login.current?.abort(); clearConnection(); setAccess(undefined); setSelected(''); setInput(''); setError(''); setBusy(false); setAuthorityNotice('')
  }
  return <><p className="eyebrow">Identity and tenant access</p><h1>Your workspace access</h1><p className="intro">Sign in, then choose an authorized workspace. Tokens stay in memory and are cleared when you disconnect or reload.</p><section className="card">
    {!connection ? <><ProviderSignIn />{options?.manual_token && <><h2>Development API access</h2><form onSubmit={(e) => { e.preventDefault(); void connect() }}><label htmlFor="access-token">API access token</label><input id="access-token" type="password" autoComplete="off" value={input} onChange={(e) => setInput(e.target.value)} required disabled={busy}/><button disabled={busy || !input.trim()}>{busy ? 'Connecting…' : 'Connect'}</button></form></>}</> : <><h2>{connection.session.user.name}</h2><label htmlFor="tenant-select">Workspace</label><select id="tenant-select" value={selected} disabled={commandBusy} onChange={(e) => { setSelected(e.target.value); setAccess(undefined); setError(''); setAuthorityNotice('') }}><option value="">Choose a workspace</option>{connection.session.tenants.map((t) => <option value={t.id} key={t.id}>{t.name}</option>)}</select>{connection.session.tenants.length === 0 && <p>No active workspace memberships.</p>}<div aria-live="polite">{selected && !access && !error && <p role="status">Checking workspace access…</p>}{access && <><p className="access-summary">Access verified · {access.roles.join(', ')}</p></>}</div><button onClick={disconnect}>Disconnect</button></>}
    {error && <p role="alert">{error}</p>}{connection && authorityNotice && <p role="status">{authorityNotice}</p>}
  </section>{connection && Object.keys(administration.pending).length > 0 && <section className="card administration" aria-label="Pending administration changes"><h2>Administration changes requiring review</h2><p>These requests may have committed. Return to the original workspace and resource, reload current state and review it before a new change. Keep the IDs before closing this tab.</p><ul>{Object.values(administration.pending).map((entry) => <li key={entry.scope}>{entry.kind} · Workspace <code>{entry.tenant}</code> · Resource <code>{entry.resource}</code>{entry.target && <> · Target <code>{entry.target}</code></>}</li>)}</ul></section>}{connection && Object.keys(pendingCommands).length > 0 && <section className="card" aria-label="Stored command recovery keys">
    <h2>Command recovery keys</h2><p>Open the original application and revision to check its receipt before another write. Keep the key before closing this tab.</p>
    <ul>{Object.entries(pendingCommands).map(([scope, key]) => <li key={scope}><code>{key}</code> · Workspace {scope.split(':')[0]} · {scope.split(':')[1]} · revision {scope.split(':')[2]}</li>)}</ul>
  </section>}{connection && access && selected === access.tenant_id && <><section className="actions"><button type="button" disabled={commandBusy} aria-expanded={teamsOpen} onClick={() => setTeamsOpen((old) => !old)}>{teamsOpen ? 'Close teams' : 'Manage teams'}</button><button type="button" disabled={commandBusy} aria-expanded={rolesOpen} onClick={() => setRolesOpen((old) => !old)}>{rolesOpen ? 'Close roles' : 'Manage roles'}</button></section>{rolesOpen && <RolesPanel key={"roles:" + actor + ":" + selected} token={connection.token} tenant={selected} actor={actor} recovery={administration} blocked={commandBusy} onSelfAssignment={() => { setAccess(undefined); setError(''); setAuthorityNotice('Your role assignment was saved. Checking current workspace access…'); setAccessAttempt((n) => n + 1); }}/>}{teamsOpen && <TeamsPanel key={"teams:" + actor + ':' + selected} token={connection.token} tenant={selected} recovery={administration} />}<ApplicationWorkspace key={"preview:" + actor + ':' + selected} token={connection.token} tenant={selected} administration={administration}
    pendingCommands={pendingCommands}
    onPendingCommand={(scope, key) => setCommandJournal((old) => { const next = { ...old }; if (key) next[actorPrefix + scope] = key; else delete next[actorPrefix + scope]; return next })}
    onCommandBusy={(key, pending) => setInFlight((old) => { const next = { ...old }; if (pending) next[key] = actor; else delete next[key]; return next })}
  /></>}</>
}
