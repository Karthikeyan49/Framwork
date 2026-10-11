export function apiBaseURL(value: string | undefined): string {
  const base = value?.trim() || '/api'
  if (base.startsWith('/') && !base.startsWith('//') && !base.includes('\\') && !base.includes('?') && !base.includes('#')) return base.replace(/\/$/, '')
  const url = new URL(base)
  const loopback = url.hostname === 'localhost' || url.hostname === '127.0.0.1'
  if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback)) || url.username || url.password || url.search || url.hash) throw new Error('Invalid API base URL')
  return url.toString().replace(/\/$/, '')
}

// VITE_ variables are public build configuration. Never store secrets here.
export const config = { apiBaseURL: apiBaseURL(import.meta.env.VITE_API_BASE_URL) }
