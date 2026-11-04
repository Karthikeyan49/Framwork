-- ADR062. No existing event, deadline, token, state or attempt is rewritten.
-- Quiesce owned dispatchers, apply forward, validate readiness, then restart.
-- +goose Up
ALTER TABLE control.event_deliveries DROP CONSTRAINT event_deliveries_attempts_check;
ALTER TABLE control.event_deliveries ADD CONSTRAINT event_deliveries_attempts_check CHECK(attempts BETWEEN 0 AND 20);

-- Public runtime contract metadata, never tenant routing or business contents.
-- +goose StatementBegin
CREATE FUNCTION control.event_delivery_policy()
 RETURNS TABLE(version text,lease_ms integer,receipt_ms integer,max_attempts integer,retry_base_ms integer,retry_cap_ms integer)
 LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS $$
 SELECT 'event-outage-v2'::text,5000,900000,20,500,60000
$$;
-- +goose StatementEnd
ALTER FUNCTION control.event_delivery_policy() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.event_delivery_policy() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.event_delivery_policy() TO framework_app,framework_worker,framework_dispatcher,framework_migrator;

-- Private pure helper. Only trusted function owners may call it; invalid input
-- yields NULL and cannot create an unbounded/negative retry timestamp.
-- +goose StatementBegin
CREATE FUNCTION control.event_retry_delay(n integer) RETURNS interval
 LANGUAGE sql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
 SELECT CASE WHEN n BETWEEN 1 AND 20
 THEN interval '1 millisecond'*least(60000::double precision,500*power(2::double precision,n-1))
 ELSE NULL::interval END
$$;
-- +goose StatementEnd
ALTER FUNCTION control.event_retry_delay(integer) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.event_retry_delay(integer) FROM PUBLIC,framework_app,framework_worker,framework_dispatcher;

-- Identical routing signature and audience/resource boundary. Only timing and
-- attempt count change; old schema1.1 callers continue using the same ports.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.claim_event(token uuid)
 RETURNS TABLE(tenant_id uuid,id uuid,body_hash text,consumer_id text,generation bigint,lease_token uuid)
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
 IF d.attempts>=20 THEN
 UPDATE control.event_deliveries x SET state='DEAD',last_error=CASE WHEN d.state='LEASED' THEN 'lease_expired' ELSE 'receipt_timeout' END WHERE x.tenant_id=d.tenant_id AND x.id=d.id;
 RETURN;END IF;
 UPDATE control.event_deliveries x SET audience=(SELECT p.role_id FROM control.worker_principals p WHERE p.tenant_id=x.tenant_id AND p.active),
 state='LEASED',generation=x.generation+1,attempts=x.attempts+1,lease_token=token,
 lease_until=clock_timestamp()+interval '5 seconds',
 next_at=clock_timestamp()+interval '5 seconds'+control.event_retry_delay(x.attempts+1),last_error=''
 WHERE x.tenant_id=d.tenant_id AND x.id=d.id RETURNING x.* INTO d;
 INSERT INTO control.event_attempts(tenant_id,id,generation,lease_token,audience) VALUES(d.tenant_id,d.id,d.generation,token,d.audience);
 RETURN QUERY SELECT d.tenant_id,d.id,d.body_hash,d.consumer_id,d.generation,d.lease_token;END $$;
-- +goose StatementEnd

-- Confirmed publish starts the receipt window; it does not renew the broker
-- lease. Late/stale finish calls still fail before changing state or evidence.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.finish_event(t uuid,e uuid,g bigint,token uuid,published boolean) RETURNS boolean
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE d control.event_deliveries%ROWTYPE;BEGIN
 IF session_user<>'framework_dispatcher' THEN RAISE EXCEPTION 'dispatcher required';END IF;
 SELECT * INTO d FROM control.event_deliveries WHERE tenant_id=t AND id=e FOR UPDATE;
 IF NOT FOUND OR d.state<>'LEASED' OR d.generation<>g OR d.lease_token IS DISTINCT FROM token OR d.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE control.event_deliveries SET state=CASE WHEN published THEN 'SENT' WHEN attempts>=20 THEN 'DEAD' ELSE 'PENDING' END,
 next_at=clock_timestamp()+CASE WHEN published THEN interval '15 minutes' ELSE control.event_retry_delay(d.attempts) END,
 published_generation=CASE WHEN published THEN g ELSE published_generation END,
 last_error=CASE WHEN published THEN '' ELSE 'publish_failed' END WHERE tenant_id=t AND id=e;
 UPDATE control.event_attempts SET published_at=CASE WHEN published THEN clock_timestamp() ELSE NULL END,
 finished_at=CASE WHEN published THEN NULL ELSE clock_timestamp() END,outcome=CASE WHEN published THEN 'PUBLISHED' ELSE 'PUBLISH_FAILED' END
 WHERE tenant_id=t AND id=e AND generation=g;
 RETURN true;END $$;
-- +goose StatementEnd
ALTER FUNCTION control.claim_event(uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION control.finish_event(uuid,uuid,bigint,uuid,boolean) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.claim_event(uuid),control.finish_event(uuid,uuid,bigint,uuid,boolean) FROM PUBLIC,framework_app,framework_worker;
GRANT EXECUTE ON FUNCTION control.claim_event(uuid),control.finish_event(uuid,uuid,bigint,uuid,boolean) TO framework_dispatcher;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only outage recovery; preserve events, attempts and inbox'; END $$;
-- +goose StatementEnd
