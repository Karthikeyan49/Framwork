-- Independent review: exact entity shares, protected catalog lookup and defense
-- against accidentally restored generic grants. Preserve prior migrations/data.
-- +goose Up
ALTER TABLE control.worker_principals ADD CONSTRAINT worker_principal_name
 CHECK(role_name::text ~ '^[a-z][a-z0-9_]{0,62}$' AND role_name::text NOT IN('worker','dispatcher','operator','framework_app','framework_dispatcher','framework_migrator','framework_schema_owner','framework_maintenance','framework_audit_writer'));
ALTER POLICY worker_share_boundary ON runtime.record_shares
 USING(EXISTS(SELECT 1 FROM control.worker_authority() a JOIN runtime.workflow_bindings b ON b.tenant_id=a.tenant_id AND b.id=a.job_id
 WHERE a.kind='workflow' AND b.tenant_id=record_shares.tenant_id AND b.app_id=record_shares.app_id
 AND b.metadata_revision=record_shares.metadata_revision AND b.ir_hash=record_shares.ir_hash
 AND record_shares.entity_id=(convert_from(b.plan_body,'UTF8')::jsonb->>'entity_id')
 AND b.record_id=record_shares.id AND record_shares.user_id=a.actor_id));
-- Retain tenant-scoped team metadata: descendant-team resolution needs the
-- hierarchy even when the actor is a direct member of only its parent team.

-- +goose StatementBegin
DO $$ DECLARE tab text;BEGIN
 FOREACH tab IN ARRAY ARRAY['runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals'] LOOP
 EXECUTE format('CREATE POLICY worker_no_generic ON %s AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false)',tab);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname||'.'||c.relname IN('runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals')
 AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
ALTER FUNCTION control.worker_grants_safe() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.worker_grants_safe() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.worker_grants_safe() TO framework_worker;

-- Put implicit temporary relation/type lookup explicitly last. This also
-- hardens retained SECURITY DEFINER IAM/control functions, without changing their
-- behavior, owner, grants, signatures or stored artifacts.
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
 WHERE p.prosecdef AND n.nspname IN('iam','runtime','control','audit') LOOP
 EXECUTE format('ALTER FUNCTION %s SET search_path=pg_catalog,pg_temp',f);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'preserve reviewed worker boundaries; forward-fix only';END $$;
-- +goose StatementEnd
