import assert from 'node:assert/strict'
import { test } from 'node:test'
import { runtimeEnv, runtimeKindOf } from './runtime-env.mjs'
const source={PATH:'/synthetic/bin',DATABASE_URL:'api-credential',WORKER_DATABASE_URL:'worker-credential',DISPATCHER_DATABASE_URL:'dispatcher-credential',MIGRATION_DATABASE_URL:'migration-credential',POSTGRES_PASSWORD:'operator-secret',REDIS_URL:'cache-credential',EVENT_SENDER_REDIS_URL:'sender-credential',EVENT_RECEIVER_REDIS_URL:'receiver-credential',EVENTS_ENABLED:'true',VITE_API_BASE_URL:'/api'}
test('all backend children preserve explicit workflow enablement',()=>{
 for(const value of ['false','true'])for(const kind of ['api','worker','dispatcher'])assert.equal(runtimeEnv({...source,WORKFLOWS_ENABLED:value},kind).WORKFLOWS_ENABLED,value)
 assert.equal(runtimeEnv({...source,WORKFLOWS_ENABLED:'false'},'web').WORKFLOWS_ENABLED,undefined)
})
test('API/worker/dispatcher/frontend children receive only their credential bindings',()=>{
 for(const kind of ['api','worker','dispatcher','web']){const env=runtimeEnv(source,kind);for(const key of ['MIGRATION_DATABASE_URL','POSTGRES_PASSWORD','EVENT_SENDER_REDIS_URL','EVENT_RECEIVER_REDIS_URL'])assert.equal(env[key],undefined);if(kind==='api'){assert.equal(env.DATABASE_URL,'api-credential');assert.equal(env.WORKER_DATABASE_URL,undefined);assert.equal(env.DISPATCHER_DATABASE_URL,undefined);assert.equal(env.REDIS_URL,'cache-credential');assert.equal(env.EVENT_REDIS_URL,undefined)}else if(kind==='worker'){assert.equal(env.DATABASE_URL,'worker-credential');assert.equal(env.WORKER_DATABASE_URL,'worker-credential');assert.equal(env.DISPATCHER_DATABASE_URL,undefined);assert.equal(env.REDIS_URL,'receiver-credential');assert.equal(env.EVENT_REDIS_URL,'receiver-credential')}else if(kind==='dispatcher'){assert.equal(env.DATABASE_URL,'dispatcher-credential');assert.equal(env.DISPATCHER_DATABASE_URL,'dispatcher-credential');assert.equal(env.WORKER_DATABASE_URL,undefined);assert.equal(env.EVENT_REDIS_URL,'sender-credential')}else{assert.equal(env.DATABASE_URL,undefined);assert.equal(env.REDIS_URL,undefined);assert.equal(env.VITE_API_BASE_URL,'/api')}}
})
test('M8 local provisioning rejects production without changes',()=>{const temp=fs.mkdtempSync(path.join(os.tmpdir(),'m8-env-'));try{const original='APP_ENV="production"\n';fs.writeFileSync(path.join(temp,'.env'),original);const env={...process.env};delete env.APP_ENV;const result=spawnSync(process.execPath,[fileURLToPath(new URL('./m8-env.mjs',import.meta.url))],{cwd:temp,env,encoding:'utf8'});assert.notEqual(result.status,0);assert.equal(fs.readFileSync(path.join(temp,'.env'),'utf8'),original);assert.equal(fs.existsSync(path.join(temp,'.local')),false)}finally{fs.rmSync(temp,{recursive:true,force:true})}})
test('M8 local provisioning refuses reused operator credentials before writes',()=>{const temp=fs.mkdtempSync(path.join(os.tmpdir(),'m8-env-'));try{const original=`APP_ENV=development\nREDIS_PASSWORD=${'a'.repeat(64)}\nEVENT_OPERATOR_PASSWORD=${'a'.repeat(64)}\n`;fs.writeFileSync(path.join(temp,'.env'),original);const env={...process.env};delete env.APP_ENV;const result=spawnSync(process.execPath,[fileURLToPath(new URL('./m8-env.mjs',import.meta.url))],{cwd:temp,env,encoding:'utf8'});assert.notEqual(result.status,0);assert.equal(fs.readFileSync(path.join(temp,'.env'),'utf8'),original);assert.equal(fs.existsSync(path.join(temp,'.local')),false)}finally{fs.rmSync(temp,{recursive:true,force:true})}})

