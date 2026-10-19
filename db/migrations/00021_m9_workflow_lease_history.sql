-- Append-only routing evidence complements business history across process crashes.
-- No payload access or tenant data mutation. Forward-only.
-- +goose Up
CREATE TABLE control.workflow_attempts(
 tenant_id uuid NOT NULL,id uuid NOT NULL,generation bigint NOT NULL,
 phase text NOT NULL CHECK(phase IN('CLAIMED','EXPIRED','FINISHED','BLOCKED','DEAD')),
 token uuid,occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id,generation,phase),FOREIGN KEY(tenant_id,id) REFERENCES control.workflow_jobs(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.workflow_attempts');
REVOKE ALL ON control.workflow_attempts FROM framework_app;
CREATE TRIGGER immutable_workflow_attempt BEFORE UPDATE OR DELETE ON control.workflow_attempts FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.claim_workflow(token_in uuid) RETURNS TABLE(tenant_id uuid,id uuid,ir_hash text,generation bigint,token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE j control.workflow_jobs%ROWTYPE;BEGIN
 IF session_user<>'framework_worker' OR token_in IS NULL THEN RAISE EXCEPTION 'worker required';END IF;
 SELECT x.* INTO j FROM control.workflow_jobs x WHERE x.state='RUNNABLE' AND x.due_at<=clock_timestamp()
 AND (x.lease_until IS NULL OR x.lease_until<=clock_timestamp()) ORDER BY x.due_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 IF j.token IS NOT NULL THEN INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(j.tenant_id,j.id,j.generation,'EXPIRED',j.token) ON CONFLICT DO NOTHING;END IF;
 IF j.lease_failures>=20 THEN
 UPDATE control.workflow_jobs x SET state='DEAD' WHERE x.tenant_id=j.tenant_id AND x.id=j.id;
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(j.tenant_id,j.id,j.generation,'DEAD',j.token) ON CONFLICT DO NOTHING;RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM control.job_routes r JOIN iam.users u ON u.id=r.actor_id JOIN iam.memberships m ON m.user_id=u.id AND m.tenant_id=r.tenant_id JOIN iam.tenants t ON t.id=r.tenant_id WHERE r.tenant_id=j.tenant_id AND r.id=j.id AND u.active AND m.active AND t.active AND m.role<>'viewer') THEN
 UPDATE control.workflow_jobs x SET state='BLOCKED',generation=x.generation+1,token=NULL,lease_until=NULL WHERE x.tenant_id=j.tenant_id AND x.id=j.id;
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase) VALUES(j.tenant_id,j.id,j.generation,'BLOCKED') ON CONFLICT DO NOTHING;RETURN;END IF;
 UPDATE control.workflow_jobs x SET generation=x.generation+1,token=token_in,lease_until=clock_timestamp()+interval '5 seconds',due_at=clock_timestamp()+interval '5 seconds',lease_failures=x.lease_failures+1
 WHERE x.tenant_id=j.tenant_id AND x.id=j.id RETURNING x.* INTO j;
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(j.tenant_id,j.id,j.generation,'CLAIMED',j.token);
 RETURN QUERY SELECT j.tenant_id,j.id,j.ir_hash,j.generation,j.token;
END $$;
-- +goose StatementEnd

-- A separate trigger records completed lease facts without changing schedule semantics.
-- +goose StatementBegin
CREATE FUNCTION control.workflow_attempt_finished() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF OLD.token IS NOT NULL AND NEW.token IS NULL AND NEW.state<>'BLOCKED' THEN
 INSERT INTO control.workflow_attempts(tenant_id,id,generation,phase,token) VALUES(OLD.tenant_id,OLD.id,OLD.generation,'FINISHED',OLD.token) ON CONFLICT DO NOTHING;
 END IF;RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION control.workflow_attempt_finished() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.workflow_attempt_finished() FROM PUBLIC;
CREATE TRIGGER workflow_attempt_finished AFTER UPDATE ON control.workflow_jobs FOR EACH ROW EXECUTE FUNCTION control.workflow_attempt_finished();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain workflow attempt evidence; use a forward fix';END $$;
-- +goose StatementEnd
