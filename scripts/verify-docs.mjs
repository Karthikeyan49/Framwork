import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { fileURLToPath } from 'node:url'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../docs')
let count = 0
for (const [, expected, file] of fs.readFileSync(path.join(root, 'SHA256SUMS.md'), 'utf8').matchAll(/`([a-f0-9]{64})`  `([^`]+)`/g)) {
  if (crypto.createHash('sha256').update(fs.readFileSync(path.join(root, file))).digest('hex') !== expected) throw new Error(`Source document changed: ${file}`)
  count++
}
if (count !== 84) throw new Error('Incomplete source document manifest')
console.log(`PASS: ${count} architecture source checksums`)
