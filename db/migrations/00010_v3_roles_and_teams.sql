-- Purpose: explicit V3 role taxonomy and single-job worker boundary. Forward-only.
-- Administrator role bootstrap precedes this controlled migration; no default passwords.
-- +goose Up
GRANT USAGE,CREATE ON SCHEMA iam,runtime,audit,control TO framework_schema_owner;
CREATE TABLE iam.teams (
 tenant_id uuid NOT NULL,id uuid NOT NULL,parent_id uuid,name text NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,parent_id) REFERENCES iam.teams(tenant_id,id),CHECK(parent_id IS DISTINCT FROM id)
);
ALTER TABLE iam.teams ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam.teams FORCE ROW LEVEL SECURITY;
CREATE POLICY team_operator ON iam.teams TO framework_migrator,framework_schema_owner USING(true) WITH CHECK(true);
CREATE POLICY team_scope ON iam.teams FOR SELECT TO framework_app,framework_worker USING(iam.tenant_matches_scope(tenant_id));
CREATE TABLE iam.team_memberships (
 tenant_id uuid NOT NULL,team_id uuid NOT NULL,user_id uuid NOT NULL,active boolean NOT NULL DEFAULT true,
 PRIMARY KEY(tenant_id,team_id,user_id),FOREIGN KEY(tenant_id,team_id) REFERENCES iam.teams(tenant_id,id)
);
ALTER TABLE iam.team_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam.team_memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY team_member_operator ON iam.team_memberships TO framework_migrator,framework_schema_owner USING(true) WITH CHECK(true);
CREATE POLICY team_member_scope ON iam.team_memberships FOR SELECT TO framework_app,framework_worker USING(iam.tenant_matches_scope(tenant_id) AND user_id=iam.authenticated_actor());
GRANT SELECT ON iam.teams,iam.team_memberships TO framework_app,framework_worker;
CREATE TABLE control.job_routes (
 tenant_id uuid NOT NULL,id uuid NOT NULL,actor_id uuid NOT NULL,release_hash text NOT NULL CHECK(release_hash ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,id)
);
-- Routing has no business payload. Cross-tenant dispatcher may see these IDs only.
ALTER TABLE control.job_routes ENABLE ROW LEVEL SECURITY;
ALTER TABLE control.job_routes FORCE ROW LEVEL SECURITY;
CREATE POLICY route_operator ON control.job_routes TO framework_migrator,framework_schema_owner USING(true) WITH CHECK(true);
CREATE POLICY route_dispatch ON control.job_routes FOR SELECT TO framework_dispatcher USING(true);
CREATE POLICY route_api ON control.job_routes TO framework_app USING(iam.tenant_matches_scope(tenant_id)) WITH CHECK(iam.tenant_matches_scope(tenant_id) AND actor_id=iam.authenticated_actor());
CREATE POLICY route_worker ON control.job_routes FOR SELECT TO framework_worker USING(id::text=current_setting('app.job_id',true) AND tenant_id=iam.current_tenant_id());
GRANT SELECT,INSERT ON control.job_routes TO framework_app;
GRANT SELECT ON control.job_routes TO framework_worker,framework_dispatcher;
GRANT USAGE ON SCHEMA control TO framework_dispatcher;
-- +goose StatementBegin
CREATE FUNCTION iam.worker_tenant_scope(candidate uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT session_user='framework_worker' AND iam.tenant_matches_scope(candidate) AND EXISTS(
 SELECT 1 FROM control.job_routes j WHERE j.tenant_id=candidate AND j.id::text=current_setting('app.job_id',true) AND j.actor_id=iam.authenticated_actor())
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION iam.worker_tenant_scope(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION iam.worker_tenant_scope(uuid) TO framework_worker;
-- Narrow live delegated identity lookup; caller binds auth+tenant before business reads.
-- +goose StatementBegin
CREATE FUNCTION iam.worker_identity(job uuid) RETURNS TABLE(actor uuid,tenant uuid,issuer text,subject text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT u.id,j.tenant_id,u.issuer,u.subject FROM control.job_routes j JOIN iam.users u ON u.id=j.actor_id
 JOIN iam.memberships m ON m.tenant_id=j.tenant_id AND m.user_id=u.id
 JOIN iam.tenants t ON t.id=j.tenant_id
 WHERE session_user='framework_worker' AND j.id=job AND j.tenant_id=iam.current_tenant_id() AND job::text=current_setting('app.job_id',true) AND u.active AND m.active AND t.active
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION iam.worker_identity(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION iam.worker_identity(uuid) TO framework_worker;
GRANT USAGE ON SCHEMA iam,runtime,audit,control TO framework_worker;
GRANT SELECT ON iam.users,iam.tenants,iam.memberships,iam.roles,control.metadata_versions,control.metadata_heads TO framework_worker;
GRANT EXECUTE ON FUNCTION iam.authenticated_actor(),iam.current_tenant_id(),iam.tenant_matches_scope(uuid) TO framework_worker;
GRANT INSERT ON audit.entries TO framework_worker,framework_audit_writer;
GRANT USAGE ON SCHEMA audit,iam TO framework_audit_writer;
ALTER POLICY authenticated_user ON iam.users TO framework_app,framework_worker;
ALTER POLICY authenticated_membership ON iam.memberships TO framework_app,framework_worker;
ALTER POLICY authenticated_tenant ON iam.tenants TO framework_app,framework_worker;
CREATE POLICY worker_audit ON audit.entries FOR INSERT TO framework_worker WITH CHECK(actor_id=iam.authenticated_actor() AND iam.worker_tenant_scope(tenant_id));
CREATE POLICY worker_versions ON control.metadata_versions FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(tenant_id));
CREATE POLICY worker_heads ON control.metadata_heads FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(tenant_id));
-- +goose StatementBegin
DO $$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['records','record_sets','record_links','record_shares','unique_projections'] LOOP
 EXECUTE format('CREATE POLICY worker_scope ON runtime.%I TO framework_worker USING(iam.worker_tenant_scope(tenant_id)) WITH CHECK(iam.worker_tenant_scope(tenant_id))',tab);
 EXECUTE format('GRANT SELECT ON runtime.%I TO framework_worker',tab);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT INSERT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data),UPDATE(custom_data,row_version,updated_at,updated_by),DELETE ON runtime.records TO framework_worker;
GRANT INSERT,UPDATE(write_version) ON runtime.record_sets TO framework_worker;
GRANT INSERT,DELETE ON runtime.record_links TO framework_worker;
-- RLS applies even to NOLOGIN owner for privileged lookup functions.
-- +goose StatementBegin
DO $$ DECLARE t record; f record; schema_name text; BEGIN
 IF NOT has_database_privilege('framework_schema_owner',current_database(),'CREATE') THEN
 EXECUTE format('GRANT CREATE ON DATABASE %I TO framework_schema_owner',current_database()); END IF;
 FOR t IN SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname IN('iam','runtime','audit','control') AND c.relkind IN('r','p') LOOP
  IF t.nspname='iam' AND t.relname='roles' THEN NULL;
  ELSE EXECUTE format('CREATE POLICY schema_owner_access ON %I.%I TO framework_schema_owner USING(true) WITH CHECK(true)',t.nspname,t.relname); END IF;
  EXECUTE format('ALTER TABLE %I.%I OWNER TO framework_schema_owner',t.nspname,t.relname);
 END LOOP;
 FOR f IN SELECT p.oid::regprocedure AS signature FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname IN('iam','runtime','audit','control') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f.signature); END LOOP;
 FOREACH schema_name IN ARRAY ARRAY['iam','runtime','audit','control'] LOOP
 EXECUTE format('ALTER SCHEMA %I OWNER TO framework_schema_owner',schema_name); END LOOP;
