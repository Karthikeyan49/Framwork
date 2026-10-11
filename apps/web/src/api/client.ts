export class APIError extends Error {
  constructor(public readonly status: number, public readonly requestID?: string) {
    super(status === 503 ? 'A required service is unavailable.' : 'The request could not be completed.')
    this.name = 'APIError'
  }
}

export interface Readiness {
  status: 'ready' | 'not_ready'
  dependencies: Record<string, 'ok' | 'unavailable'>
}
export interface Session { user: { id: string; name: string }; tenants: { id: string; name: string }[] }
export interface TenantAccess { tenant_id: string; roles: string[]; decision: { allow: boolean; decision_id: string; policy_version: string } }
const isRecord = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const isID = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)
export function isSession(v: unknown): v is Session {
  return isRecord(v) && isRecord(v.user) && isID(v.user.id) && typeof v.user.name === 'string' && Array.isArray(v.tenants) && v.tenants.every((t: unknown) => isRecord(t) && isID(t.id) && typeof t.name === 'string')
}
export function isTenantAccess(v: unknown): v is TenantAccess {
  return isRecord(v) && isID(v.tenant_id) && Array.isArray(v.roles) && v.roles.every((r: unknown) => typeof r === 'string') && isRecord(v.decision) && v.decision.allow === true && typeof v.decision.decision_id === 'string' && typeof v.decision.policy_version === 'string'
}

export function isReadiness(value: unknown): value is Readiness {
  if (!value || typeof value !== 'object') return false
  const record = value as Record<string, unknown>
  return (record.status === 'ready' || record.status === 'not_ready') && !!record.dependencies && typeof record.dependencies === 'object' && !Array.isArray(record.dependencies) && Object.values(record.dependencies).every((v) => v === 'ok' || v === 'unavailable')
}

export function createAPIClient(baseURL: string, transport: typeof fetch = fetch) {
  async function authenticated<T>(path: string, token: string, validate: (v: unknown) => v is T, signal?: AbortSignal): Promise<T> {
    const response = await transport(`${baseURL}${path}`, { method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${token}` }, credentials: 'omit', signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000) })
    if (!response.ok) throw new APIError(response.status, response.headers.get('X-Request-ID') ?? undefined)
    const body: unknown = await response.json()
    if (!validate(body)) throw new Error('Invalid identity response')
    return body
  }
  return {
    session: (token: string, signal?: AbortSignal) => authenticated('/v1/session', token, isSession, signal),
    access: (token: string, tenantID: string, signal?: AbortSignal) => {
      if (!isID(tenantID)) return Promise.reject(new Error('Invalid tenant selection'))
      return authenticated(`/v1/tenants/${tenantID}/access`, token, isTenantAccess, signal)
    },
    async readiness(signal?: AbortSignal): Promise<Readiness> {
      const response = await transport(`${baseURL}/readyz`, {
        method: 'GET', headers: { Accept: 'application/json' },
        credentials: 'same-origin',
        signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000),
      })
      if (!response.ok && response.status !== 503) throw new APIError(response.status, response.headers.get('X-Request-ID') ?? undefined)
      const body: unknown = await response.json()
      if (!isReadiness(body) || (response.ok && body.status !== 'ready') || (response.status === 503 && body.status !== 'not_ready')) throw new Error('Invalid service response')
      return body
    },
  }
}