import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
test('local provisioning rejects quoted production and preserves the file',()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'m6r-env-'));try{
 const script=fileURLToPath(new URL('./m6r-env.mjs',import.meta.url));for(const setting of ['APP_ENV="production"','APP_ENV=unknown']){
 fs.writeFileSync(path.join(temp,'.env'),setting+'\n');const env={...process.env};delete env.APP_ENV;const result=spawnSync(process.execPath,[script],{cwd:temp,env,encoding:'utf8'});assert.notEqual(result.status,0);assert.equal(fs.readFileSync(path.join(temp,'.env'),'utf8'),setting+'\n')
 }
 }finally{fs.rmSync(temp,{recursive:true,force:true})}
})

test('only API children receive sign-in configuration and credentials',()=>{
 const configured={...source,SIGNIN_REDIS_URL:'signin-credential',SIGNIN_PASSWORD:'private-password',OIDC_CLIENT_ID:'web',OIDC_AUTHORIZATION_URL:'https://id.example/authorize',OIDC_TOKEN_URL:'https://id.example/token',OIDC_REDIRECT_URI:'https://web.example/auth/callback',OIDC_SCOPES:'openid',AUTH_CLOCK_SKEW:'30s'}
 for(const kind of ['api','worker','dispatcher','web']){
  const env=runtimeEnv(configured,kind)
  assert.equal(env.SIGNIN_PASSWORD,undefined)
  for(const key of ['SIGNIN_REDIS_URL','OIDC_CLIENT_ID','OIDC_AUTHORIZATION_URL','OIDC_TOKEN_URL','OIDC_REDIRECT_URI','OIDC_SCOPES','AUTH_CLOCK_SKEW'])assert.equal(env[key],kind==='api'?configured[key]:undefined)
 }
})

test('local sign-in activation refuses production and invalid bindings without invoking docker',()=>{
 const script=fileURLToPath(new URL('./signin-roles.mjs',import.meta.url))
 for(const APP_ENV of ['production','development']){
  const env={PATH:'/nonexistent',APP_ENV};const result=spawnSync(process.execPath,[script],{env,encoding:'utf8'})
  assert.notEqual(result.status,0);assert.match(result.stderr,/Local sign-in ACL activation failed/);assert.equal(result.stdout,'')
 }
})

test('database concurrency reaches only the API process',()=>{
 for(const value of ['1','4','10','0']){
  const configured={...source,API_DB_CONCURRENCY:value}
  assert.equal(runtimeEnv(configured,'api').API_DB_CONCURRENCY,value)
  for(const kind of ['worker','dispatcher','web'])assert.equal(runtimeEnv(configured,kind).API_DB_CONCURRENCY,undefined)
 }
})

test('API request admission tuning reaches only the API process',()=>{
 const configured={...source,API_REQUESTS_PER_SECOND:'300',API_REQUEST_BURST:'600',API_GLOBAL_REQUESTS_PER_SECOND:'1000',API_GLOBAL_REQUEST_BURST:'2000'}
 for(const key of ['API_REQUESTS_PER_SECOND','API_REQUEST_BURST','API_GLOBAL_REQUESTS_PER_SECOND','API_GLOBAL_REQUEST_BURST']){
  assert.equal(runtimeEnv(configured,'api')[key],configured[key])
  for(const kind of ['worker','dispatcher','web'])assert.equal(runtimeEnv(configured,kind)[key],undefined)
 }
})

