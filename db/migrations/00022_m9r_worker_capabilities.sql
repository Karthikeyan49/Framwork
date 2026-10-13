-- M9R: authenticated worker audiences, exact transaction capabilities and narrow reads.
-- Quiesce workers during this forward-only cutover. No business records are changed.
-- +goose Up
CREATE TABLE control.worker_principals(
 role_id oid PRIMARY KEY, role_name name NOT NULL UNIQUE,
 tenant_id uuid NOT NULL UNIQUE REFERENCES iam.tenants(id),
 active boolean NOT NULL DEFAULT true
);
ALTER TABLE control.worker_principals OWNER TO framework_schema_owner;
ALTER TABLE control.worker_principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE control.worker_principals FORCE ROW LEVEL SECURITY;
CREATE POLICY principal_operator ON control.worker_principals TO framework_schema_owner,framework_migrator USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON control.worker_principals TO framework_migrator;

-- Provision LOGIN roles through the DBA, then explicitly assign them. Neither
-- worker nor dispatcher may make assignments or acquire operator privileges.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.assign_worker(principal_name text, t uuid, enabled boolean)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
DECLARE r pg_roles%ROWTYPE;BEGIN
 IF session_user<>'framework_migrator' THEN RAISE EXCEPTION 'operator required';END IF;
 SELECT * INTO r FROM pg_roles WHERE rolname=principal_name;
 IF NOT FOUND OR principal_name !~ '^[a-z][a-z0-9_]{0,62}$' OR NOT r.rolcanlogin OR r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication
 OR NOT pg_has_role(r.oid,'framework_worker','USAGE') OR EXISTS(SELECT 1 FROM pg_roles danger WHERE danger.oid<>r.oid
 AND (danger.rolsuper OR danger.rolbypassrls OR danger.rolcreatedb OR danger.rolcreaterole OR danger.rolreplication
 OR danger.rolname IN('framework_app','framework_dispatcher','framework_migrator','framework_schema_owner','framework_audit_writer','pg_read_all_data','pg_write_all_data','pg_read_server_files','pg_write_server_files','pg_execute_server_program')) AND pg_has_role(r.oid,danger.oid,'MEMBER'))
 THEN RAISE EXCEPTION 'unsafe worker principal';END IF;
 IF NOT EXISTS(SELECT 1 FROM iam.tenants WHERE id=t AND active) THEN RAISE EXCEPTION 'active tenant required';END IF;
 INSERT INTO control.worker_principals(role_id,role_name,tenant_id,active) VALUES(r.oid,r.rolname,t,enabled)
 ON CONFLICT(role_id) DO UPDATE SET role_name=excluded.role_name,tenant_id=excluded.tenant_id,active=excluded.active;
