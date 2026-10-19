-- Versioned capability authority. Bootstrap preserves documented V1 roles.
-- +goose Up
ALTER TABLE iam.roles DROP CONSTRAINT roles_name_check;
ALTER TABLE iam.roles ADD CHECK(name ~ '^[a-z][a-z0-9_]{0,63}$');
-- This is now an operator-only identifier registry. Runtime discovery must
-- use the tenant-scoped heads (API) or the exact delegated definition (worker).
REVOKE SELECT ON iam.roles FROM framework_app,framework_worker;
CREATE TABLE control.role_versions(
 tenant_id uuid NOT NULL,name text NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 1000000000),
 operation_id uuid NOT NULL,base_version bigint NOT NULL CHECK(base_version>=0),body bytea NOT NULL,body_hash text NOT NULL,
 active boolean NOT NULL,bootstrap boolean NOT NULL DEFAULT false,created_by uuid,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,name,version),UNIQUE(tenant_id,operation_id),
 FOREIGN KEY(tenant_id) REFERENCES iam.tenants(id),
 CHECK(name ~ '^[a-z][a-z0-9_]{0,63}$' AND octet_length(body) BETWEEN 1 AND 65536 AND body_hash=encode(sha256(body),'hex'))
);
-- IAM tenant PK is itself the tenant key; installer requires its literal name.
ALTER TABLE control.role_versions OWNER TO framework_schema_owner;
ALTER TABLE control.role_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE control.role_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY schema_owner_access ON control.role_versions TO framework_schema_owner USING(true) WITH CHECK(true);
CREATE POLICY tenant_scope ON control.role_versions TO framework_app USING(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id())));
CREATE POLICY worker_no_generic ON control.role_versions AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
GRANT SELECT ON control.role_versions TO framework_app;
CREATE TABLE control.role_heads(
 tenant_id uuid NOT NULL,name text NOT NULL,version bigint NOT NULL,active boolean NOT NULL,
 PRIMARY KEY(tenant_id,name),FOREIGN KEY(tenant_id,name,version) REFERENCES control.role_versions(tenant_id,name,version)
);
SELECT runtime.install_tenant_rls('control.role_heads');
REVOKE INSERT,UPDATE,DELETE ON control.role_heads FROM framework_app;
CREATE POLICY worker_no_generic ON control.role_heads AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER immutable_role_version BEFORE UPDATE OR DELETE OR TRUNCATE ON control.role_versions FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_role_head BEFORE DELETE OR TRUNCATE ON control.role_heads FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
CREATE FUNCTION control.seed_tenant_roles(t uuid) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE n text;b bytea;
BEGIN
 IF NOT pg_has_role(session_user,'framework_migrator','MEMBER') THEN RAISE EXCEPTION 'operator bootstrap required' USING ERRCODE='42501';END IF;
 FOREACH n IN ARRAY ARRAY['owner','admin','member','viewer'] LOOP
 b:=convert_to(jsonb_build_object('schema_version','tenant-capabilities-v1','name',n,'grants','[]'::jsonb)::text,'UTF8');
 INSERT INTO control.role_versions(tenant_id,name,version,operation_id,base_version,body,body_hash,active,bootstrap) VALUES(t,n,1,gen_random_uuid(),0,b,encode(sha256(b),'hex'),true,true) ON CONFLICT(tenant_id,name,version) DO NOTHING;
 INSERT INTO control.role_heads VALUES(t,n,1,true) ON CONFLICT DO NOTHING;
 END LOOP;
END $$;
CREATE FUNCTION control.initial_tenant_roles() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$ BEGIN PERFORM control.seed_tenant_roles(NEW.id);RETURN NEW;END $$;
CREATE FUNCTION control.role_manager(t uuid) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR NOT iam.tenant_matches_scope(t) THEN RAISE EXCEPTION 'active tenant owner required' USING ERRCODE='42501';END IF;
 -- A changed row forces stale REPEATABLE READ managers to abort. A lock-only
 -- fence permits two owners to demote themselves from snapshots showing two.
 UPDATE iam.tenants SET name=name WHERE id=t;
 PERFORM 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role='owner' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'active tenant owner required' USING ERRCODE='42501';END IF;
END $$;
CREATE FUNCTION control.publish_role(t uuid,n text,op uuid,expected bigint,b bytea,h text,enabled boolean) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE prior control.role_versions%ROWTYPE;current bigint;doc jsonb;
BEGIN
 PERFORM control.role_manager(t);
 IF n IS NULL OR n='owner' OR n !~ '^[a-z][a-z0-9_]{0,63}$' OR op IS NULL OR expected IS NULL OR expected<0 OR expected>=1000000000 OR b IS NULL OR octet_length(b) NOT BETWEEN 1 AND 65536 OR enabled IS NULL OR h IS DISTINCT FROM encode(sha256(b),'hex') THEN RAISE EXCEPTION 'invalid role witness' USING ERRCODE='22023';END IF;
 doc:=convert_from(b,'UTF8')::jsonb;
 IF doc->>'name' IS DISTINCT FROM n OR doc->>'schema_version' IS DISTINCT FROM 'tenant-capabilities-v1' OR jsonb_typeof(doc->'grants') IS DISTINCT FROM 'array' OR jsonb_array_length(doc->'grants')>64 THEN RAISE EXCEPTION 'invalid role definition' USING ERRCODE='22023';END IF;
 SELECT x.* INTO prior FROM control.role_versions x WHERE x.tenant_id=t AND x.operation_id=op;
 IF FOUND THEN IF prior.name<>n OR prior.base_version<>expected OR prior.body_hash<>h OR prior.active<>enabled THEN RAISE EXCEPTION 'role operation conflict' USING ERRCODE='55000';END IF;RETURN;END IF;
 SELECT x.version INTO current FROM control.role_heads x WHERE x.tenant_id=t AND x.name=n FOR UPDATE;
 IF coalesce(current,0)<>expected THEN RAISE EXCEPTION 'role base changed' USING ERRCODE='55000';END IF;
 INSERT INTO iam.roles(name) VALUES(n) ON CONFLICT DO NOTHING;
 INSERT INTO control.role_versions(tenant_id,name,version,operation_id,base_version,body,body_hash,active,created_by) VALUES(t,n,expected+1,op,expected,b,h,enabled,iam.authenticated_actor());
 INSERT INTO control.role_heads VALUES(t,n,expected+1,enabled) ON CONFLICT(tenant_id,name) DO UPDATE SET version=excluded.version,active=excluded.active;
