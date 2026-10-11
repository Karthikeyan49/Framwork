import { describe, expect, it, vi } from 'vitest'
import { APIError, createAPIClient } from './client'
import { apiBaseURL } from '../config'

describe('API boundary', () => {
  it('reads readiness with cancellation and no tenant claims', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ status: 'ready', dependencies: { postgres: 'ok', redis: 'ok' } })))
    expect((await createAPIClient('/api', transport).readiness()).status).toBe('ready')
    expect(transport).toHaveBeenCalledWith('/api/readyz', expect.objectContaining({ method: 'GET', signal: expect.any(AbortSignal), credentials: 'same-origin' }))
  })
  it('preserves dependency outage as a readable state', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ status: 'not_ready', dependencies: { redis: 'unavailable' } }), { status: 503 }))
    expect((await createAPIClient('/api', transport).readiness()).status).toBe('not_ready')
  })
  it('redacts server errors and retains correlation', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response('private credentials', { status: 500, headers: { 'X-Request-ID': 'abc' } }))
    await expect(createAPIClient('/api', transport).readiness()).rejects.toEqual(new APIError(500, 'abc'))
  })
  it('rejects malformed and inconsistent payloads', async () => {
    for (const value of [{ status: 'ready' }, { status: 'ready', dependencies: { redis: 'unexpected' } }, { status: 'not_ready', dependencies: {} }]) {
      const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(value)))
      await expect(createAPIClient('/api', transport).readiness()).rejects.toThrow('Invalid service response')
    }
  })
  it('propagates cancellation', async () => {
    const controller = new AbortController(); controller.abort()
    const transport = vi.fn<typeof fetch>().mockImplementation(async (_url, options) => { options?.signal?.throwIfAborted(); return new Response() })
    await expect(createAPIClient('/api', transport).readiness(controller.signal)).rejects.toThrow()
  })
  it('validates public configuration', () => {
    expect(apiBaseURL(undefined)).toBe('/api')
    expect(apiBaseURL('https://api.example.com/')).toBe('https://api.example.com')
    for (const value of ['//evil.example', 'http://evil.example', 'https://u:secret@api.example.com', 'javascript:alert(1)']) expect(() => apiBaseURL(value)).toThrow()
  })
})
