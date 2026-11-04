package data

import (
	"context"
	"database/sql"
)

func aggregateReady(ctx context.Context, db *sql.DB) error {
	var valid bool
	err := db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='runtime.numeric_projections'::regclass AND attname='aggregate_valid_v1' AND atttypid='boolean'::regtype AND NOT attnotnull AND NOT attisdropped)
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='runtime.numeric_projections'::regclass AND tgname='certify_aggregate_projection_v1' AND tgfoid='runtime.certify_aggregate_projection_v1()'::regprocedure AND tgenabled='O' AND tgtype=23)
 AND (SELECT count(*)=2 AND bool_and(indisvalid AND indisready) FROM pg_index WHERE (indexrelid,indrelid) IN(('runtime.records_aggregate_headers'::regclass,'runtime.records'::regclass),('runtime.numeric_aggregate_values'::regclass,'runtime.numeric_projections'::regclass)))
 AND EXISTS(SELECT 1 FROM pg_class WHERE oid='runtime.numeric_projections'::regclass AND relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole AND NOT has_table_privilege(oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') AND NOT has_any_column_privilege(oid,'INSERT,UPDATE,REFERENCES'))
 AND (SELECT count(*)=2 AND bool_and(p.proowner='framework_schema_owner'::regrole AND NOT owner.rolcanlogin
 AND p.proconfig @> CASE WHEN p.proname='numeric_value' THEN ARRAY['search_path=pg_catalog'] ELSE ARRAY['search_path=pg_catalog, pg_temp'] END
 AND p.prosecdef=(p.proname='certify_aggregate_projection_v1') AND p.provolatile=CASE WHEN p.proname='numeric_value' THEN 'i'::"char" ELSE 'v'::"char" END
 AND NOT has_function_privilege('framework_app',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE x.grantee=0 AND x.privilege_type='EXECUTE'))
 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner WHERE p.oid IN('runtime.numeric_value(jsonb,text,text,boolean)'::regprocedure,'runtime.certify_aggregate_projection_v1()'::regprocedure))`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	return nil
}
