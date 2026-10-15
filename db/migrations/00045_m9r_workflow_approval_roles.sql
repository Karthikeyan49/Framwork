-- R5e: opt-in metadata/IR1.3 and a live, revocation-fenced approval role port.
-- Forward-only. No artifact/record rewrite, IAM DML grant or worker authority.
-- +goose Up
ALTER TABLE control.metadata_versions DROP CONSTRAINT metadata_versions_schema_version_check;
ALTER TABLE control.metadata_versions ADD CONSTRAINT metadata_versions_schema_version_check
 CHECK(schema_version IN('1.0.0','1.1.0','1.2.0','1.3.0'));
-- +goose StatementBegin
CREATE FUNCTION control.lock_workflow_approver(t uuid,expected_actor uuid,expected_role text) RETURNS boolean
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE actor uuid;
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id()
 OR expected_role IS NULL OR expected_role !~ '^[a-z][a-z0-9_]{0,63}$'
 OR iam.tenant_matches_scope(t) IS NOT TRUE THEN RETURN false;END IF;
 actor:=iam.authenticated_actor();
 IF actor IS NULL OR actor IS DISTINCT FROM expected_actor THEN RETURN false;END IF;
 -- Same tenant fence/order as role publication/assignment. Unlike a plain
 -- snapshot lookup these locks abort stale REPEATABLE READ approvals and hold
 -- admitted authority stable until the signal/history/audit transaction ends.
 PERFORM 1 FROM iam.tenants x WHERE x.id=t AND x.active FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM iam.users x WHERE x.id=actor AND x.active FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=actor AND m.active AND m.role=expected_role FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM control.role_heads h WHERE h.tenant_id=t AND h.name=expected_role AND h.active FOR SHARE;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION control.lock_workflow_approver(uuid,uuid,text) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.lock_workflow_approver(uuid,uuid,text) FROM PUBLIC,framework_worker,framework_dispatcher;
GRANT EXECUTE ON FUNCTION control.lock_workflow_approver(uuid,uuid,text) TO framework_app;
