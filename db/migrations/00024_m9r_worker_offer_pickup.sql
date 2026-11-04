-- One committed pickup per issued offer, preserving the crash lease fence.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.assigned_workflow() RETURNS TABLE(tenant_id uuid,id uuid,ir_hash text,generation bigint,token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE issued control.workflow_jobs%ROWTYPE;BEGIN
 SELECT j.* INTO issued FROM control.workflow_jobs j
 JOIN control.worker_principals p ON p.tenant_id=j.tenant_id AND p.role_id=j.audience AND p.active
 JOIN pg_roles r ON r.oid=p.role_id AND r.rolname=p.role_name AND r.rolname=session_user
 WHERE control.worker_principal() AND j.state='RUNNABLE' AND NOT j.begun AND j.lease_until>clock_timestamp()
 ORDER BY j.due_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 -- Pickup commits before business execution. Offline audiences do not consume
 -- execution-failure budget; expired picked-up offers retain crash evidence.
 UPDATE control.workflow_jobs j SET begun=true WHERE j.tenant_id=issued.tenant_id AND j.id=issued.id;
 RETURN QUERY SELECT issued.tenant_id,issued.id,issued.ir_hash,issued.generation,issued.token;
END;
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only worker pickup fence';END $$;
-- +goose StatementEnd
