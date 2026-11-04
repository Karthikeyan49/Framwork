-- ADR063. Preserve issued state/evidence; event callers must use READ COMMITTED.
-- Quiesce owned consumers/dispatchers and upgrade their binary at this cutover.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_authority()
 RETURNS TABLE(tenant_id uuid,job_id uuid,kind text,actor_id uuid,release_hash text)
 LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT c.tenant_id,c.job_id,c.kind,r.actor_id,r.release_hash
 FROM control.worker_context c JOIN control.worker_principals p ON p.role_id=c.audience AND p.tenant_id=c.tenant_id AND p.active
 JOIN pg_roles principal ON principal.oid=p.role_id AND principal.rolname=p.role_name AND principal.rolname=session_user
 JOIN control.job_routes r ON r.tenant_id=c.tenant_id AND r.id=c.job_id
 JOIN iam.users u ON u.id=r.actor_id AND u.active
 JOIN iam.memberships m ON m.tenant_id=r.tenant_id AND m.user_id=u.id AND m.active
 JOIN iam.tenants t ON t.id=r.tenant_id AND t.active
 WHERE c.backend=pg_backend_pid() AND c.transaction_id=pg_current_xact_id()
 AND control.worker_principal() AND c.tenant_id=iam.current_tenant_id() AND c.job_id::text=current_setting('app.job_id',true)
 AND CASE c.kind WHEN 'workflow' THEN m.role<>'viewer' AND EXISTS(
 SELECT 1 FROM control.workflow_jobs j WHERE j.tenant_id=c.tenant_id AND j.id=c.job_id AND j.audience=c.audience
 AND j.state='RUNNABLE' AND j.generation=c.generation AND j.token=c.token AND j.lease_until>clock_timestamp())
 WHEN 'event' THEN current_setting('transaction_isolation')='read committed' AND EXISTS(
 SELECT 1 FROM control.event_deliveries d WHERE d.tenant_id=c.tenant_id AND d.id=c.job_id
 AND d.audience=c.audience AND d.generation=c.generation AND d.lease_token=c.token AND d.state='SENT'
 AND d.published_generation=c.generation AND d.next_at>clock_timestamp()) ELSE false END
$$;
-- +goose StatementEnd
ALTER FUNCTION control.worker_authority() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.worker_authority() FROM PUBLIC;

-- Closed, no-argument semantic probe. Runtime callers receive contract metadata
-- only, never direct execution of the private helper or tenant contents.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.event_delivery_policy()
 RETURNS TABLE(version text,lease_ms integer,receipt_ms integer,max_attempts integer,retry_base_ms integer,retry_cap_ms integer)
 LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT 'event-outage-v3'::text,5000,900000,20,500,60000
 WHERE (SELECT bool_and(control.event_retry_delay(n::integer) IS NOT DISTINCT FROM delay*interval '1 millisecond')
 FROM unnest(ARRAY[500,1000,2000,4000,8000,16000,32000,60000,60000,60000,60000,60000,60000,60000,60000,60000,60000,60000,60000,60000]) WITH ORDINALITY AS expected(delay,n))
 AND control.event_retry_delay(0) IS NULL AND control.event_retry_delay(-1) IS NULL
 AND control.event_retry_delay(21) IS NULL AND control.event_retry_delay(2147483647) IS NULL
 AND control.event_retry_delay(-2147483648) IS NULL AND control.event_retry_delay(NULL::integer) IS NULL
$$;
-- +goose StatementEnd
ALTER FUNCTION control.event_delivery_policy() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.event_delivery_policy() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.event_delivery_policy() TO framework_app,framework_worker,framework_dispatcher,framework_migrator;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only live event authority; stop delivery and forward-fix'; END $$;
-- +goose StatementEnd