test('AI provider and tool settings reach only the processes that use them',()=>{
 const configured={...source,AI_PROVIDER_FILE:'/synthetic/ai-provider.json',TOOL_CATALOG_FILE:'/synthetic/tools.json',TOOL_SECRETS_DIR:'/synthetic/tool-secrets',TOOL_CA_FILE:'/synthetic/tool-ca.pem',TOOL_ALLOW_INTERNAL:'true'}
 const expected={api:{AI_PROVIDER_FILE:true,TOOL_CATALOG_FILE:true},worker:{TOOL_CATALOG_FILE:true,TOOL_SECRETS_DIR:true,TOOL_CA_FILE:true,TOOL_ALLOW_INTERNAL:true},dispatcher:{},web:{}}
 for(const kind of ['api','worker','dispatcher','web']){
  const env=runtimeEnv(configured,kind)
  for(const key of ['AI_PROVIDER_FILE','TOOL_CATALOG_FILE','TOOL_SECRETS_DIR','TOOL_CA_FILE','TOOL_ALLOW_INTERNAL'])assert.equal(env[key],expected[kind][key]?configured[key]:undefined,`${kind} ${key}`)
 }
 // Unset stays unset: no empty string is invented for a process that has no such setting.
 for(const kind of ['api','worker'])for(const key of ['AI_PROVIDER_FILE','TOOL_CATALOG_FILE','TOOL_SECRETS_DIR','TOOL_CA_FILE','TOOL_ALLOW_INTERNAL'])assert.equal(Object.hasOwn(runtimeEnv(source,kind),key),false,`${kind} ${key}`)
})

test('the documented launcher hands the configured AI and tool settings to its API and worker children',()=>{
 // with-env.mjs is what make api/worker run; it must build the child environment with runtimeEnv, not copy the parent's.
 const script=fileURLToPath(new URL('./with-env.mjs',import.meta.url))
 const probe="process.stdout.write(JSON.stringify(Object.fromEntries(['AI_PROVIDER_FILE','TOOL_CATALOG_FILE','TOOL_SECRETS_DIR','TOOL_CA_FILE','TOOL_ALLOW_INTERNAL','MIGRATION_DATABASE_URL'].map(k=>[k,process.env[k]]))))"
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'launcher-'));try{
  fs.mkdirSync(path.join(temp,'bin'));const fake=path.join(temp,'bin','api');fs.writeFileSync(fake,'#!'+process.execPath+'\n'+probe+'\n',{mode:0o755})
  const env={PATH:process.env.PATH,AI_PROVIDER_FILE:'/synthetic/ai.json',TOOL_CATALOG_FILE:'/synthetic/tools.json',TOOL_SECRETS_DIR:'/synthetic/secrets',MIGRATION_DATABASE_URL:'operator-credential'}
  const result=spawnSync(process.execPath,[script,fake],{cwd:temp,env,encoding:'utf8'})
  assert.equal(result.status,0,result.stderr)
  const seen=JSON.parse(result.stdout)
  assert.equal(seen.AI_PROVIDER_FILE,'/synthetic/ai.json');assert.equal(seen.TOOL_CATALOG_FILE,'/synthetic/tools.json')
  assert.equal(seen.TOOL_SECRETS_DIR,undefined,'tool secrets directory must not reach the API process');assert.equal(seen.MIGRATION_DATABASE_URL,undefined)
 }finally{fs.rmSync(temp,{recursive:true,force:true})}
})

test('every spelling of a backend command is recognised, and nothing else is',()=>{
 // The launcher hands the whole environment (operator credentials included) to a command it does not recognise, so the forms
 // that name a backend process are listed here (independent review F-04: a trailing slash and a module path were missed).
 const backend=[
  ['go',['run','./cmd/api'],'api'],['go',['run','./cmd/worker'],'worker'],['go',['run','./cmd/dispatcher'],'dispatcher'],
  ['go',['run','./cmd/worker/'],'worker'],['go',['run','cmd/worker'],'worker'],['go',['run','cmd/worker/'],'worker'],
  ['go',['run','github.com/Karthikeyan49/Framwork/cmd/worker'],'worker'],['go',['run','github.com/Karthikeyan49/Framwork/cmd/api/'],'api'],
  ['go',['run','./cmd/worker/main.go'],'worker'],['go',['run','./cmd/../cmd/dispatcher'],'dispatcher'],['go',['run','.//cmd//api'],'api'],
  ['./scripts/go.sh',['run','./cmd/worker'],'worker'],
  ['bin/api',[],'api'],['./bin/worker',[],'worker'],['/srv/framework/bin/dispatcher',[],'dispatcher'],['bin/api/',[],'api'],['./bin/../bin/api',[],'api'],
  ['node',['wrapper.mjs','bin/worker'],'worker'],
 ]
 for(const [command,args,kind] of backend)assert.equal(runtimeKindOf(command,args),kind,`${command} ${args.join(' ')}`)
 const other=[
  ['go',['run','./cmd/migrate','up']],['go',['run','./cmd/release-check']],['go',['test','-tags=integration','./tests/integration']],
  ['node',['scripts/dev-identity.mjs']],['./scripts/go.sh',['vet','./...']],
  ['go',['run','./cmd/workers']],['go',['run','./cmd/worker-extra']],['go',['run','./cmd/apix']],['go',['run','./pkg/cmd/api/internal']],
  ['bin/apix',[]],['obin/api',[]],['api',[]],['worker',['api']],['go',['run','--tags=cmd/worker']],
 ]
 for(const [command,args] of other)assert.equal(runtimeKindOf(command,args),undefined,`${command} ${args.join(' ')}`)
})

