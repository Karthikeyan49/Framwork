import { expect, it } from 'vitest'
import { proxyTarget } from './proxy'

it('honors custom ports and maps wildcard listeners to loopback', () => {
  for (const address of ['0.0.0.0:8081', ':8081', '[::]:8081', '127.0.0.1:8081']) expect(proxyTarget(address)).toBe('http://127.0.0.1:8081')
  expect(proxyTarget('localhost:9000')).toBe('http://localhost:9000')
  expect(proxyTarget('[::1]:8081')).toBe('http://[::1]:8081')
  expect(() => proxyTarget('invalid/path')).toThrow()
})
