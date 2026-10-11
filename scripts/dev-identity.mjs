import fs from 'node:fs'
import crypto from 'node:crypto'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
if (!['development', 'test'].includes(process.env.APP_ENV ?? 'development')) throw new Error('Local identity tooling is disabled outside development/test')
const dir = path.join(root, '.local/identity')
const privatePath = path.join(dir, 'private.pem'), publicPath = path.join(dir, 'jwks.json')
const issuer = process.env.AUTH_ISSUER ?? 'http://localhost:8080/dev-issuer'
const audience = process.env.AUTH_AUDIENCE ?? 'framework-api'
// Never mint a development token impersonating a configured external issuer.
if (issuer !== 'http://localhost:8080/dev-issuer' || audience !== 'framework-api' || (process.env.AUTH_JWKS_FILE && path.resolve(root, process.env.AUTH_JWKS_FILE) !== publicPath)) throw new Error('Local tooling requires the explicit local issuer configuration')
fs.mkdirSync(dir, { recursive: true, mode: 0o700 })
if (!fs.existsSync(privatePath) && !fs.existsSync(publicPath)) {
  const { privateKey, publicKey } = crypto.generateKeyPairSync('rsa', { modulusLength: 3072 })
  fs.writeFileSync(privatePath, privateKey.export({ type: 'pkcs8', format: 'pem' }), { mode: 0o600, flag: 'wx' })
  const jwk = publicKey.export({ format: 'jwk' })
  fs.writeFileSync(publicPath, JSON.stringify({ keys: [{ ...jwk, kid: 'local-v1', alg: 'RS256', use: 'sig' }] }), { mode: 0o600, flag: 'wx' })
}
if (!fs.existsSync(privatePath) || !fs.existsSync(publicPath)) throw new Error('Incomplete local key pair; restore both files rather than silently rotating')
if ((fs.statSync(privatePath).mode & 0o077) !== 0) throw new Error('Local signing key must have owner-only permissions')
const privateKey = crypto.createPrivateKey(fs.readFileSync(privatePath))
const expected = crypto.createPublicKey(privateKey).export({ format: 'jwk' })
const key = JSON.parse(fs.readFileSync(publicPath, 'utf8')).keys?.[0]
if (key?.n !== expected.n || key?.e !== expected.e || key?.kid !== 'local-v1') throw new Error('Local key pair does not match; restore it explicitly')
if (process.argv[2] === 'token') {
  const subject = process.argv[3]
  if (!subject || subject.length > 255 || process.argv.length !== 4) throw new Error('Supply one provisioned local subject')
  const now = Math.floor(Date.now() / 1000)
  const encode = (v) => Buffer.from(JSON.stringify(v)).toString('base64url')
  const input = `${encode({ alg: 'RS256', typ: 'at+jwt', kid: 'local-v1' })}.${encode({ iss: issuer, aud: audience, sub: subject, iat: now, nbf: now, exp: now + 900 })}`
  const signature = crypto.sign('RSA-SHA256', Buffer.from(input), privateKey).toString('base64url')
  process.stdout.write(`${input}.${signature}\n`)
} else if (!process.argv[2]) console.log('Local identity keys ready; existing keys preserved.')
else throw new Error('Unsupported identity tooling operation')
