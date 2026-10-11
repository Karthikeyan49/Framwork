import { useEffect, useRef, useState } from 'react'
import { APIError, createAPIClient, type Session, type TenantAccess } from './api/client'
import { config } from './config'

export function AccessConsole() {
  const [input, setInput] = useState('')
  const [connection, setConnection] = useState<{ token: string; session: Session }>()
  const [selected, setSelected] = useState('')
  const [access, setAccess] = useState<TenantAccess>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
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
    {!connection ? <form onSubmit={(e) => { e.preventDefault(); void connect() }}><label htmlFor="access-token">API access token</label><input id="access-token" type="password" autoComplete="off" value={input} onChange={(e) => setInput(e.target.value)} required disabled={busy}/><button disabled={busy || !input.trim()}>{busy ? 'Connecting…' : 'Connect'}</button></form> : <><h2>{connection.session.user.name}</h2><label htmlFor="tenant-select">Workspace</label><select id="tenant-select" value={selected} onChange={(e) => { setSelected(e.target.value); setAccess(undefined); setError('') }}><option value="">Choose a workspace</option>{connection.session.tenants.map((t) => <option value={t.id} key={t.id}>{t.name}</option>)}</select>{connection.session.tenants.length === 0 && <p>No active workspace memberships.</p>}<div aria-live="polite">{selected && !access && !error && <p role="status">Checking workspace access…</p>}{access && <><p>Access verified · {access.roles.join(', ')}</p><p>Policy: {access.decision.policy_version}</p></>}</div><button onClick={disconnect}>Disconnect</button></>}
    {error && <p role="alert">{error}</p>}
  </section></>
}