END $$;
CREATE FUNCTION control.role_head_guard() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.name IS DISTINCT FROM OLD.name OR NEW.version<>OLD.version+1 OR NEW.name='owner') THEN RAISE EXCEPTION 'role head must advance exactly once' USING ERRCODE='55000';END IF;
 IF NOT EXISTS(SELECT 1 FROM control.role_versions v WHERE v.tenant_id=NEW.tenant_id AND v.name=NEW.name AND v.version=NEW.version AND v.active=NEW.active AND (NEW.name<>'owner' OR v.bootstrap AND v.version=1 AND v.active)) THEN RAISE EXCEPTION 'role head witness mismatch' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION control.assign_role(t uuid,u uuid,n text,expected_role text) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE m iam.memberships%ROWTYPE;
BEGIN
 PERFORM control.role_manager(t);
 IF NOT EXISTS(SELECT 1 FROM control.role_heads h WHERE h.tenant_id=t AND h.name=n AND h.active) THEN RAISE EXCEPTION 'active tenant role required' USING ERRCODE='55000';END IF;
 SELECT x.* INTO m FROM iam.memberships x JOIN iam.users who ON who.id=x.user_id AND who.active WHERE x.tenant_id=t AND x.user_id=u AND x.active FOR UPDATE OF x;
 IF NOT FOUND THEN RAISE EXCEPTION 'active tenant member required' USING ERRCODE='55000';END IF;
 IF expected_role IS NULL OR m.role IS DISTINCT FROM expected_role THEN RAISE EXCEPTION 'member role changed' USING ERRCODE='55000';END IF;
 IF m.role='owner' AND n<>'owner' AND (SELECT count(*) FROM iam.memberships x JOIN iam.users who ON who.id=x.user_id AND who.active WHERE x.tenant_id=t AND x.role='owner' AND x.active)<=1 THEN RAISE EXCEPTION 'cannot remove last owner' USING ERRCODE='55000';END IF;
 UPDATE iam.memberships SET role=n WHERE tenant_id=t AND user_id=u;
END $$;
CREATE FUNCTION control.worker_role_definition() RETURNS TABLE(tenant_id uuid,actor_id uuid,name text,version bigint,body_hash text,active boolean,bootstrap boolean,body bytea) LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT a.tenant_id,a.actor_id,h.name,h.version,v.body_hash,h.active,v.bootstrap,v.body FROM control.worker_authority() a JOIN iam.memberships m ON m.tenant_id=a.tenant_id AND m.user_id=a.actor_id AND m.active JOIN control.role_heads h ON h.tenant_id=m.tenant_id AND h.name=m.role JOIN control.role_versions v ON v.tenant_id=h.tenant_id AND v.name=h.name AND v.version=h.version
$$;
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
ALTER FUNCTION control.seed_tenant_roles(uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION control.initial_tenant_roles() OWNER TO framework_schema_owner;
ALTER FUNCTION control.role_manager(uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION control.role_head_guard() OWNER TO framework_schema_owner;
ALTER FUNCTION control.publish_role(uuid,text,uuid,bigint,bytea,text,boolean) OWNER TO framework_schema_owner;
ALTER FUNCTION control.assign_role(uuid,uuid,text,text) OWNER TO framework_schema_owner;
ALTER FUNCTION control.worker_role_definition() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.seed_tenant_roles(uuid),control.initial_tenant_roles(),control.role_manager(uuid),control.publish_role(uuid,text,uuid,bigint,bytea,text,boolean),control.assign_role(uuid,uuid,text,text),control.worker_role_definition() FROM PUBLIC;
REVOKE ALL ON FUNCTION control.role_head_guard() FROM PUBLIC;
CREATE TRIGGER role_head_guard BEFORE INSERT OR UPDATE ON control.role_heads FOR EACH ROW EXECUTE FUNCTION control.role_head_guard();
GRANT EXECUTE ON FUNCTION control.seed_tenant_roles(uuid) TO framework_migrator;
GRANT EXECUTE ON FUNCTION control.publish_role(uuid,text,uuid,bigint,bytea,text,boolean),control.assign_role(uuid,uuid,text,text) TO framework_app;
GRANT EXECUTE ON FUNCTION control.worker_role_definition() TO framework_worker;
SELECT control.seed_tenant_roles(id) FROM iam.tenants;
ALTER TABLE iam.memberships ADD CONSTRAINT membership_tenant_role FOREIGN KEY(tenant_id,role) REFERENCES control.role_heads(tenant_id,name);
CREATE TRIGGER initial_tenant_roles AFTER INSERT ON iam.tenants FOR EACH ROW EXECUTE FUNCTION control.initial_tenant_roles();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain role histories and assignments; forward recovery only';END $$;
-- +goose StatementEnd