END $function$
-- +goose StatementEnd
ALTER FUNCTION control.assign_worker(text,uuid,boolean) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.assign_worker(text,uuid,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.assign_worker(text,uuid,boolean) TO framework_migrator;

CREATE TABLE control.worker_context(
 backend integer PRIMARY KEY, transaction_id xid8 NOT NULL, audience oid NOT NULL,
 tenant_id uuid NOT NULL, job_id uuid NOT NULL, kind text NOT NULL CHECK(kind IN('workflow','event')),
 generation bigint NOT NULL, token uuid NOT NULL
);
ALTER TABLE control.worker_context OWNER TO framework_schema_owner;
ALTER TABLE control.worker_context ENABLE ROW LEVEL SECURITY;
ALTER TABLE control.worker_context FORCE ROW LEVEL SECURITY;
CREATE POLICY context_owner ON control.worker_context TO framework_schema_owner USING(true) WITH CHECK(true);

ALTER TABLE control.workflow_jobs ADD audience oid;
ALTER TABLE control.workflow_jobs ADD begun boolean NOT NULL DEFAULT false;
ALTER TABLE control.event_deliveries ADD audience oid;
ALTER TABLE control.event_attempts ADD audience oid;

-- Old leases have no audience. Keep their immutable attempt evidence; the new
-- dispatcher can reissue them after expiry. They cannot bind a worker context.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_principal()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
 SELECT EXISTS(SELECT 1 FROM pg_roles r JOIN control.worker_principals p ON p.role_id=r.oid AND p.role_name=r.rolname
 WHERE r.rolname=session_user AND r.rolcanlogin AND NOT(r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication)
 AND pg_has_role(session_user,'framework_worker','USAGE') AND p.active)
$function$
-- +goose StatementEnd

-- Authority does not derive from GUCs. They can only narrow an already bound
-- transaction. The live lease and assignment are rechecked for every use.
-- +goose StatementBegin
CREATE FUNCTION control.worker_authority() RETURNS TABLE(tenant_id uuid,job_id uuid,kind text,actor_id uuid,release_hash text)
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
 WHEN 'event' THEN EXISTS(SELECT 1 FROM control.event_deliveries d WHERE d.tenant_id=c.tenant_id AND d.id=c.job_id
 AND d.audience=c.audience AND d.generation=c.generation AND d.lease_token=c.token AND d.state='SENT'
 AND d.published_generation=c.generation AND d.next_at>clock_timestamp()) ELSE false END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.bind_worker_job(t uuid, j uuid, k text, g bigint, tok uuid, h text)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
DECLARE principal oid; valid boolean:=false; prior control.worker_context%ROWTYPE;
BEGIN
 SELECT * INTO prior FROM control.worker_context WHERE backend=pg_backend_pid();
 DELETE FROM control.worker_context WHERE backend=pg_backend_pid();
 -- A transaction may bind once; failed rebind removes authority.
 IF prior.transaction_id=pg_current_xact_id() THEN RETURN false;END IF;
 SELECT r.oid INTO principal FROM pg_roles r JOIN control.worker_principals p ON p.role_id=r.oid AND p.role_name=r.rolname
 WHERE r.rolname=session_user AND p.tenant_id=t AND p.active AND control.worker_principal();
 IF principal IS NULL OR g<1 OR tok IS NULL OR h IS NULL THEN RETURN false;END IF;
 IF k='workflow' THEN
   -- Same order as API signals: instance, then scheduler fence. Under repeatable
   -- read a changed row raises serialization failure rather than accepting a stale lease.
   PERFORM 1 FROM runtime.workflow_instances WHERE tenant_id=t AND id=j FOR UPDATE;
   IF NOT FOUND THEN RETURN false;END IF;
   SELECT x.audience=principal AND x.generation=g AND x.token=tok AND x.ir_hash=h AND x.state='RUNNABLE'
     AND x.lease_until>clock_timestamp() INTO valid FROM control.workflow_jobs x WHERE x.tenant_id=t AND x.id=j FOR UPDATE;
 ELSIF k='event' THEN
   SELECT d.audience=principal AND d.generation=g AND d.lease_token=tok AND d.state='SENT'
     AND d.published_generation=g AND d.next_at>clock_timestamp() INTO valid
     FROM control.event_deliveries d WHERE d.tenant_id=t AND d.id=j FOR UPDATE;
 ELSE RETURN false;END IF;
 IF k='workflow' AND coalesce(valid,false) THEN
 UPDATE control.workflow_jobs SET begun=true WHERE tenant_id=t AND id=j;END IF;
 IF NOT coalesce(valid,false) OR NOT EXISTS(SELECT 1 FROM control.job_routes WHERE tenant_id=t AND id=j AND release_hash=h) THEN RETURN false;END IF;
 INSERT INTO control.worker_context VALUES(pg_backend_pid(),pg_current_xact_id(),principal,t,j,k,g,tok);
 PERFORM set_config('app.tenant_id',t::text,true),set_config('app.job_id',j::text,true);
 IF NOT EXISTS(SELECT 1 FROM control.worker_authority()) THEN
   DELETE FROM control.worker_context WHERE backend=pg_backend_pid();RETURN false;
 END IF;
 RETURN true;
END $function$
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION iam.worker_tenant_scope(candidate uuid) RETURNS boolean
 LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control.worker_authority() a WHERE a.tenant_id=candidate AND a.actor_id=iam.authenticated_actor())
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION iam.worker_identity(job uuid) RETURNS TABLE(actor uuid,tenant uuid,issuer text,subject text)
 LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT a.actor_id,a.tenant_id,u.issuer,u.subject FROM control.worker_authority() a JOIN iam.users u ON u.id=a.actor_id WHERE a.job_id=job
