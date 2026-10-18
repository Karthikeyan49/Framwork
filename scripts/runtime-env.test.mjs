import assert from 'node:assert/strict'
import { test } from 'node:test'
import { runtimeEnv } from './runtime-env.mjs'
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
