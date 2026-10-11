// Runs owned foundation processes, real dependency probes, telemetry and proxy requests.
import { spawn, execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
if (!['development', 'test'].includes(process.env.APP_ENV)) throw new Error('Smoke requires development or test configuration')
const since = new Date().toISOString()
fs.mkdirSync(path.join(root, '.local'), { recursive: true })
const owned = []
function start(command, args, name, detached = false) {
  const log = fs.openSync(path.join(root, '.local', `smoke-${name}.log`), 'w', 0o600)
  const child = spawn(command, args, { cwd: root, env: process.env, stdio: ['ignore', log, log], detached })
  fs.closeSync(log)
  const exit = new Promise((resolve) => {
    child.on('error', () => resolve({ code: 1, signal: null }))
    child.on('exit', (code, signal) => resolve({ code, signal }))
  })
  owned.push({ child, exit, name, detached })
  return child
}
async function get(url, options = {}) { return fetch(url, { ...options, signal: AbortSignal.timeout(3000) }) }
async function ready(url) {
  for (let i = 0; i < 50; i++) {
    try { const response = await get(url); if (response.ok) return response } catch { /* Bounded startup polling. */ }
    if (owned.some(({ child }) => child.exitCode !== null)) throw new Error('An owned process failed during startup; inspect .local/smoke-*.log')
    await delay(100)
  }
  throw new Error('Startup readiness timeout')
}
async function stop(item) {
  if (!item.child.pid) return
  try { if (item.detached) process.kill(-item.child.pid, 'SIGTERM'); else item.child.kill('SIGTERM') } catch { /* Already exited. */ }
  const timeout = new Promise((resolve) => { const timer = setTimeout(() => resolve(null), 12000); timer.unref() })
  const result = await Promise.race([item.exit, timeout])
  if (!result) { try { if (item.detached) process.kill(-item.child.pid, 'SIGKILL'); else item.child.kill('SIGKILL') } catch { /* Already exited. */ } throw new Error(`${item.name} shutdown timeout`) }
  if (!item.detached) assert.equal(result.code, 0, `${item.name} graceful exit`)
}
try {
  assert.ok((await get('http://127.0.0.1:13133/')).ok, 'Collector health')
  start(path.join(root, 'bin/api'), [], 'api')
  start(path.join(root, 'bin/worker'), [], 'worker')
  start('npm', ['--prefix', 'apps/web', 'run', 'dev'], 'web', true)
  const base = `http://${process.env.HTTP_ADDR || '127.0.0.1:8080'}`
  const health = await ready(`${base}/healthz`)
  assert.equal((await health.json()).status, 'ok')
  assert.match(health.headers.get('x-request-id'), /^[a-f0-9]{32}$/)
  const traceID = '00112233445566778899aabbccddeeff'
  const readiness = await get(`${base}/readyz`, { headers: { traceparent: `00-${traceID}-0011223344556677-01` } })
  assert.equal(readiness.status, 200)
  assert.deepEqual(await readiness.json(), { status: 'ready', dependencies: { postgres: 'ok', redis: 'ok', identity: 'ok', tenant: 'ok', audit: 'ok', metadata: 'ok', data: 'ok' } })
  const web = await ready('http://127.0.0.1:5173/')
  assert.match(await web.text(), /Framework Console/)
  const proxy = await get('http://127.0.0.1:5173/api/readyz')
  assert.equal(proxy.status, 200)
  assert.equal((await proxy.json()).status, 'ready')
  const unauthenticated = await get('http://127.0.0.1:5173/api/v1/session')
  assert.equal(unauthenticated.status, 401)
  const cors = await get(`${base}/healthz`, { headers: { Origin: 'https://untrusted.example' } })
  assert.equal(cors.status, 403)
  // Runtime metrics export on a 10-second interval; sampled parent forces trace export.
  await delay(11000)
  assert.ok(fs.readFileSync(path.join(root, '.local/smoke-api.log'), 'utf8').includes(traceID), 'W3C request/trace correlation')
  for (const item of owned) await stop(item)
  const collector = execFileSync('docker', ['compose', '--env-file', '.env', '-f', 'infra/compose/compose.yaml', 'logs', '--since', since, 'otel-collector'], { cwd: root, encoding: 'utf8' })
  assert.match(collector, /Traces/, 'Collector received traces')
  assert.match(collector, /Metrics/, 'Collector received runtime/HTTP metrics')
  console.log('PASS: live health/readiness, PG/Redis/IAM, frontend proxy, authentication denial, exact CORS, trace correlation, collector traces/metrics, graceful API/worker shutdown')
} finally {
  for (const item of owned) { if (item.child.exitCode === null && item.child.signalCode === null) await stop(item) }
}
