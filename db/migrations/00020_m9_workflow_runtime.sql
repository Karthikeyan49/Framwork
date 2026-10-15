-- Additive M9: immutable workflow inputs/pins, bounded PG durability and evidence.
-- No customer record changes or new tenant DDL privileges. Deploy before binary.
-- Quiesce metadata authoring for the brief constraint lock; forward-only recovery.
-- +goose Up
ALTER TABLE control.metadata_versions DROP CONSTRAINT metadata_versions_schema_version_check;
ALTER TABLE control.metadata_versions ADD CONSTRAINT metadata_versions_schema_version_check
 CHECK(schema_version IN('1.0.0','1.1.0','1.2.0'));

CREATE TABLE runtime.workflow_bindings(
 tenant_id uuid NOT NULL,id uuid NOT NULL,actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 app_id text NOT NULL,metadata_revision bigint NOT NULL CHECK(metadata_revision BETWEEN 1 AND 1000000000),
 ir_hash text NOT NULL CHECK(ir_hash=encode(sha256(ir_body),'hex')),ir_body bytea NOT NULL CHECK(octet_length(ir_body)<=4194304),
 workflow_id text NOT NULL,workflow_version text NOT NULL,
 plan_hash text NOT NULL CHECK(plan_hash=encode(sha256(plan_body),'hex')),plan_body bytea NOT NULL CHECK(octet_length(plan_body)<=1048576),
 record_id uuid NOT NULL,record_version bigint NOT NULL CHECK(record_version>0),
 inputs bytea NOT NULL CHECK(octet_length(inputs)<=65536),input_hash text NOT NULL CHECK(input_hash=encode(sha256(inputs),'hex')),
 start_key text NOT NULL CHECK(length(start_key) BETWEEN 1 AND 160),request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 request_id text NOT NULL CHECK(length(request_id)<=64),traceparent text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,app_id,metadata_revision,ir_hash,workflow_id,start_key)
);
SELECT runtime.install_tenant_rls('runtime.workflow_bindings');
REVOKE INSERT,UPDATE,DELETE ON runtime.workflow_bindings FROM framework_app;
GRANT INSERT(tenant_id,id,app_id,metadata_revision,ir_hash,ir_body,workflow_id,workflow_version,plan_hash,plan_body,record_id,record_version,inputs,input_hash,start_key,request_hash,request_id,traceparent) ON runtime.workflow_bindings TO framework_app;
CREATE TRIGGER immutable_workflow_binding BEFORE UPDATE OR DELETE ON runtime.workflow_bindings FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE runtime.workflow_instances(
 tenant_id uuid NOT NULL,id uuid NOT NULL,state_id text NOT NULL,
 status text NOT NULL CHECK(status IN('READY','WAITING_TIMER','WAITING_APPROVAL','RETRY_WAIT','SUCCEEDED','FAILED','CANCELLED','TIMED_OUT','DEAD','RECONCILE','COMPENSATING','COMPENSATION_FAILED')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),attempt integer NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 10),
 entered_at timestamptz NOT NULL DEFAULT clock_timestamp(),due_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 deadline timestamptz NOT NULL,termination text NOT NULL DEFAULT '' CHECK(termination IN('','CANCELLED','TIMED_OUT','FAILED')),
 failure text NOT NULL DEFAULT '' CHECK(failure IN('','retryable','permanent','unknown','timeout','cancelled','condition','compensation','authorization')),
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,id) REFERENCES runtime.workflow_bindings(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.workflow_instances');
REVOKE DELETE,UPDATE ON runtime.workflow_instances FROM framework_app;
GRANT UPDATE(state_id,status,version,attempt,entered_at,due_at,termination,failure) ON runtime.workflow_instances TO framework_app,framework_worker;

CREATE TABLE runtime.workflow_history(
 tenant_id uuid NOT NULL,instance_id uuid NOT NULL,sequence bigint NOT NULL,state_id text NOT NULL,status text NOT NULL,
 event text NOT NULL CHECK(length(event) BETWEEN 1 AND 40),actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),step_key text NOT NULL DEFAULT '',result_hash text NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,instance_id,sequence),FOREIGN KEY(tenant_id,instance_id) REFERENCES runtime.workflow_instances(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.workflow_history');
REVOKE INSERT,UPDATE,DELETE ON runtime.workflow_history FROM framework_app;
GRANT INSERT(tenant_id,instance_id,sequence,state_id,status,event,step_key,result_hash) ON runtime.workflow_history TO framework_app,framework_worker;
CREATE TRIGGER immutable_workflow_history BEFORE UPDATE OR DELETE ON runtime.workflow_history FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE runtime.workflow_receipts(
 tenant_id uuid NOT NULL,instance_id uuid NOT NULL,step_key text NOT NULL CHECK(length(step_key)<=200),
 activity_ref text NOT NULL,request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 output bytea NOT NULL CHECK(octet_length(output)<=16384),output_hash text NOT NULL CHECK(output_hash=encode(sha256(output),'hex')),
 sequence bigint NOT NULL,compensation_ref text NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,instance_id,step_key),FOREIGN KEY(tenant_id,instance_id) REFERENCES runtime.workflow_instances(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.workflow_receipts');
REVOKE INSERT,UPDATE,DELETE ON runtime.workflow_receipts FROM framework_app;
GRANT INSERT(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) ON runtime.workflow_receipts TO framework_app,framework_worker;
CREATE TRIGGER immutable_workflow_receipt BEFORE UPDATE OR DELETE ON runtime.workflow_receipts FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE runtime.workflow_signals(
 tenant_id uuid NOT NULL,instance_id uuid NOT NULL,idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 160),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),sequence bigint NOT NULL,
 actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 PRIMARY KEY(tenant_id,instance_id,idempotency_key),FOREIGN KEY(tenant_id,instance_id) REFERENCES runtime.workflow_instances(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.workflow_signals');
REVOKE INSERT,UPDATE,DELETE ON runtime.workflow_signals FROM framework_app;
GRANT INSERT(tenant_id,instance_id,idempotency_key,request_hash,sequence) ON runtime.workflow_signals TO framework_app;
CREATE TRIGGER immutable_workflow_signal BEFORE UPDATE OR DELETE ON runtime.workflow_signals FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

-- Owned by the events module: immutable lifecycle intents, no external subscriber yet.
CREATE TABLE runtime.workflow_outbox(
 tenant_id uuid NOT NULL,instance_id uuid NOT NULL,sequence bigint NOT NULL,
 body bytea NOT NULL CHECK(octet_length(body)<=8192),body_hash text NOT NULL CHECK(body_hash=encode(sha256(body),'hex')),
 PRIMARY KEY(tenant_id,instance_id,sequence),FOREIGN KEY(tenant_id,instance_id) REFERENCES runtime.workflow_instances(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.workflow_outbox');
REVOKE INSERT,UPDATE,DELETE ON runtime.workflow_outbox FROM framework_app;
GRANT INSERT(tenant_id,instance_id,sequence,body,body_hash) ON runtime.workflow_outbox TO framework_app,framework_worker;
CREATE TRIGGER immutable_workflow_outbox BEFORE UPDATE OR DELETE ON runtime.workflow_outbox FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE control.workflow_jobs(
 tenant_id uuid NOT NULL,id uuid NOT NULL,ir_hash text NOT NULL,
 state text NOT NULL DEFAULT 'RUNNABLE' CHECK(state IN('RUNNABLE','FINISHED','BLOCKED','DEAD')),
 due_at timestamptz NOT NULL DEFAULT clock_timestamp(),generation bigint NOT NULL DEFAULT 0 CHECK(generation>=0),
 token uuid,lease_until timestamptz,lease_failures integer NOT NULL DEFAULT 0 CHECK(lease_failures BETWEEN 0 AND 20),
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,id) REFERENCES control.job_routes(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.workflow_jobs');
REVOKE ALL ON control.workflow_jobs FROM framework_app;
CREATE INDEX workflow_due ON control.workflow_jobs(due_at,id) WHERE state='RUNNABLE';

-- +goose StatementBegin
DO $$ DECLARE tab text;keycol text;BEGIN
 FOREACH tab IN ARRAY ARRAY['workflow_bindings','workflow_instances','workflow_history','workflow_receipts','workflow_signals','workflow_outbox'] LOOP
 keycol:=CASE WHEN tab IN('workflow_bindings','workflow_instances') THEN 'id' ELSE 'instance_id' END;
 EXECUTE format('CREATE POLICY workflow_worker ON runtime.%I TO framework_worker USING(iam.worker_tenant_scope(tenant_id) AND %I::text=current_setting(''app.job_id'',true)) WITH CHECK(iam.worker_tenant_scope(tenant_id) AND %I::text=current_setting(''app.job_id'',true))',tab,keycol,keycol);
 EXECUTE format('GRANT SELECT ON runtime.%I TO framework_worker',tab);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION runtime.route_workflow() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(NEW.tenant_id) OR NEW.actor_id IS DISTINCT FROM iam.authenticated_actor() THEN RAISE EXCEPTION 'trusted workflow producer required';END IF;
 INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES(NEW.tenant_id,NEW.id,NEW.actor_id,NEW.ir_hash);
 INSERT INTO control.workflow_jobs(tenant_id,id,ir_hash) VALUES(NEW.tenant_id,NEW.id,NEW.ir_hash);
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.route_workflow() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.route_workflow() FROM PUBLIC;
CREATE TRIGGER route_workflow AFTER INSERT ON runtime.workflow_bindings FOR EACH ROW EXECUTE FUNCTION runtime.route_workflow();

-- Routing-only claim. No payload access; live membership is rechecked in WithJob.
-- +goose StatementBegin
CREATE FUNCTION control.claim_workflow(token_in uuid) RETURNS TABLE(tenant_id uuid,id uuid,ir_hash text,generation bigint,token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE j control.workflow_jobs%ROWTYPE;BEGIN
 IF session_user<>'framework_worker' OR token_in IS NULL THEN RAISE EXCEPTION 'worker required';END IF;
 SELECT x.* INTO j FROM control.workflow_jobs x WHERE x.state='RUNNABLE' AND x.due_at<=clock_timestamp()
 AND (x.lease_until IS NULL OR x.lease_until<=clock_timestamp()) ORDER BY x.due_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 IF j.lease_failures>=20 THEN UPDATE control.workflow_jobs x SET state='DEAD' WHERE x.tenant_id=j.tenant_id AND x.id=j.id;RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM control.job_routes r JOIN iam.users u ON u.id=r.actor_id JOIN iam.memberships m ON m.user_id=u.id AND m.tenant_id=r.tenant_id JOIN iam.tenants t ON t.id=r.tenant_id WHERE r.tenant_id=j.tenant_id AND r.id=j.id AND u.active AND m.active AND t.active AND m.role<>'viewer') THEN
 UPDATE control.workflow_jobs x SET state='BLOCKED',generation=x.generation+1,token=NULL,lease_until=NULL WHERE x.tenant_id=j.tenant_id AND x.id=j.id;RETURN;END IF;
 UPDATE control.workflow_jobs x SET generation=x.generation+1,token=token_in,lease_until=clock_timestamp()+interval '5 seconds',due_at=clock_timestamp()+interval '5 seconds',lease_failures=x.lease_failures+1
 WHERE x.tenant_id=j.tenant_id AND x.id=j.id RETURNING x.* INTO j;
 RETURN QUERY SELECT j.tenant_id,j.id,j.ir_hash,j.generation,j.token;
END $$;
-- +goose StatementEnd

-- Exact live lease lookup, locking with the business transaction after instance lock.
-- +goose StatementBegin
CREATE FUNCTION control.check_workflow_lease(t uuid,i uuid,h text,g bigint,tok uuid) RETURNS boolean
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE j control.workflow_jobs%ROWTYPE;BEGIN
 IF session_user<>'framework_worker' OR NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true) THEN RAISE EXCEPTION 'scoped worker required';END IF;
 SELECT * INTO j FROM control.workflow_jobs WHERE tenant_id=t AND id=i FOR UPDATE;
 RETURN FOUND AND j.state='RUNNABLE' AND j.ir_hash=h AND j.generation=g AND j.token=tok AND j.lease_until>clock_timestamp();
END $$;
-- +goose StatementEnd

-- Schedule derives from durable business state; callers cannot choose another pin.
-- +goose StatementBegin
CREATE FUNCTION control.schedule_workflow(t uuid,i uuid) RETURNS void
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE s runtime.workflow_instances%ROWTYPE;BEGIN
 IF NOT iam.tenant_matches_scope(t) OR session_user NOT IN('framework_app','framework_worker') OR (session_user='framework_worker' AND (NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true))) THEN RAISE EXCEPTION 'scoped workflow actor required';END IF;
 SELECT * INTO s FROM runtime.workflow_instances WHERE tenant_id=t AND id=i;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM runtime.workflow_history h WHERE h.tenant_id=t AND h.instance_id=i AND h.sequence=s.version) OR NOT EXISTS(SELECT 1 FROM runtime.workflow_outbox o WHERE o.tenant_id=t AND o.instance_id=i AND o.sequence=s.version) THEN RAISE EXCEPTION 'atomic workflow evidence required';END IF;
 UPDATE control.workflow_jobs SET state=CASE WHEN s.status IN('SUCCEEDED','FAILED','CANCELLED','TIMED_OUT','DEAD','RECONCILE','COMPENSATION_FAILED') THEN 'FINISHED' ELSE 'RUNNABLE' END,
 due_at=CASE WHEN s.status='COMPENSATING' THEN s.due_at ELSE least(s.due_at,s.deadline) END,
 generation=generation+1,token=NULL,lease_until=NULL,lease_failures=0 WHERE tenant_id=t AND id=i;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control.workflow_route_state(t uuid,i uuid) RETURNS text
 LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NOT iam.tenant_matches_scope(t) OR session_user NOT IN('framework_app','framework_worker') OR (session_user='framework_worker' AND (NOT iam.worker_tenant_scope(t) OR i::text IS DISTINCT FROM current_setting('app.job_id',true))) THEN RAISE EXCEPTION 'scoped workflow actor required';END IF;
 RETURN (SELECT state FROM control.workflow_jobs WHERE tenant_id=t AND id=i);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$ DECLARE f record;BEGIN
 FOR f IN SELECT p.oid::regprocedure sig FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('claim_workflow','check_workflow_lease','schedule_workflow','workflow_route_state') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f.sig);EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f.sig);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.claim_workflow(uuid),control.check_workflow_lease(uuid,uuid,text,bigint,uuid) TO framework_worker;
GRANT EXECUTE ON FUNCTION control.schedule_workflow(uuid,uuid),control.workflow_route_state(uuid,uuid) TO framework_app,framework_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow pins, receipts and history require a reviewed forward fix';END $$;
-- +goose StatementEnd
