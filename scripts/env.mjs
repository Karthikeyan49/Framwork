import fs from 'node:fs'
import crypto from 'node:crypto'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const dest = path.join(root, '.env')
if (fs.existsSync(dest)) { console.log('Existing .env preserved.'); process.exit(0) }
const password = () => crypto.randomBytes(32).toString('hex')
const admin = password(), migrator = password(), app = password(), redis = password()
const contents = `# Generated for local development only. Never commit this file.
APP_ENV=development
HTTP_ADDR=127.0.0.1:8080
LOG_LEVEL=info
POSTGRES_PASSWORD=${admin}
MIGRATOR_PASSWORD=${migrator}
APP_DB_PASSWORD=${app}
REDIS_PASSWORD=${redis}
DATABASE_URL=postgres://framework_app:${app}@127.0.0.1:5432/framework?sslmode=disable
MIGRATION_DATABASE_URL=postgres://framework_migrator:${migrator}@127.0.0.1:5432/framework?sslmode=disable
REDIS_URL=redis://:${redis}@127.0.0.1:6379/0
CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
VITE_API_BASE_URL=/api
`
fs.writeFileSync(dest, contents, { mode: 0o600, flag: 'wx' })
console.log('Created .env with random development credentials (mode 0600).')