$$;
-- +goose StatementEnd

CREATE POLICY worker_user_boundary ON iam.users AS RESTRICTIVE FOR SELECT TO framework_worker
 USING(id IN(SELECT actor_id FROM control.worker_authority()));
ALTER POLICY route_worker ON control.job_routes USING(EXISTS(SELECT 1 FROM control.worker_authority() a WHERE a.tenant_id=job_routes.tenant_id AND a.job_id=job_routes.id));

-- Table-level REVOKE does not remove column grants.
REVOKE ALL ON runtime.records,runtime.record_sets,runtime.record_links,runtime.unique_projections,control.metadata_heads,control.metadata_versions,runtime.outbox_intents FROM framework_worker;
REVOKE INSERT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data),UPDATE(custom_data,row_version,updated_at,updated_by) ON runtime.records FROM framework_worker;
REVOKE UPDATE(write_version) ON runtime.record_sets FROM framework_worker;
REVOKE INSERT(tenant_id,id,app_id,metadata_revision,ir_hash,entity_id,record_id,record_version,event_type,request_id,traceparent) ON runtime.outbox_intents FROM framework_worker;
CREATE POLICY worker_share_boundary ON runtime.record_shares AS RESTRICTIVE TO framework_worker
 USING(EXISTS(SELECT 1 FROM control.worker_authority() a JOIN runtime.workflow_bindings b ON b.tenant_id=a.tenant_id AND b.id=a.job_id
 WHERE a.kind='workflow' AND b.tenant_id=record_shares.tenant_id AND b.app_id=record_shares.app_id AND b.metadata_revision=record_shares.metadata_revision
 AND b.ir_hash=record_shares.ir_hash AND b.record_id=record_shares.id AND record_shares.user_id=a.actor_id));

-- The read port returns only the declared source fields; no arbitrary record
-- arguments or direct SELECT on records are granted to workers.
-- +goose StatementBegin
CREATE FUNCTION runtime.worker_source_record() RETURNS SETOF runtime.records
 LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE b runtime.workflow_bindings%ROWTYPE; rec runtime.records%ROWTYPE;
BEGIN
 SELECT w.* INTO b FROM control.worker_authority() a JOIN runtime.workflow_bindings w ON w.tenant_id=a.tenant_id AND w.id=a.job_id WHERE a.kind='workflow';
 IF NOT FOUND THEN RETURN;END IF;
 SELECT r.* INTO rec FROM runtime.records r WHERE r.tenant_id=b.tenant_id AND r.app_id=b.app_id AND r.metadata_revision=b.metadata_revision
 AND r.ir_hash=b.ir_hash AND r.entity_id=(convert_from(b.plan_body,'UTF8')::jsonb->>'entity_id') AND r.id=b.record_id;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT coalesce(jsonb_object_agg(f.key,f.value),'{}'::jsonb) INTO rec.custom_data FROM jsonb_each(rec.custom_data) f
 WHERE f.key IN(SELECT jsonb_array_elements_text(convert_from(b.inputs,'UTF8')::jsonb->'fields'));
 RETURN NEXT rec;
END $$;
-- +goose StatementEnd

-- Worker polls already issued leases, never issues/renews or chooses an audience.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.assigned_workflow()
 RETURNS TABLE(tenant_id uuid, id uuid, ir_hash text, generation bigint, token uuid)
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
 SELECT j.tenant_id,j.id,j.ir_hash,j.generation,j.token FROM control.workflow_jobs j
 JOIN control.worker_principals p ON p.tenant_id=j.tenant_id AND p.role_id=j.audience AND p.active
 JOIN pg_roles r ON r.oid=p.role_id AND r.rolname=p.role_name AND r.rolname=session_user
 WHERE control.worker_principal() AND j.state='RUNNABLE' AND j.lease_until>clock_timestamp()
 ORDER BY j.due_at,j.id LIMIT 1
