import assert from 'node:assert/strict'
import { test } from 'node:test'
import { runtimeEnv } from './runtime-env.mjs'
const source={PATH:'/synthetic/bin',DATABASE_URL:'api-credential',WORKER_DATABASE_URL:'worker-credential',DISPATCHER_DATABASE_URL:'dispatcher-credential',MIGRATION_DATABASE_URL:'migration-credential',POSTGRES_PASSWORD:'operator-secret',REDIS_URL:'cache-credential',VITE_API_BASE_URL:'/api'}
test('API/worker/frontend children receive only their credential bindings',()=>{
 for(const kind of ['api','worker','web']){const env=runtimeEnv(source,kind);for(const key of ['MIGRATION_DATABASE_URL','DISPATCHER_DATABASE_URL','POSTGRES_PASSWORD'])assert.equal(env[key],undefined);if(kind==='api'){assert.equal(env.DATABASE_URL,'api-credential');assert.equal(env.WORKER_DATABASE_URL,undefined)}else if(kind==='worker'){assert.equal(env.DATABASE_URL,'worker-credential');assert.equal(env.WORKER_DATABASE_URL,'worker-credential')}else{assert.equal(env.DATABASE_URL,undefined);assert.equal(env.REDIS_URL,undefined);assert.equal(env.VITE_API_BASE_URL,'/api')}}
})

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
