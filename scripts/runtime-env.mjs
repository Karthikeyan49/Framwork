// Pass only runtime configuration to application children; operator credentials
// remain in the controlled migration/provisioning processes.
export function runtimeEnv(source,kind) {
  const shared = ['PATH','HOME','TMPDIR','LANG','LC_ALL','GOCACHE','GOMODCACHE','GOTOOLCHAIN','GOROOT','GOPATH','APP_ENV','HTTP_ADDR','LOG_LEVEL','CORS_ORIGINS','AUTH_ISSUER','AUTH_AUDIENCE','AUTH_JWKS_FILE','OTEL_EXPORTER_OTLP_ENDPOINT']
  const env = Object.fromEntries(shared.filter(k=>source[k]!==undefined).map(k=>[k,source[k]]))
  if(kind==='worker'){env.DATABASE_URL=source.WORKER_DATABASE_URL;env.WORKER_DATABASE_URL=source.WORKER_DATABASE_URL;env.REDIS_URL=source.EVENT_RECEIVER_REDIS_URL;env.EVENT_REDIS_URL=source.EVENT_RECEIVER_REDIS_URL;env.EVENTS_ENABLED=source.EVENTS_ENABLED}
  else if(kind==='dispatcher'){env.DATABASE_URL=source.DISPATCHER_DATABASE_URL;env.DISPATCHER_DATABASE_URL=source.DISPATCHER_DATABASE_URL;env.REDIS_URL=source.EVENT_SENDER_REDIS_URL;env.EVENT_REDIS_URL=source.EVENT_SENDER_REDIS_URL;env.EVENTS_ENABLED=source.EVENTS_ENABLED}
  else if(kind==='api') {env.DATABASE_URL=source.DATABASE_URL;env.REDIS_URL=source.REDIS_URL}
  else {delete env.REDIS_URL; for(const k of ['VITE_API_BASE_URL','APP_ENV'])if(source[k]!==undefined)env[k]=source[k]}
  return env
}
