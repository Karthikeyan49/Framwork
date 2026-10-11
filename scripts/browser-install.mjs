import { existsSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..')
if (existsSync('/usr/bin/chromium')) console.log('Using installed system Chromium for browser tests.')
else {
 const result=spawnSync('npm',['--prefix','apps/web','exec','--','playwright','install','chromium'],{cwd:root,stdio:'inherit',env:{...process.env,PLAYWRIGHT_BROWSERS_PATH:path.join(root,'.local/cache/ms-playwright')}})
 process.exit(result.status ?? 1)
}
