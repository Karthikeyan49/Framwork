// Generate development-only Redis ACL bindings without replacing existing secrets.
import fs from 'node:fs'
import crypto from 'node:crypto'
import {parseEnv} from 'node:util'
const current=fs.readFileSync('.env','utf8'),parsed=parseEnv(current)
if(!['development','test'].includes(process.env.APP_ENV??parsed.APP_ENV))throw new Error('Local event provisioning requires development/test')
const additions=[],credentials={}
for(const [prefix,user] of [['EVENT_SENDER','event_dispatcher'],['EVENT_RECEIVER','event_worker'],['EVENT_OPERATOR','event_operator']]){
 const key=`${prefix}_PASSWORD`,url=`${prefix}_REDIS_URL`
 if(parsed[key]!==undefined&&!/^[a-f0-9]{64}$/.test(parsed[key]))throw new Error('Retained event credential requires explicit configuration')
 if(parsed[url]!==undefined&&parsed[key]===undefined)throw new Error('Preserve external binding; configure local ACL credentials explicitly')
 credentials[user]=parsed[key]??crypto.randomBytes(32).toString('hex')
 if(parsed[key]===undefined)additions.push(`${key}=${credentials[user]}`)
 if(parsed[url]===undefined)additions.push(`${url}=redis://${user}:${credentials[user]}@127.0.0.1:6379/0`)
}
if(!/^[a-f0-9]{64}$/.test(parsed.REDIS_PASSWORD??''))throw new Error('Local cache password must already be initialized')
const passwords=[parsed.REDIS_PASSWORD,...Object.values(credentials)]
if(new Set(passwords).size!==passwords.length)throw new Error('Cache, sender, receiver and operator credentials must be distinct')
if(parsed.EVENTS_ENABLED===undefined)additions.push('EVENTS_ENABLED=true')
if(additions.length)fs.appendFileSync('.env','\n# M8 local broker bindings (preserve secrets).\n'+additions.join('\n')+'\n',{mode:0o600})
fs.mkdirSync('.local/m8',{recursive:true})
const digest=s=>crypto.createHash('sha256').update(s).digest('hex')
fs.writeFileSync('.local/m8/users.acl',`user default on #${digest(parsed.REDIS_PASSWORD)} ~framework:cache:* -@all +ping +get +set +del +expire +ttl\nuser event_operator on #${digest(credentials.event_operator)} ~* &* +@all\nuser event_dispatcher on #${digest(credentials.event_dispatcher)} ~framework:events:preview:v1 -@all +ping +xadd\nuser event_worker on #${digest(credentials.event_worker)} ~framework:events:preview:v1 -@all +ping +xgroup|create +xreadgroup +xautoclaim +xack\n`,{mode:0o600})
fs.chmodSync('.local/m8/users.acl',0o600)
console.log('M8 local ACL bindings prepared; retained credentials preserved.')