$function$
-- +goose StatementEnd

-- +goose StatementBegin
DO $$ DECLARE f regprocedure; BEGIN
 FOREACH f IN ARRAY ARRAY['control.worker_principal()'::regprocedure,'control.worker_authority()'::regprocedure,
 'control.bind_worker_job(uuid,uuid,text,bigint,uuid,text)'::regprocedure,'runtime.worker_source_record()'::regprocedure,'control.assigned_workflow()'::regprocedure] LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC',f);
 EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO framework_worker',f);
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control.claim_workflow(uuid),control.resolve_event(uuid,uuid,text,text,bigint,uuid) FROM framework_worker;
GRANT EXECUTE ON FUNCTION control.claim_workflow(uuid) TO framework_dispatcher;

-- Routing name is dispatcher-only and bound to the issued attempt. The broker
-- destination is not selected by a worker or by an incoming pointer.
-- +goose StatementBegin
CREATE FUNCTION control.event_audience(t uuid,e uuid,g bigint,tok uuid) RETURNS text
 LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'framework_dispatcher' THEN RAISE EXCEPTION 'dispatcher required';END IF;
 RETURN (SELECT p.role_name::text FROM control.event_attempts a JOIN control.worker_principals p ON p.role_id=a.audience AND p.tenant_id=a.tenant_id AND p.active
 WHERE a.tenant_id=t AND a.id=e AND a.generation=g AND a.lease_token=tok);
