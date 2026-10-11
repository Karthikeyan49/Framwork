import { spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { parseEnv } from 'node:util'
import { fileURLToPath } from 'node:url'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const env = { ...parseEnv(fs.readFileSync(path.join(root, '.env'), 'utf8')), ...process.env }
console.log(`Development coordinator PID: ${process.pid}. Stop with Ctrl-C or SIGTERM to this owned PID.`)
const children = [
  spawn(path.join(root, 'bin/api'), [], { cwd: root, env, stdio: 'inherit' }),
  spawn(path.join(root, 'bin/worker'), [], { cwd: root, env, stdio: 'inherit' }),
  // The detached frontend must not take over the parent terminal's raw input.
  spawn('npm', ['--prefix', 'apps/web', 'run', 'dev'], { cwd: root, env, stdio: ['ignore', 'inherit', 'inherit'], detached: process.platform !== 'win32' }),
]
let stopping = false
function stop(code) {
  if (stopping) return
  stopping = true
  process.exitCode = code
  for (const child of children) {
    if (!child.pid) continue
    try { if (child.spawnargs[0] === 'npm' && process.platform !== 'win32') process.kill(-child.pid, 'SIGTERM'); else child.kill('SIGTERM') } catch { /* Child already stopped. */ }
  }
  const timer = setTimeout(() => { for (const child of children) { try { child.kill('SIGKILL') } catch { /* Already stopped. */ } } }, 12000)
  timer.unref()
}
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => stop(0))
for (const child of children) { child.on('error', () => stop(1)); child.on('exit', (code) => { if (!stopping) stop(code || 1) }) }
