-- Forward refinement: distinguish durable dead-letter facts from transient publication.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION control.resolve_event_v2(t uuid,e uuid,h text,c text,g bigint,token uuid)
 RETURNS TABLE(release_hash text,completed boolean,ready boolean,dead boolean)
 LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'framework_worker' THEN RAISE EXCEPTION 'worker required';END IF;
 RETURN QUERY SELECT j.release_hash,d.state='COMPLETED',d.published_generation>=g AND d.state NOT IN('DEAD','LEGACY_PAUSED'),d.state='DEAD'
 FROM control.event_deliveries d JOIN control.event_attempts a USING(tenant_id,id)
 JOIN control.job_routes j USING(tenant_id,id)
 WHERE d.tenant_id=t AND d.id=e AND d.body_hash=h AND d.consumer_id=c AND a.generation=g AND a.lease_token=token AND g>0;
END $$;
-- +goose StatementEnd
ALTER FUNCTION control.resolve_event_v2(uuid,uuid,text,text,bigint,uuid) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.resolve_event_v2(uuid,uuid,text,text,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.resolve_event_v2(uuid,uuid,text,text,bigint,uuid) TO framework_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only event routing recovery';END $$;
-- +goose StatementEnd
