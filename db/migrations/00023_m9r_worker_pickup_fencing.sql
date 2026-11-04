-- Preserve migration22 as applied. Refine pickup evidence, role drift and stale-lease denial.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.assign_worker(principal_name text,t uuid,enabled boolean) RETURNS void
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
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
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.assigned_workflow() RETURNS TABLE(tenant_id uuid,id uuid,ir_hash text,generation bigint,token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE issued control.workflow_jobs%ROWTYPE;BEGIN
 SELECT j.* INTO issued FROM control.workflow_jobs j
 JOIN control.worker_principals p ON p.tenant_id=j.tenant_id AND p.role_id=j.audience AND p.active
 JOIN pg_roles r ON r.oid=p.role_id AND r.rolname=p.role_name AND r.rolname=session_user
 WHERE control.worker_principal() AND j.state='RUNNABLE' AND j.lease_until>clock_timestamp()
 ORDER BY j.due_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 -- Pickup commits before business execution. Offline audiences do not consume
 -- execution-failure budget; expired picked-up offers retain crash evidence.
 UPDATE control.workflow_jobs j SET begun=true WHERE j.tenant_id=issued.tenant_id AND j.id=issued.id;
 RETURN QUERY SELECT issued.tenant_id,issued.id,issued.ir_hash,issued.generation,issued.token;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.bind_worker_job(t uuid,j uuid,k text,g bigint,tok uuid,h text) RETURNS boolean
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE principal oid; valid boolean:=false; prior control.worker_context%ROWTYPE;
BEGIN
 SELECT * INTO prior FROM control.worker_context WHERE backend=pg_backend_pid();
 DELETE FROM control.worker_context WHERE backend=pg_backend_pid();
 -- A transaction may bind once; failed rebind removes authority.
 IF prior.transaction_id=pg_current_xact_id() THEN RETURN false;END IF;
 SELECT r.oid INTO principal FROM pg_roles r JOIN control.worker_principals p ON p.role_id=r.oid AND p.role_name=r.rolname
 WHERE r.rolname=session_user AND p.tenant_id=t AND p.active AND control.worker_principal();
 IF principal IS NULL OR g<1 OR tok IS NULL OR h IS NULL THEN RETURN false;END IF;
 -- Reject invalid tickets before acquiring source/instance locks. Revalidate
 -- under the fence afterward so a racing rollover cannot exploit the precheck.
 IF k='workflow' AND NOT EXISTS(SELECT 1 FROM control.workflow_jobs x WHERE x.tenant_id=t AND x.id=j AND x.audience=principal
 AND x.generation=g AND x.token=tok AND x.ir_hash=h AND x.state='RUNNABLE' AND x.lease_until>clock_timestamp()) THEN RETURN false;END IF;
 IF k='event' AND NOT EXISTS(SELECT 1 FROM control.event_deliveries d WHERE d.tenant_id=t AND d.id=j AND d.audience=principal
 AND d.generation=g AND d.lease_token=tok AND d.state='SENT' AND d.published_generation=g AND d.next_at>clock_timestamp()) THEN RETURN false;END IF;
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
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.check_workflow_lease(t uuid,i uuid,h text,g bigint,tok uuid) RETURNS boolean
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE j control.workflow_jobs%ROWTYPE;BEGIN
 IF NOT control.worker_principal() OR NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true) THEN RETURN false;END IF;
 SELECT * INTO j FROM control.workflow_jobs WHERE tenant_id=t AND id=i FOR UPDATE;
 RETURN FOUND AND j.state='RUNNABLE' AND j.ir_hash=h AND j.generation=g AND j.token=tok AND j.lease_until>clock_timestamp();
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_principal() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM pg_roles r JOIN control.worker_principals p ON p.role_id=r.oid AND p.role_name=r.rolname
 WHERE r.rolname=session_user AND r.rolcanlogin AND NOT(r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication)
 AND pg_has_role(session_user,'framework_worker','USAGE') AND p.active)
 AND NOT EXISTS(SELECT 1 FROM pg_roles danger WHERE danger.rolname<>session_user
 AND (danger.rolsuper OR danger.rolbypassrls OR danger.rolcreaterole OR danger.rolcreatedb OR danger.rolreplication
 OR danger.rolname IN('framework_app','framework_dispatcher','framework_migrator','framework_schema_owner','framework_audit_writer','pg_read_all_data','pg_write_all_data','pg_read_server_files','pg_write_server_files','pg_execute_server_program'))
 AND pg_has_role(session_user,danger.oid,'MEMBER'))
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only worker capability refinement';END $$;
-- +goose StatementEnd
