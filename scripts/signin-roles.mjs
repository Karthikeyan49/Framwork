// Add only the local sign-in identity to an already running Compose Redis.
// Preserve all existing cache/event users and never put a password in argv/logs.
import crypto from 'node:crypto'
import {execFileSync} from 'node:child_process'
try {
  if (!['development','test'].includes(process.env.APP_ENV)) throw new Error('mode')
  const operator=new URL(process.env.EVENT_OPERATOR_REDIS_URL ?? '')
  const signin=new URL(process.env.SIGNIN_REDIS_URL ?? '')
  const local=u=>u.protocol==='redis:'&&['127.0.0.1','localhost','[::1]'].includes(u.hostname)&&u.port==='6379'&&u.pathname==='/0'&&!u.search&&!u.hash
  if(!local(operator)||!local(signin)||operator.host!==signin.host||operator.username!=='event_operator'||signin.username!=='framework_signin'||!/^[a-f0-9]{64}$/.test(operator.password)||!/^[a-f0-9]{64}$/.test(signin.password)||signin.password!==process.env.SIGNIN_PASSWORD||signin.password===operator.password)throw new Error('binding')
  const env=Object.fromEntries(['PATH','HOME','TMPDIR','LANG','LC_ALL','DOCKER_HOST','DOCKER_CONFIG'].filter(k=>process.env[k]!==undefined).map(k=>[k,process.env[k]]))
  env.REDISCLI_AUTH=operator.password
  const digest=crypto.createHash('sha256').update(signin.password).digest('hex')
  const output=execFileSync('docker',['compose','--env-file','.env','-f','infra/compose/compose.yaml','exec','-T','--env','REDISCLI_AUTH','redis','redis-cli','--user','event_operator','--raw','-x','ACL','SETUSER','framework_signin','reset','on','~framework:signin:v1:*','-@all','+ping','+get','+set','+ttl','+eval','+getdel'],{env,input:'#'+digest,stdio:['pipe','pipe','pipe'],timeout:10000}).toString().trim()
  if(output!=='OK')throw new Error('activation')
  console.log('Local sign-in ACL activated; existing users retained.')
} catch {
  console.error('Local sign-in ACL activation failed; verify development Compose bindings.')
  process.exitCode=1
}
