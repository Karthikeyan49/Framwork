-- Additive, forward-only M8. Existing intents remain immutable legacy evidence.
-- +goose Up
ALTER TABLE runtime.outbox_intents ADD COLUMN request_id text NOT NULL DEFAULT '' CHECK(length(request_id)<=64);
ALTER TABLE runtime.outbox_intents ADD COLUMN traceparent text NOT NULL DEFAULT '' CHECK(traceparent='' OR traceparent ~ '^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$');
GRANT INSERT(request_id,traceparent) ON runtime.outbox_intents TO framework_app,framework_worker;
CREATE TRIGGER immutable_intent BEFORE UPDATE OR DELETE ON runtime.outbox_intents FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE runtime.event_sequences(
 tenant_id uuid NOT NULL,aggregate_key text NOT NULL,sequence bigint NOT NULL CHECK(sequence>0),PRIMARY KEY(tenant_id,aggregate_key)
);
SELECT runtime.install_tenant_rls('runtime.event_sequences');
REVOKE ALL ON runtime.event_sequences FROM framework_app;
CREATE TABLE runtime.event_envelopes(
 tenant_id uuid NOT NULL,id uuid NOT NULL,body bytea NOT NULL CHECK(octet_length(body)<=8192),
 body_hash text NOT NULL CHECK(body_hash=encode(sha256(body),'hex')),PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,id) REFERENCES runtime.outbox_intents(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.event_envelopes');
REVOKE ALL ON runtime.event_envelopes FROM framework_app;
CREATE POLICY event_worker ON runtime.event_envelopes FOR SELECT TO framework_worker
 USING(iam.worker_tenant_scope(tenant_id) AND id::text=current_setting('app.job_id',true));
GRANT SELECT ON runtime.event_envelopes TO framework_worker;
CREATE TRIGGER immutable_envelope BEFORE UPDATE OR DELETE ON runtime.event_envelopes FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE control.event_deliveries(
 tenant_id uuid NOT NULL,id uuid NOT NULL,body_hash text NOT NULL CHECK(body_hash ~ '^[0-9a-f]{64}$'),
 consumer_id text NOT NULL DEFAULT 'preview.receipt.v1' CHECK(consumer_id='preview.receipt.v1'),
 aggregate_key text NOT NULL,sequence bigint NOT NULL CHECK(sequence>0),
 state text NOT NULL CHECK(state IN('PENDING','LEASED','SENT','COMPLETED','DEAD','LEGACY_PAUSED')),
 generation bigint NOT NULL DEFAULT 0 CHECK(generation>=0),attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 lease_token uuid,lease_until timestamptz,next_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 published_generation bigint NOT NULL DEFAULT 0,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,last_error text NOT NULL DEFAULT '' CHECK(last_error IN('','publish_failed','receipt_timeout','lease_expired','consumer_failed')),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,aggregate_key,sequence)
);
SELECT runtime.install_tenant_rls('control.event_deliveries');
REVOKE ALL ON control.event_deliveries FROM framework_app;
CREATE INDEX delivery_due ON control.event_deliveries(next_at,created_at) WHERE state IN('PENDING','LEASED','SENT');
CREATE TABLE control.event_attempts(
 tenant_id uuid NOT NULL,id uuid NOT NULL,generation bigint NOT NULL,lease_token uuid NOT NULL,
 claimed_at timestamptz NOT NULL DEFAULT clock_timestamp(),published_at timestamptz,finished_at timestamptz,outcome text NOT NULL DEFAULT 'CLAIMED',
 PRIMARY KEY(tenant_id,id,generation),FOREIGN KEY(tenant_id,id) REFERENCES control.event_deliveries(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.event_attempts');
REVOKE ALL ON control.event_attempts FROM framework_app;
CREATE TABLE runtime.event_inbox(
 tenant_id uuid NOT NULL,consumer_id text NOT NULL CHECK(consumer_id='preview.receipt.v1'),event_id uuid NOT NULL,
 body_hash text NOT NULL CHECK(body_hash ~ '^[0-9a-f]{64}$'),completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,consumer_id,event_id),FOREIGN KEY(tenant_id,event_id) REFERENCES runtime.event_envelopes(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.event_inbox');
REVOKE ALL ON runtime.event_inbox FROM framework_app;
CREATE POLICY inbox_worker ON runtime.event_inbox TO framework_worker
 USING(iam.worker_tenant_scope(tenant_id) AND event_id::text=current_setting('app.job_id',true))
 WITH CHECK(iam.worker_tenant_scope(tenant_id) AND event_id::text=current_setting('app.job_id',true)
 AND body_hash=(SELECT e.body_hash FROM runtime.event_envelopes e WHERE e.tenant_id=event_inbox.tenant_id AND e.id=event_inbox.event_id));
GRANT SELECT ON runtime.event_inbox TO framework_worker;
GRANT INSERT(tenant_id,consumer_id,event_id,body_hash) ON runtime.event_inbox TO framework_worker;
CREATE TRIGGER immutable_inbox BEFORE UPDATE OR DELETE ON runtime.event_inbox FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();

CREATE TABLE control.event_replays(
 tenant_id uuid NOT NULL,id uuid NOT NULL,generation bigint NOT NULL,operator_ref text NOT NULL CHECK(length(operator_ref) BETWEEN 1 AND 64),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 200),occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 writer_role text NOT NULL DEFAULT session_user,PRIMARY KEY(tenant_id,id,generation)
);
SELECT runtime.install_tenant_rls('control.event_replays');
REVOKE ALL ON control.event_replays FROM framework_app;
CREATE TRIGGER immutable_replay BEFORE UPDATE OR DELETE ON control.event_replays FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
-- Quarantine stores no untrusted broker body, only an identity hash and reason.
CREATE TABLE control.event_quarantine(
 tenant_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',pointer_hash text NOT NULL CHECK(pointer_hash ~ '^[0-9a-f]{64}$'),
 reason text NOT NULL CHECK(reason IN('malformed','unroutable')),occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,pointer_hash)
);
SELECT runtime.install_tenant_rls('control.event_quarantine');
REVOKE ALL ON control.event_quarantine FROM framework_app;

-- Existing intents do not get invented snapshots, routes or actionable sequences.
INSERT INTO control.event_deliveries(tenant_id,id,body_hash,aggregate_key,sequence,state)
 SELECT tenant_id,id,repeat('0',64),'legacy:'||id::text,1,'LEGACY_PAUSED' FROM runtime.outbox_intents;

-- +goose StatementBegin
CREATE FUNCTION runtime.freeze_event() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE k text;s bigint;b bytea;h text;BEGIN
 IF NOT iam.tenant_matches_scope(NEW.tenant_id) OR NEW.actor_id IS DISTINCT FROM iam.authenticated_actor()
 OR (session_user='framework_worker' AND NOT iam.worker_tenant_scope(NEW.tenant_id))
 OR session_user NOT IN('framework_app','framework_worker') THEN RAISE EXCEPTION 'trusted event producer required';END IF;
 k:=jsonb_build_array('m8-v1',NEW.app_id,NEW.metadata_revision,NEW.ir_hash,NEW.entity_id,NEW.record_id)::text;
 INSERT INTO runtime.event_sequences(tenant_id,aggregate_key,sequence) VALUES(NEW.tenant_id,k,1)
 ON CONFLICT(tenant_id,aggregate_key) DO UPDATE SET sequence=event_sequences.sequence+1 RETURNING sequence INTO s;
 b:=convert_to(jsonb_build_object('schema_version','1.0.0','epoch','m8-v1','mode','preview','topic','record.changes.v1',
 'id',NEW.id,'tenant_id',NEW.tenant_id,'actor_id',NEW.actor_id,'app_id',NEW.app_id,'metadata_revision',NEW.metadata_revision,
 'ir_hash',NEW.ir_hash,'aggregate_type',NEW.entity_id,'aggregate_id',NEW.record_id,'aggregate_sequence',s,
 'event_type',NEW.event_type,'occurred_at',to_char(NEW.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
 'request_id',NEW.request_id,'traceparent',NEW.traceparent,'payload',jsonb_build_object('record_id',NEW.record_id,'record_version',NEW.record_version))::text,'UTF8');
 h:=encode(sha256(b),'hex');
 INSERT INTO runtime.event_envelopes(tenant_id,id,body,body_hash) VALUES(NEW.tenant_id,NEW.id,b,h);
 INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES(NEW.tenant_id,NEW.id,NEW.actor_id,NEW.ir_hash);
 INSERT INTO control.event_deliveries(tenant_id,id,body_hash,aggregate_key,sequence,state) VALUES(NEW.tenant_id,NEW.id,h,k,s,'PENDING');
 RETURN NEW;END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.freeze_event() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.freeze_event() FROM PUBLIC;
CREATE TRIGGER freeze_event AFTER INSERT ON runtime.outbox_intents FOR EACH ROW EXECUTE FUNCTION runtime.freeze_event();

-- One fenced lease, transaction ends before any broker network operation.
-- +goose StatementBegin
CREATE FUNCTION control.claim_event(token uuid) RETURNS TABLE(tenant_id uuid,id uuid,body_hash text,consumer_id text,generation bigint,lease_token uuid)
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE d control.event_deliveries%ROWTYPE;BEGIN
 IF session_user<>'framework_dispatcher' OR token IS NULL THEN RAISE EXCEPTION 'dispatcher required';END IF;
 SELECT x.* INTO d FROM control.event_deliveries x WHERE x.state IN('PENDING','LEASED','SENT')
 AND x.next_at<=clock_timestamp() AND (x.state<>'LEASED' OR x.lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM control.event_deliveries p WHERE p.tenant_id=x.tenant_id AND p.aggregate_key=x.aggregate_key AND p.sequence<x.sequence AND p.state<>'COMPLETED')
 ORDER BY x.next_at,x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE control.event_attempts a SET finished_at=clock_timestamp(),outcome=CASE WHEN d.state='LEASED' THEN 'LEASE_EXPIRED' ELSE 'RECEIPT_TIMEOUT' END
 WHERE a.tenant_id=d.tenant_id AND a.id=d.id AND a.generation=d.generation AND a.finished_at IS NULL;
 IF d.attempts>=5 THEN
 UPDATE control.event_deliveries x SET state='DEAD',last_error=CASE WHEN d.state='LEASED' THEN 'lease_expired' ELSE 'receipt_timeout' END WHERE x.tenant_id=d.tenant_id AND x.id=d.id;
 RETURN;END IF;
 UPDATE control.event_deliveries x SET state='LEASED',generation=x.generation+1,attempts=x.attempts+1,lease_token=token,
 lease_until=clock_timestamp()+interval '5 seconds',next_at=clock_timestamp()+interval '5 seconds',last_error=''
 WHERE x.tenant_id=d.tenant_id AND x.id=d.id RETURNING x.* INTO d;
 INSERT INTO control.event_attempts(tenant_id,id,generation,lease_token) VALUES(d.tenant_id,d.id,d.generation,token);
 RETURN QUERY SELECT d.tenant_id,d.id,d.body_hash,d.consumer_id,d.generation,d.lease_token;END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control.finish_event(t uuid,e uuid,g bigint,token uuid,published boolean) RETURNS boolean
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE d control.event_deliveries%ROWTYPE;BEGIN
 IF session_user<>'framework_dispatcher' THEN RAISE EXCEPTION 'dispatcher required';END IF;
 SELECT * INTO d FROM control.event_deliveries WHERE tenant_id=t AND id=e FOR UPDATE;
 IF NOT FOUND OR d.state<>'LEASED' OR d.generation<>g OR d.lease_token IS DISTINCT FROM token OR d.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE control.event_deliveries SET state=CASE WHEN published THEN 'SENT' WHEN attempts>=5 THEN 'DEAD' ELSE 'PENDING' END,
 next_at=clock_timestamp()+CASE WHEN published THEN interval '5 seconds' ELSE interval '500 milliseconds'*power(2,d.attempts-1) END,
 published_generation=CASE WHEN published THEN g ELSE published_generation END,
 last_error=CASE WHEN published THEN '' ELSE 'publish_failed' END WHERE tenant_id=t AND id=e;
 UPDATE control.event_attempts SET published_at=CASE WHEN published THEN clock_timestamp() ELSE NULL END,
 finished_at=CASE WHEN published THEN NULL ELSE clock_timestamp() END,outcome=CASE WHEN published THEN 'PUBLISHED' ELSE 'PUBLISH_FAILED' END
 WHERE tenant_id=t AND id=e AND generation=g;
 RETURN true;END $$;
-- +goose StatementEnd

-- An untrusted stream pointer can only mint a ticket for an existing exact attempt.
-- +goose StatementBegin
CREATE FUNCTION control.resolve_event(t uuid,e uuid,h text,c text,g bigint,token uuid) RETURNS TABLE(release_hash text,completed boolean,ready boolean)
 LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'framework_worker' THEN RAISE EXCEPTION 'worker required';END IF;
 RETURN QUERY SELECT j.release_hash,d.state='COMPLETED',d.published_generation>=g AND d.state NOT IN('DEAD','LEGACY_PAUSED')
 FROM control.event_deliveries d JOIN control.event_attempts a USING(tenant_id,id)
 JOIN control.job_routes j USING(tenant_id,id)
 WHERE d.tenant_id=t AND d.id=e AND d.body_hash=h AND d.consumer_id=c AND a.generation=g AND a.lease_token=token AND g>0;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control.complete_event(t uuid,e uuid,h text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'framework_worker' OR NOT iam.worker_tenant_scope(t) OR e::text IS DISTINCT FROM current_setting('app.job_id',true) THEN RAISE EXCEPTION 'scoped worker required';END IF;
 IF NOT EXISTS(SELECT 1 FROM runtime.event_inbox i WHERE i.tenant_id=t AND i.event_id=e AND i.body_hash=h AND i.consumer_id='preview.receipt.v1') THEN RAISE EXCEPTION 'committed transaction receipt required';END IF;
 UPDATE control.event_deliveries SET state='COMPLETED',completed_at=clock_timestamp() WHERE tenant_id=t AND id=e AND body_hash=h AND state IN('SENT','LEASED','PENDING');
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE control.event_attempts SET finished_at=clock_timestamp(),outcome='CONSUMED' WHERE tenant_id=t AND id=e AND finished_at IS NULL;
 RETURN true;END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control.quarantine_event(h text,r text) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN IF session_user<>'framework_worker' THEN RAISE EXCEPTION 'worker required';END IF;
 INSERT INTO control.event_quarantine(pointer_hash,reason) VALUES(h,r) ON CONFLICT DO NOTHING;END $$;
-- +goose StatementEnd

-- Controlled replay preserves immutable body, identity, inbox and attempt history.
-- +goose StatementBegin
CREATE FUNCTION control.replay_event(t uuid,e uuid,expected bigint,op text,why text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE d control.event_deliveries%ROWTYPE;BEGIN
 IF session_user<>'framework_migrator' THEN RAISE EXCEPTION 'operator required';END IF;
 SELECT * INTO d FROM control.event_deliveries WHERE tenant_id=t AND id=e FOR UPDATE;
 IF NOT FOUND OR d.generation<>expected OR d.state IN('LEGACY_PAUSED','LEASED') THEN RETURN false;END IF;
 INSERT INTO control.event_replays(tenant_id,id,generation,operator_ref,reason) VALUES(t,e,expected,op,why);
 UPDATE control.event_deliveries SET state=CASE WHEN d.state='COMPLETED' THEN 'COMPLETED' ELSE 'PENDING' END,attempts=0,next_at=clock_timestamp(),lease_token=NULL,lease_until=NULL,last_error='' WHERE tenant_id=t AND id=e;
 RETURN true;END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$ DECLARE f record;BEGIN
 FOR f IN SELECT p.oid::regprocedure sig FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('claim_event','finish_event','resolve_event','complete_event','quarantine_event','replay_event') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f.sig);EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f.sig);
 END LOOP;END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.claim_event(uuid),control.finish_event(uuid,uuid,bigint,uuid,boolean) TO framework_dispatcher;
GRANT EXECUTE ON FUNCTION control.resolve_event(uuid,uuid,text,text,bigint,uuid),control.complete_event(uuid,uuid,text),control.quarantine_event(text,text) TO framework_worker;
GRANT EXECUTE ON FUNCTION control.replay_event(uuid,uuid,bigint,text,text) TO framework_migrator;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only M8 recovery; retain events, attempts and inbox';END $$;
-- +goose StatementEnd
