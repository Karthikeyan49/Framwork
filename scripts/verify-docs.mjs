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

let v3count=0
for(const [,expected,file]of fs.readFileSync(path.join(root,'v3/SHA256SUMS.md'),'utf8').matchAll(/`([a-f0-9]{64})`\s+`([^`]+)`/g)){if(crypto.createHash('sha256').update(fs.readFileSync(path.join(root,'v3',file))).digest('hex')!==expected)throw new Error(`V3 document changed: ${file}`);v3count++}
if(v3count!==43)throw new Error('Incomplete V3 manifest')
console.log(`PASS: ${v3count} V3 source checksums`)
