-- R3a: retain FORCE RLS, live membership and all data/grants. Uncorrelated
-- InitPlans avoid checking membership once per scanned row. No context cache.
-- +goose Up
-- +goose StatementBegin
DO $$ DECLARE t regclass; BEGIN
 FOR t IN SELECT c.oid::regclass FROM pg_catalog.pg_policy p JOIN pg_catalog.pg_class c ON c.oid=p.polrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname IN('runtime','control') AND p.polname='tenant_scope' LOOP
  EXECUTE format('ALTER POLICY tenant_scope ON %s TO framework_app USING(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id()))) WITH CHECK(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id())))',t);
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER POLICY team_scope ON iam.teams USING(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id())));
ALTER POLICY team_member_scope ON iam.team_memberships USING(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id())) AND user_id=iam.authenticated_actor());
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.install_tenant_rls(target regclass) RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
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
 EXECUTE format('CREATE POLICY tenant_scope ON %I.%I TO framework_app USING(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id()))) WITH CHECK(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id())))',schema_name,table_name);
 EXECUTE format('CREATE POLICY schema_owner_access ON %I.%I TO framework_schema_owner USING(true) WITH CHECK(true)',schema_name,table_name);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I.%I TO framework_app',schema_name,table_name);
 EXECUTE format('ALTER TABLE %I.%I OWNER TO framework_schema_owner',schema_name,table_name);
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION runtime.install_tenant_rls(regclass) FROM PUBLIC,framework_app,framework_worker,framework_dispatcher;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'preserve tenant isolation; reviewed forward recovery only';END $$;
-- +goose StatementEnd
