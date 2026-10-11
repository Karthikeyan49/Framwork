-- Purpose: authenticated transaction context and reusable tenant-table RLS.
-- Compatibility: tighter IAM policies; deploy before the M2 API.
-- Validation: normal-role CRUD, spoofing, pool reuse, cancellation and recovery tests.
-- Recovery: atomic migration rollback on failure; forward fixes after deployment.
-- +goose Up
-- These bounded read-only lookup functions avoid recursive IAM RLS. Their owner
-- uses the existing explicit operator policies, not superuser or BYPASSRLS.
-- +goose StatementBegin
CREATE FUNCTION iam.authenticated_actor() RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
    SELECT u.id FROM iam.users u WHERE u.active
      AND u.id::text=current_setting('app.actor_id',true)
      AND u.issuer=current_setting('app.auth_issuer',true)
      AND u.subject=current_setting('app.auth_subject',true)
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION iam.current_tenant_id() RETURNS uuid
LANGUAGE sql STABLE SET search_path = pg_catalog AS $$
    SELECT CASE WHEN current_setting('app.tenant_id',true)
      ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
      THEN current_setting('app.tenant_id',true)::uuid ELSE NULL END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION iam.tenant_matches_scope(candidate uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
    SELECT COALESCE(candidate=iam.current_tenant_id(),false) AND EXISTS (
      SELECT 1 FROM iam.memberships m JOIN iam.tenants t ON t.id=m.tenant_id
      WHERE m.tenant_id=candidate AND m.user_id=iam.authenticated_actor()
        AND m.active AND t.active)
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION iam.authenticated_actor(), iam.current_tenant_id(), iam.tenant_matches_scope(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION iam.authenticated_actor(), iam.current_tenant_id(), iam.tenant_matches_scope(uuid) TO framework_app;

DROP POLICY authenticated_membership ON iam.memberships;
CREATE POLICY authenticated_membership ON iam.memberships FOR SELECT TO framework_app USING (
    user_id=iam.authenticated_actor()
    OR (tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id))
);
DROP POLICY authenticated_tenant ON iam.tenants;
CREATE POLICY authenticated_tenant ON iam.tenants FOR SELECT TO framework_app USING (
    active AND EXISTS (SELECT 1 FROM iam.memberships m WHERE m.tenant_id=id
      AND m.user_id=iam.authenticated_actor() AND m.active)
);
DROP POLICY authenticated_audit ON audit.entries;
CREATE POLICY authenticated_audit ON audit.entries FOR INSERT TO framework_app WITH CHECK (
    (actor_id=iam.authenticated_actor()
      AND (tenant_id IS NULL OR iam.tenant_matches_scope(tenant_id)))
    OR (actor_id IS NULL AND tenant_id IS NULL AND action='authentication.denied' AND NOT allowed)
);
GRANT USAGE ON SCHEMA runtime,control TO framework_app;

-- Migration-only installer: never executable by the normal application role.
-- No production business table is created in M2. Future DDL must call this in
-- its own migration transaction before the table becomes available to runtime.
-- +goose StatementBegin
CREATE FUNCTION runtime.install_tenant_rls(target regclass) RETURNS void
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    schema_name name;
    table_name name;
    tenant_column smallint;
BEGIN
    SELECT n.nspname,c.relname INTO schema_name,table_name
      FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
      WHERE c.oid=target AND n.nspname IN ('runtime','control')
        AND c.relkind IN ('r','p') AND NOT c.relispartition
        AND c.relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user);
    IF schema_name IS NULL OR current_user<>'framework_migrator' THEN
        RAISE EXCEPTION 'tenant RLS requires a migration-owned tenant table';
    END IF;
    SELECT attnum INTO tenant_column FROM pg_attribute
      WHERE attrelid=target AND attname='tenant_id' AND atttypid='uuid'::regtype
        AND attnotnull AND NOT attisdropped;
    IF tenant_column IS NULL OR NOT EXISTS (
      SELECT 1 FROM pg_index WHERE indrelid=target AND indisprimary)
      OR EXISTS (SELECT 1 FROM pg_index WHERE indrelid=target AND indisunique
        AND NOT tenant_column=ANY((indkey::smallint[])[0:indnkeyatts-1]))
      OR EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid=target AND contype='x') THEN
        RAISE EXCEPTION 'tenant table needs nonnull UUID scope and tenant-inclusive primary/unique keys';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint fk WHERE fk.conrelid=target AND fk.contype='f'
      AND NOT EXISTS (SELECT 1 FROM pg_attribute ref
        WHERE ref.attrelid=fk.confrelid AND ref.attname='tenant_id' AND NOT ref.attisdropped
          AND EXISTS (SELECT 1 FROM unnest(fk.conkey,fk.confkey) pair(local_col,remote_col)
            WHERE pair.local_col=tenant_column AND pair.remote_col=ref.attnum))) THEN
        RAISE EXCEPTION 'tenant foreign keys must include matching tenant scope';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_policy WHERE polrelid=target) THEN
        RAISE EXCEPTION 'existing policies require explicit security review';
    END IF;
    EXECUTE format('REVOKE ALL ON TABLE %I.%I FROM PUBLIC,framework_app',schema_name,table_name);
    EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY',schema_name,table_name);
    EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY',schema_name,table_name);
    EXECUTE format('CREATE POLICY tenant_scope ON %I.%I TO framework_app USING
      (tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id)) WITH CHECK
      (tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id))',schema_name,table_name);
    EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON TABLE %I.%I TO framework_app',schema_name,table_name);
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION runtime.install_tenant_rls(regclass) FROM PUBLIC,framework_app;

-- +goose Down
-- Forward fixes only: intentionally refuse to restore the weaker M1 policies.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'tenant security migrations require a forward fix'; END $$;
-- +goose StatementEnd
