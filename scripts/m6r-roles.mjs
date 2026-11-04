import fs from 'node:fs'
import {spawnSync} from 'node:child_process'
if(!['development','test'].includes(process.env.APP_ENV))throw new Error('Local role bootstrap requires development/test')
let sql=fs.readFileSync('infra/compose/v3-role-bootstrap.sql','utf8')
const passwords=[]
for(const [prefix,role]of [['WORKER','framework_worker'],['DISPATCHER','framework_dispatcher']]){
 const password=process.env[`${prefix}_DB_PASSWORD`]
 if(!/^[a-f0-9]{64}$/.test(password??''))throw new Error('Explicit random local role credential missing')
 passwords.push(password);sql+=`\nALTER ROLE ${role} LOGIN PASSWORD '${password}';\n`
}
const r=spawnSync('docker',['compose','--env-file','.env','-f','infra/compose/compose.yaml','exec','-T','postgres','psql','-U','postgres','-d','framework','-v','ON_ERROR_STOP=1'],{input:sql,encoding:'utf8'})
fs.mkdirSync('.local/m6r',{recursive:true})
let log=(r.stdout??'')+(r.stderr??'');for(const password of passwords)log=log.replaceAll(password,'[REDACTED]')
fs.writeFileSync('.local/m6r/role-bootstrap.log',log,{mode:0o600})
if(r.status!==0)throw new Error('Controlled role bootstrap failed; inspect ignored sanitized log')
console.log('V3 role taxonomy provisioned; no runtime migration privilege granted.')
