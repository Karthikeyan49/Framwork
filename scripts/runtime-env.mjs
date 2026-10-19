// Pass only runtime configuration to application children; operator credentials
// remain in the controlled migration/provisioning processes.
export function runtimeEnv(source,kind) {
  const shared = ['PATH','HOME','TMPDIR','LANG','LC_ALL','GOCACHE','GOMODCACHE','GOTOOLCHAIN','GOROOT','GOPATH','APP_ENV','HTTP_ADDR','LOG_LEVEL','CORS_ORIGINS','AUTH_ISSUER','AUTH_AUDIENCE','AUTH_JWKS_FILE','OTEL_EXPORTER_OTLP_ENDPOINT']
  const env = Object.fromEntries(shared.filter(k=>source[k]!==undefined).map(k=>[k,source[k]]))
  if(kind==='worker'){env.DATABASE_URL=source.WORKER_DATABASE_URL;env.WORKER_DATABASE_URL=source.WORKER_DATABASE_URL;env.REDIS_URL=source.EVENT_RECEIVER_REDIS_URL;env.EVENT_REDIS_URL=source.EVENT_RECEIVER_REDIS_URL;env.EVENTS_ENABLED=source.EVENTS_ENABLED}
  else if(kind==='dispatcher'){env.DATABASE_URL=source.DISPATCHER_DATABASE_URL;env.DISPATCHER_DATABASE_URL=source.DISPATCHER_DATABASE_URL;env.REDIS_URL=source.EVENT_SENDER_REDIS_URL;env.EVENT_REDIS_URL=source.EVENT_SENDER_REDIS_URL;env.EVENTS_ENABLED=source.EVENTS_ENABLED}
  else if(kind==='api') {env.DATABASE_URL=source.DATABASE_URL;env.REDIS_URL=source.REDIS_URL;for(const key of ['API_DB_CONCURRENCY','API_REQUESTS_PER_SECOND','API_REQUEST_BURST','API_GLOBAL_REQUESTS_PER_SECOND','API_GLOBAL_REQUEST_BURST','AUTH_CLOCK_SKEW','OIDC_CLIENT_ID','OIDC_AUTHORIZATION_URL','OIDC_TOKEN_URL','OIDC_REDIRECT_URI','OIDC_SCOPES','SIGNIN_REDIS_URL'])if(source[key]!==undefined)env[key]=source[key]}
  else {delete env.REDIS_URL; for(const k of ['VITE_API_BASE_URL','APP_ENV'])if(source[k]!==undefined)env[k]=source[k]}
  if(['api','worker','dispatcher'].includes(kind)){if(source.WORKFLOWS_ENABLED!==undefined)env.WORKFLOWS_ENABLED=source.WORKFLOWS_ENABLED}
  return env
}