END $$;
-- +goose StatementEnd
ALTER FUNCTION control.event_audience(uuid,uuid,bigint,uuid) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.event_audience(uuid,uuid,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.event_audience(uuid,uuid,bigint,uuid) TO framework_dispatcher;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.claim_workflow(token_in uuid) RETURNS TABLE(tenant_id uuid,id uuid,ir_hash text,generation bigint,token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE j control.workflow_jobs%ROWTYPE;BEGIN
 IF session_user<>'framework_dispatcher' OR token_in IS NULL THEN RAISE EXCEPTION 'dispatcher required';END IF;
 SELECT x.* INTO j FROM control.workflow_jobs x WHERE x.state='RUNNABLE' AND x.due_at<=clock_timestamp()
 AND EXISTS(SELECT 1 FROM control.worker_principals p JOIN pg_roles r ON r.oid=p.role_id AND r.rolname=p.role_name WHERE p.tenant_id=x.tenant_id AND p.active AND r.rolcanlogin AND pg_has_role(r.oid,'framework_worker','USAGE'))
 AND (x.lease_until IS NULL OR x.lease_until<=clock_timestamp()) ORDER BY x.due_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 IF j.token IS NOT NULL THEN INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(j.tenant_id,j.id,j.generation,'EXPIRED',j.token) ON CONFLICT DO NOTHING;END IF;
 IF j.lease_failures>=20 THEN
 UPDATE control.workflow_jobs x SET state='DEAD' WHERE x.tenant_id=j.tenant_id AND x.id=j.id;
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(j.tenant_id,j.id,j.generation,'DEAD',j.token) ON CONFLICT DO NOTHING;RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM control.job_routes r JOIN iam.users u ON u.id=r.actor_id JOIN iam.memberships m ON m.user_id=u.id AND m.tenant_id=r.tenant_id JOIN iam.tenants t ON t.id=r.tenant_id WHERE r.tenant_id=j.tenant_id AND r.id=j.id AND u.active AND m.active AND t.active AND m.role<>'viewer') THEN
 UPDATE control.workflow_jobs x SET state='BLOCKED',generation=x.generation+1,token=NULL,lease_until=NULL WHERE x.tenant_id=j.tenant_id AND x.id=j.id;
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase) VALUES(j.tenant_id,j.id,j.generation,'BLOCKED') ON CONFLICT DO NOTHING;RETURN;END IF;
 UPDATE control.workflow_jobs x SET generation=x.generation+1,token=token_in,lease_until=clock_timestamp()+interval '5 seconds',due_at=clock_timestamp()+interval '5 seconds',lease_failures=x.lease_failures+CASE WHEN x.begun THEN 1 ELSE 0 END,begun=false,audience=(SELECT p.role_id FROM control.worker_principals p WHERE p.tenant_id=x.tenant_id AND p.active)
 WHERE x.tenant_id=j.tenant_id AND x.id=j.id RETURNING x.* INTO j;
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(j.tenant_id,j.id,j.generation,'CLAIMED',j.token);
 RETURN QUERY SELECT j.tenant_id,j.id,j.ir_hash,j.generation,j.token;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.claim_event(token uuid) RETURNS TABLE(tenant_id uuid,id uuid,body_hash text,consumer_id text,generation bigint,lease_token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE d control.event_deliveries%ROWTYPE;BEGIN
 IF session_user<>'framework_dispatcher' OR token IS NULL THEN RAISE EXCEPTION 'dispatcher required';END IF;
 SELECT x.* INTO d FROM control.event_deliveries x WHERE x.state IN('PENDING','LEASED','SENT')
 AND EXISTS(SELECT 1 FROM control.worker_principals p JOIN pg_roles r ON r.oid=p.role_id AND r.rolname=p.role_name WHERE p.tenant_id=x.tenant_id AND p.active AND r.rolcanlogin AND pg_has_role(r.oid,'framework_worker','USAGE'))
 AND x.next_at<=clock_timestamp() AND (x.state<>'LEASED' OR x.lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM control.event_deliveries p WHERE p.tenant_id=x.tenant_id AND p.aggregate_key=x.aggregate_key AND p.sequence<x.sequence AND p.state<>'COMPLETED')
 ORDER BY x.next_at,x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE control.event_attempts a SET finished_at=clock_timestamp(),outcome=CASE WHEN d.state='LEASED' THEN 'LEASE_EXPIRED' ELSE 'RECEIPT_TIMEOUT' END
 WHERE a.tenant_id=d.tenant_id AND a.id=d.id AND a.generation=d.generation AND a.finished_at IS NULL;
 IF d.attempts>=5 THEN
 UPDATE control.event_deliveries x SET state='DEAD',last_error=CASE WHEN d.state='LEASED' THEN 'lease_expired' ELSE 'receipt_timeout' END WHERE x.tenant_id=d.tenant_id AND x.id=d.id;
 RETURN;END IF;
 UPDATE control.event_deliveries x SET audience=(SELECT p.role_id FROM control.worker_principals p WHERE p.tenant_id=x.tenant_id AND p.active),state='LEASED',generation=x.generation+1,attempts=x.attempts+1,lease_token=token,
 lease_until=clock_timestamp()+interval '5 seconds',next_at=clock_timestamp()+interval '5 seconds',last_error=''
 WHERE x.tenant_id=d.tenant_id AND x.id=d.id RETURNING x.* INTO d;
 INSERT INTO control.event_attempts(tenant_id,id,generation,lease_token,audience) VALUES(d.tenant_id,d.id,d.generation,token,d.audience);
 RETURN QUERY SELECT d.tenant_id,d.id,d.body_hash,d.consumer_id,d.generation,d.lease_token;END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.resolve_event_v2(t uuid,e uuid,h text,c text,g bigint,token uuid)
 RETURNS TABLE(release_hash text,completed boolean,ready boolean,dead boolean)
 LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NOT control.worker_principal() THEN RAISE EXCEPTION 'worker required';END IF;
 RETURN QUERY SELECT j.release_hash,d.state='COMPLETED',d.generation=g AND d.lease_token=token AND d.published_generation=g AND d.state='SENT' AND d.next_at>clock_timestamp(),d.state='DEAD'
 FROM control.event_deliveries d JOIN control.event_attempts a USING(tenant_id,id)
 JOIN control.job_routes j USING(tenant_id,id)
 WHERE d.tenant_id=t AND d.id=e AND d.body_hash=h AND d.consumer_id=c AND a.generation=g AND a.lease_token=token AND g>0 AND a.audience=(SELECT oid FROM pg_roles WHERE rolname=session_user)
 AND EXISTS(SELECT 1 FROM control.worker_principals p WHERE p.role_id=a.audience AND p.role_name=session_user AND p.tenant_id=t AND p.active);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.check_workflow_lease(t uuid, i uuid, h text, g bigint, tok uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
DECLARE j control.workflow_jobs%ROWTYPE;BEGIN
 IF NOT control.worker_principal() OR NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true) THEN RAISE EXCEPTION 'scoped worker required';END IF;
 SELECT * INTO j FROM control.workflow_jobs WHERE tenant_id=t AND id=i FOR UPDATE;
 RETURN FOUND AND j.state='RUNNABLE' AND j.ir_hash=h AND j.generation=g AND j.token=tok AND j.lease_until>clock_timestamp();
END $function$
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.schedule_workflow(t uuid,i uuid) RETURNS void
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE s runtime.workflow_instances%ROWTYPE;BEGIN
 IF NOT iam.tenant_matches_scope(t) OR (session_user<>'framework_app' AND NOT control.worker_principal()) OR (session_user<>'framework_app' AND (NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true))) THEN RAISE EXCEPTION 'scoped workflow actor required';END IF;
 SELECT * INTO s FROM runtime.workflow_instances WHERE tenant_id=t AND id=i;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM runtime.workflow_history h WHERE h.tenant_id=t AND h.instance_id=i AND h.sequence=s.version) OR NOT EXISTS(SELECT 1 FROM runtime.workflow_outbox o WHERE o.tenant_id=t AND o.instance_id=i AND o.sequence=s.version) THEN RAISE EXCEPTION 'atomic workflow evidence required';END IF;
 UPDATE control.workflow_jobs SET state=CASE WHEN s.status IN('SUCCEEDED','FAILED','CANCELLED','TIMED_OUT','DEAD','RECONCILE','COMPENSATION_FAILED') THEN 'FINISHED' ELSE 'RUNNABLE' END,
 due_at=CASE WHEN s.status='COMPENSATING' THEN s.due_at ELSE least(s.due_at,s.deadline) END,
 generation=generation+1,token=NULL,lease_until=NULL,lease_failures=0 WHERE tenant_id=t AND id=i;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.workflow_route_state(t uuid,i uuid) RETURNS text
 LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NOT iam.tenant_matches_scope(t) OR (session_user<>'framework_app' AND NOT control.worker_principal()) OR (session_user<>'framework_app' AND (NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true))) THEN RAISE EXCEPTION 'scoped workflow actor required';END IF;
 RETURN (SELECT state FROM control.workflow_jobs WHERE tenant_id=t AND id=i);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.complete_event(t uuid,e uuid,h text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NOT control.worker_principal() OR NOT iam.worker_tenant_scope(t) OR e::text IS DISTINCT FROM current_setting('app.job_id',true) THEN RAISE EXCEPTION 'scoped worker required';END IF;
 IF NOT EXISTS(SELECT 1 FROM runtime.event_inbox i WHERE i.tenant_id=t AND i.event_id=e AND i.body_hash=h AND i.consumer_id='preview.receipt.v1') THEN RAISE EXCEPTION 'committed transaction receipt required';END IF;
 UPDATE control.event_deliveries SET state='COMPLETED',completed_at=clock_timestamp() WHERE tenant_id=t AND id=e AND body_hash=h AND state IN('SENT','LEASED','PENDING');
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE control.event_attempts SET finished_at=clock_timestamp(),outcome='CONSUMED' WHERE tenant_id=t AND id=e AND finished_at IS NULL;
 RETURN true;END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.quarantine_event(h text,r text) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN IF NOT control.worker_principal() THEN RAISE EXCEPTION 'worker required';END IF;
 INSERT INTO control.event_quarantine(pointer_hash,reason) VALUES(h,r) ON CONFLICT DO NOTHING;END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'worker capability cutover is forward-only; quiesce workers and forward-fix';END $$;
-- +goose StatementEnd
