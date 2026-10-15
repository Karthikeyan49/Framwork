import { useEffect, useState } from 'react'
import { NavLink, Outlet, Route, Routes } from 'react-router-dom'
import { createAPIClient, type Readiness } from './api/client'
import { config } from './config'
import { AccessConsole } from './AccessConsole'
import { RecoveryProvider } from './administration/Recovery'

type Status = { kind: 'loading' } | { kind: 'error' } | { kind: 'loaded'; value: Readiness }

function Shell() {
  return <div className="shell"><a className="skip" href="#main">Skip to content</a><aside><NavLink className="brand" to="/">Framework<span>CONSOLE</span></NavLink><nav aria-label="Main navigation"><NavLink to="/" end>Overview</NavLink><NavLink to="/access">Workspace access</NavLink><NavLink to="/status">Service status</NavLink></nav><p className="phase">Application previews · M6</p></aside><div className="workspace"><header><span>Workspace foundation</span><span className="badge">Development shell</span></header><main id="main"><Outlet /></main><footer>AI proposes. Deterministic systems validate and execute.</footer></div></div>
}

function Overview() {
  return <><p className="eyebrow">A shared foundation</p><h1>One framework.<br/>Every application.</h1><p className="intro">The foundation for a metadata-driven ERP and CRM. Applications will move from structured drafts to verified, approved releases.</p><section className="card"><h2>Start with confidence</h2><p>Connect to your workspace to explore compiled application drafts with dynamic forms and records.</p><NavLink className="link" to="/access">Connect to a workspace <span aria-hidden="true">→</span></NavLink></section><section className="principles" aria-label="Framework principles"><div><strong>01</strong><h3>Structured definitions</h3><p>Versioned metadata, shared across one runtime.</p></div><div><strong>02</strong><h3>Verified releases</h3><p>Compile, test, preview, and approve.</p></div><div><strong>03</strong><h3>Deterministic execution</h3><p>Predictable behavior at runtime.</p></div></section></>
}

export function ServiceStatus() {
  const [status, setStatus] = useState<Status>({ kind: 'loading' })
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    createAPIClient(config.apiBaseURL).readiness(controller.signal).then((value) => {
      if (!controller.signal.aborted) setStatus({ kind: 'loaded', value })
    }).catch(() => { if (!controller.signal.aborted) setStatus({ kind: 'error' }) })
    return () => controller.abort()
  }, [attempt])
  return <><p className="eyebrow">Operations</p><h1>Service status</h1><p className="intro">Connection status for the framework’s required services.</p><section className="card" aria-live="polite">
    {status.kind === 'loading' && <p role="status">Checking services…</p>}
    {status.kind === 'error' && <div role="alert"><h2>Could not reach the API</h2><p>Check that the local stack and API are running, then try again.</p></div>}
    {status.kind === 'loaded' && <><h2>{status.value.status === 'ready' ? 'All services are ready' : 'Some services are unavailable'}</h2><ul className="services">{Object.entries(status.value.dependencies).map(([name, value]) => <li key={name}><span>{name}</span><span className={`state ${value}`}>{value === 'ok' ? 'Connected' : 'Unavailable'}</span></li>)}</ul></>}
    <button disabled={status.kind === 'loading'} onClick={() => { setStatus({ kind: 'loading' }); setAttempt((n) => n + 1) }}>Refresh status</button>
  </section></>
}

export function App() {
  return <RecoveryProvider><Routes><Route element={<Shell />}><Route index element={<Overview />} /><Route path="access" element={<AccessConsole />} /><Route path="status" element={<ServiceStatus />} /><Route path="*" element={<><h1>Page not found</h1><NavLink to="/">Return to overview</NavLink></>} /></Route></Routes></RecoveryProvider>
}
