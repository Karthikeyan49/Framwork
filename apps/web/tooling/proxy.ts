// Wildcard bind addresses are listeners, not destinations. Connect via loopback.
export function proxyTarget(address: string): string {
  const normalized = address.replace(/^(?:0\.0\.0\.0|\[::\])?:/, '127.0.0.1:')
  const url = new URL(`http://${normalized}`)
  if (!url.hostname || url.username || url.password || url.pathname !== '/' || url.search || url.hash) throw new Error('Invalid API listen address')
  return url.origin
}