test('the launcher gives every spelling of a backend command only that backend\'s environment',()=>{
 // The real launcher with a fake go and fake binaries that print the credentials they were handed.
 const script=fileURLToPath(new URL('./with-env.mjs',import.meta.url))
 const probe="process.stdout.write(JSON.stringify(Object.fromEntries(['DATABASE_URL','WORKER_DATABASE_URL','MIGRATION_DATABASE_URL','POSTGRES_PASSWORD','MIGRATOR_PASSWORD','TOOL_SECRETS_DIR','AI_PROVIDER_FILE'].map(k=>[k,process.env[k]]))))"
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'launcher-forms-'));try{
  const bin=path.join(temp,'bin');fs.mkdirSync(bin)
  for(const name of ['api','worker','dispatcher','go','tool'])fs.writeFileSync(path.join(bin,name),'#!'+process.execPath+'\n'+probe+'\n',{mode:0o755})
  const source={PATH:bin+path.delimiter+process.env.PATH,DATABASE_URL:'api-credential',WORKER_DATABASE_URL:'worker-credential',DISPATCHER_DATABASE_URL:'dispatcher-credential',MIGRATION_DATABASE_URL:'operator-credential',POSTGRES_PASSWORD:'operator-password',MIGRATOR_PASSWORD:'migrator-password',TOOL_SECRETS_DIR:'/synthetic/secrets',AI_PROVIDER_FILE:'/synthetic/ai.json'}
  const run=(...argv)=>{const result=spawnSync(process.execPath,[script,...argv],{cwd:temp,env:source,encoding:'utf8'});assert.equal(result.status,0,result.stderr);return JSON.parse(result.stdout)}
  const operatorKeys=['MIGRATION_DATABASE_URL','POSTGRES_PASSWORD','MIGRATOR_PASSWORD']
  const forms=[
   [['go','run','./cmd/worker'],'worker'],[['go','run','./cmd/worker/'],'worker'],[['go','run','github.com/Karthikeyan49/Framwork/cmd/worker'],'worker'],
   [['go','run','./cmd/api/'],'api'],[['go','run','github.com/Karthikeyan49/Framwork/cmd/dispatcher'],'dispatcher'],
   [[path.join(bin,'worker')],'worker'],[[path.join(bin,'api')],'api'],[[path.join(bin,'dispatcher')],'dispatcher'],
  ]
  for(const [argv,kind] of forms){
   const seen=run(...argv)
   for(const key of operatorKeys)assert.equal(seen[key],undefined,`${argv.join(' ')}: ${key} reached a ${kind}`)
   assert.equal(seen.DATABASE_URL,{api:'api-credential',worker:'worker-credential',dispatcher:'dispatcher-credential'}[kind],argv.join(' '))
  }
  // A command that is not a backend process keeps the whole environment: migrations and tests need the operator credentials.
  const migrate=run('go','run','./cmd/migrate','up')
  assert.equal(migrate.MIGRATION_DATABASE_URL,'operator-credential')
 }finally{fs.rmSync(temp,{recursive:true,force:true})}
})
