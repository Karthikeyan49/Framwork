package policy

import (
	"context"
	"database/sql"
)

// Ready verifies authority and grant boundaries without reading tenant data.
func (RoleStore) Ready(ctx context.Context, db *sql.DB, worker bool) error {
	if db == nil {
		return ErrScope
	}
	var valid bool
	err := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=2 AND bool_and(relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole) FROM pg_class WHERE oid IN('control.role_versions'::regclass,'control.role_heads'::regclass))
 AND (SELECT count(*)=3 AND bool_and(tgenabled='O') FROM pg_trigger WHERE (tgrelid,tgname) IN(('control.role_versions'::regclass,'immutable_role_version'),('control.role_heads'::regclass,'retain_role_head'),('control.role_heads'::regclass,'role_head_guard')))
 AND (SELECT count(*)=7 AND bool_and(prosecdef AND proowner='framework_schema_owner'::regrole AND proconfig @> ARRAY['search_path=pg_catalog, pg_temp']) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('seed_tenant_roles','initial_tenant_roles','role_manager','publish_role','assign_role','worker_role_definition','role_head_guard'))
 AND NOT EXISTS(SELECT 1 FROM pg_class WHERE oid IN('control.role_versions'::regclass,'control.role_heads'::regclass) AND (has_table_privilege(oid,'INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(oid,'INSERT,UPDATE')))
 AND NOT has_any_column_privilege('iam.roles','SELECT')
 AND NOT has_function_privilege('control.seed_tenant_roles(uuid)','EXECUTE')
 AND NOT has_function_privilege('control.role_manager(uuid)','EXECUTE')`).Scan(&valid)
	if err != nil || !valid {
		return ErrScope
	}
	if worker {
		err = db.QueryRowContext(ctx, `SELECT control.worker_principal() AND control.worker_grants_safe() AND has_function_privilege('control.worker_role_definition()','EXECUTE') AND NOT has_function_privilege('control.publish_role(uuid,text,uuid,bigint,bytea,text,boolean)','EXECUTE') AND NOT has_function_privilege('control.assign_role(uuid,uuid,text,text)','EXECUTE')`).Scan(&valid)
	} else {
		err = db.QueryRowContext(ctx, `SELECT session_user='framework_app' AND has_table_privilege('control.role_heads','SELECT') AND has_table_privilege('control.role_versions','SELECT') AND has_function_privilege('control.publish_role(uuid,text,uuid,bigint,bytea,text,boolean)','EXECUTE') AND has_function_privilege('control.assign_role(uuid,uuid,text,text)','EXECUTE') AND NOT has_function_privilege('control.worker_role_definition()','EXECUTE')`).Scan(&valid)
	}
	if err != nil || !valid {
		return ErrScope
	}
	return nil
}
