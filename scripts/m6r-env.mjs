import fs from 'node:fs'
import crypto from 'node:crypto'
import { parseEnv } from 'node:util'
if(!fs.existsSync('.env'))throw new Error('Run make env first')
const current=fs.readFileSync('.env','utf8')
const parsed=parseEnv(current)
const environment=process.env.APP_ENV??parsed.APP_ENV
if(!['development','test'].includes(environment))throw new Error('Local credential provisioning requires development/test')
const additions=[]
for(const [prefix,user]of [['WORKER','framework_worker'],['DISPATCHER','framework_dispatcher']]){
 const name=`${prefix}_DB_PASSWORD`,url=`${prefix}_DATABASE_URL`
 const existing=parsed[name]
 if(existing!==undefined && !/^[a-f0-9]{64}$/.test(existing))throw new Error('Existing dedicated credential is not an initialized local binding; configure explicitly without regeneration')
 if(parsed[url]!==undefined && existing===undefined)throw new Error('Existing dedicated URL requires an explicit credential binding; never regenerate retained credentials')
 const password=existing??crypto.randomBytes(32).toString('hex')
 if(existing===undefined)additions.push(`${name}=${password}`)
 if(parsed[url]===undefined)additions.push(`${url}=postgres://${user}:${password}@127.0.0.1:5432/framework?sslmode=disable`)
}
if(additions.length)fs.appendFileSync('.env','\n# V3 local dedicated role bindings; preserve existing secrets.\n'+additions.join('\n')+'\n',{mode:0o600})
console.log('V3 local bindings ready; existing credentials preserved.')