END $$;
-- +goose StatementEnd
-- Update installer for additive future V3 migrations; role remains migration-only.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.install_tenant_rls(target regclass) RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE schema_name name;table_name name;tenant_column smallint;BEGIN
 SELECT n.nspname,c.relname INTO schema_name,table_name FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE c.oid=target AND n.nspname IN('runtime','control') AND c.relkind='r' AND NOT c.relispartition
 AND (c.relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user) OR (c.relowner='framework_schema_owner'::regrole AND pg_has_role(current_user,'framework_schema_owner','USAGE')));
 IF schema_name IS NULL OR current_user NOT IN('framework_migrator','framework_schema_owner') THEN RAISE EXCEPTION 'migration-owned tenant table required';END IF;
 SELECT attnum INTO tenant_column FROM pg_attribute WHERE attrelid=target AND attname='tenant_id' AND atttypid='uuid'::regtype AND attnotnull AND NOT attisdropped;
 IF tenant_column IS NULL OR NOT EXISTS(SELECT 1 FROM pg_index WHERE indrelid=target AND indisprimary)
 OR EXISTS(SELECT 1 FROM pg_index WHERE indrelid=target AND indisunique AND NOT tenant_column=ANY((indkey::smallint[])[0:indnkeyatts-1]))
 OR EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid=target AND contype='x') THEN RAISE EXCEPTION 'tenant-inclusive keys required';END IF;
 IF EXISTS(SELECT 1 FROM pg_constraint fk WHERE fk.conrelid=target AND fk.contype='f' AND NOT EXISTS(SELECT 1 FROM pg_attribute ref WHERE ref.attrelid=fk.confrelid AND ref.attname='tenant_id' AND EXISTS(SELECT 1 FROM unnest(fk.conkey,fk.confkey) pair(local_col,remote_col) WHERE pair.local_col=tenant_column AND pair.remote_col=ref.attnum))) THEN RAISE EXCEPTION 'tenant-inclusive FK required';END IF;
 IF EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=target) THEN RAISE EXCEPTION 'existing policies require review';END IF;
 EXECUTE format('REVOKE ALL ON TABLE %I.%I FROM PUBLIC,framework_app',schema_name,table_name);
 EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY',schema_name,table_name);
 EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY',schema_name,table_name);
 EXECUTE format('CREATE POLICY tenant_scope ON %I.%I TO framework_app USING(tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id)) WITH CHECK(tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id))',schema_name,table_name);
 EXECUTE format('CREATE POLICY schema_owner_access ON %I.%I TO framework_schema_owner USING(true) WITH CHECK(true)',schema_name,table_name);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I.%I TO framework_app',schema_name,table_name);
 EXECUTE format('ALTER TABLE %I.%I OWNER TO framework_schema_owner',schema_name,table_name);
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION runtime.install_tenant_rls(regclass) FROM PUBLIC,framework_app,framework_worker,framework_dispatcher;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'role/security recovery requires controlled forward fix'; END $$;
-- +goose StatementEnd
