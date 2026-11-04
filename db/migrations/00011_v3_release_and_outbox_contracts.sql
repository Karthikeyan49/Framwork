-- Additive M6R contracts. No activation endpoint, dispatcher or domain DDL.
-- +goose Up
CREATE TABLE control.release_bundles(
 tenant_id uuid NOT NULL,app_id text NOT NULL,artifact_hash text NOT NULL,
 body bytea NOT NULL CHECK(octet_length(body)<=8388608),created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,artifact_hash),
 CHECK(artifact_hash=encode(sha256(body),'hex'))
);
SELECT runtime.install_tenant_rls('control.release_bundles');
REVOKE INSERT,UPDATE,DELETE ON control.release_bundles FROM framework_app;
GRANT INSERT(tenant_id,app_id,artifact_hash,body) ON control.release_bundles TO framework_app;
CREATE TABLE control.release_approvals(
 tenant_id uuid NOT NULL,app_id text NOT NULL,artifact_hash text NOT NULL,
 actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),approved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,artifact_hash,actor_id),
 FOREIGN KEY(tenant_id,app_id,artifact_hash) REFERENCES control.release_bundles(tenant_id,app_id,artifact_hash)
);
SELECT runtime.install_tenant_rls('control.release_approvals');
DROP POLICY tenant_scope ON control.release_approvals;
CREATE POLICY approval_read ON control.release_approvals FOR SELECT TO framework_app USING(iam.tenant_matches_scope(tenant_id));
CREATE POLICY approval_insert ON control.release_approvals FOR INSERT TO framework_app WITH CHECK(
 iam.tenant_matches_scope(tenant_id) AND actor_id=iam.authenticated_actor() AND EXISTS(
 SELECT 1 FROM control.release_bundles b WHERE b.tenant_id=release_approvals.tenant_id AND b.app_id=release_approvals.app_id AND b.artifact_hash=release_approvals.artifact_hash
 AND (NOT (convert_from(b.body,'UTF8')::jsonb->>'HighRisk')::boolean OR convert_from(b.body,'UTF8')::jsonb->>'RequesterID'<>actor_id::text)));
REVOKE INSERT,UPDATE,DELETE ON control.release_approvals FROM framework_app;
GRANT INSERT(tenant_id,app_id,artifact_hash) ON control.release_approvals TO framework_app;
CREATE TABLE control.active_releases(
 tenant_id uuid NOT NULL,app_id text NOT NULL,artifact_hash text NOT NULL,generation bigint NOT NULL CHECK(generation>0),
 PRIMARY KEY(tenant_id,app_id),FOREIGN KEY(tenant_id,app_id,artifact_hash) REFERENCES control.release_bundles(tenant_id,app_id,artifact_hash)
);
SELECT runtime.install_tenant_rls('control.active_releases');
DROP POLICY tenant_scope ON control.active_releases;
CREATE POLICY active_read ON control.active_releases FOR SELECT TO framework_app USING(iam.tenant_matches_scope(tenant_id));
CREATE POLICY active_write ON control.active_releases TO framework_app USING(iam.tenant_matches_scope(tenant_id)) WITH CHECK(
 iam.tenant_matches_scope(tenant_id) AND EXISTS(SELECT 1 FROM control.release_approvals a WHERE a.tenant_id=active_releases.tenant_id AND a.app_id=active_releases.app_id AND a.artifact_hash=active_releases.artifact_hash));
REVOKE DELETE,UPDATE ON control.active_releases FROM framework_app;
GRANT UPDATE(artifact_hash,generation) ON control.active_releases TO framework_app;
-- Guard immutability even against accidental operator UPDATE/DELETE. Recovery
-- copies releases or switches pointers; changing an approved bundle is forbidden.
-- +goose StatementBegin
CREATE FUNCTION control.immutable_artifact() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN RAISE EXCEPTION 'immutable artifact';END $$;
-- +goose StatementEnd
ALTER FUNCTION control.immutable_artifact() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.immutable_artifact() FROM PUBLIC;
CREATE TRIGGER immutable_bundle BEFORE UPDATE OR DELETE ON control.release_bundles FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER immutable_approval BEFORE UPDATE OR DELETE ON control.release_approvals FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TABLE runtime.outbox_intents(
 tenant_id uuid NOT NULL,id uuid NOT NULL,actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,record_id uuid NOT NULL,record_version bigint NOT NULL,
 event_type text NOT NULL CHECK(event_type IN('record.created','record.updated','record.deleted')),
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),CHECK(record_version>0),CHECK(ir_hash ~ '^[0-9a-f]{64}$')
);
SELECT runtime.install_tenant_rls('runtime.outbox_intents');
REVOKE INSERT,UPDATE,DELETE ON runtime.outbox_intents FROM framework_app;
GRANT INSERT(tenant_id,id,app_id,metadata_revision,ir_hash,entity_id,record_id,record_version,event_type) ON runtime.outbox_intents TO framework_app,framework_worker;
GRANT SELECT ON runtime.outbox_intents TO framework_worker;
CREATE POLICY worker_outbox ON runtime.outbox_intents TO framework_worker USING(iam.worker_tenant_scope(tenant_id)) WITH CHECK(iam.worker_tenant_scope(tenant_id) AND actor_id=iam.authenticated_actor());
CREATE POLICY worker_release ON control.release_bundles FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(tenant_id) AND artifact_hash=(SELECT j.release_hash FROM control.job_routes j WHERE j.tenant_id=release_bundles.tenant_id AND j.id::text=current_setting('app.job_id',true)));
GRANT SELECT ON control.release_bundles TO framework_worker;
-- Stamp updates server-side; an API/worker cannot forge another actor's stamp.
-- +goose StatementBegin
CREATE FUNCTION runtime.stamp_record_update() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN NEW.updated_by:=iam.authenticated_actor();IF NEW.updated_by IS NULL THEN RAISE EXCEPTION 'trusted actor required';END IF;RETURN NEW;END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.stamp_record_update() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.stamp_record_update() FROM PUBLIC;
CREATE TRIGGER record_update_actor BEFORE UPDATE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.stamp_record_update();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only release/outbox recovery; preserve data';END $$;
-- +goose StatementEnd
