import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn } from 'node:child_process'
import { runtimeEnv, runtimeKindOf } from './runtime-env.mjs'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const env = { ...process.env }
if (fs.existsSync(path.join(root, '.env'))) {
  // Parse dotenv as data; never source/eval shell supplied by configuration.
  const parsed = (await import('node:util')).parseEnv(fs.readFileSync(path.join(root, '.env'), 'utf8'))
  for (const [key, value] of Object.entries(parsed)) env[key] ??= value
}
const [command, ...args] = process.argv.slice(2)
if (!command) { console.error('A command is required.'); process.exit(1) }
const runtimeKind = runtimeKindOf(command, args)
const child = spawn(command, args, { cwd: root, env: runtimeKind ? runtimeEnv(env,runtimeKind) : env, stdio: 'inherit' })
child.on('error', () => { console.error('Could not start command.'); process.exitCode = 1 })
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal))
child.on('exit', (code, signal) => { process.exitCode = code ?? (signal ? 1 : 0) })
