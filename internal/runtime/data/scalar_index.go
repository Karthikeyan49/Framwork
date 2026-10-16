package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// FieldIndex preserves the existing numeric management response contract.
// It exposes checkpoint evidence, never indexed business values or overflow.
type FieldIndex = NumericIndex

func scalarType(typ string) bool {
	return typ == "string" || typ == "date" || typ == "enum" || typ == "reference" || typ == "datetime" || typ == "bool"
}

func scalarField(s Scope, a tenant.Access, entity, id string) (compiler.Field, string, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return compiler.Field{}, "", err
	}
	f, ok := field(e, id)
	if !ok || f.CoreID || !scalarType(f.Type) {
		return compiler.Field{}, "", ErrInvalid
	}
	_, schema, err := compiler.Storage(s.Program())
	if err != nil {
		return compiler.Field{}, "", ErrInvalid
	}
	return f, schema, nil
}

func scalarIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (FieldIndex, error) {
	f, schema, err := scalarField(s, a, entity, id)
	if err != nil {
		return FieldIndex{}, err
	}
	var index FieldIndex
	var typ string
	var choices []byte
	var target string
	var required, collation bool
	args := append(base(s, a, entity), id)
	err = q.QueryRowContext(ctx, `SELECT field_id,schema_hash,state,checkpoint,scanned_rows,verified_rows,field_type,required,
 CASE WHEN field_type IN('string','enum','reference') THEN runtime.scalar_collation_safe(collation_version) ELSE collation_version IS NULL END,enum_values,reference_to
 FROM runtime.scalar_indexes WHERE `+recordWhere+` AND field_id=$6`, args...).Scan(&index.FieldID, &index.SchemaHash, &index.State, &index.Checkpoint, &index.ScannedRows, &index.VerifiedRows, &typ, &required, &collation, &choices, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return FieldIndex{}, ErrNotFound
	}
	if err != nil {
		return FieldIndex{}, err
	}
	var values []string
	if json.Unmarshal(choices, &values) != nil || values == nil || !slices.Equal(values, f.EnumValues) || target != f.ReferenceTo || index.SchemaHash != schema || typ != f.Type || required != f.Required || !collation {
		return FieldIndex{}, ErrUnavailable
	}
	return index, nil
}

func scalarSortUsable(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, f compiler.Field) (bool, error) {
	// A pool defaulting to RR still creates distinct snapshots. Only the exact
	// transaction port proves that the overflow probe and page share a snapshot.
	if !postgres.RuntimeSnapshot(q) {
		return false, nil
	}
	var usable bool
	args := append(base(s, a, entity), f.ID)
	err := q.QueryRowContext(ctx, `SELECT current_setting('transaction_isolation') IN ('repeatable read','serializable')
 AND NOT EXISTS(SELECT 1 FROM runtime.scalar_projections WHERE `+recordWhere+` AND field_id=$6 AND overflow)`, args...).Scan(&usable)
	return usable, err
}

func scalarKind(s Scope, a tenant.Access, entity, id string) bool {
	_, _, err := scalarField(s, a, entity, id)
	return err == nil
}

func (Store) FieldIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (FieldIndex, error) {
	if scalarKind(s, a, entity, id) {
		return scalarIndex(ctx, q, s, a, entity, id)
	}
	return numericIndex(ctx, q, s, a, entity, id)
}

func (Store) RegisterFieldIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (FieldIndex, error) {
	if !scalarKind(s, a, entity, id) {
		return (Store{}).RegisterNumericIndex(ctx, q, s, a, entity, id)
	}
	_, schema, err := scalarField(s, a, entity, id)
	if err != nil {
		return FieldIndex{}, err
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return FieldIndex{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.register_scalar_index($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, schema, s.Revision(), s.Hash())
	if err != nil {
		return FieldIndex{}, err
	}
	return scalarIndex(ctx, q, s, a, entity, id)
}

func (Store) BuildFieldIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, expected int64) (FieldIndex, error) {
	if !scalarKind(s, a, entity, id) {
		return (Store{}).BuildNumericIndex(ctx, q, s, a, entity, id, expected)
	}
	_, schema, err := scalarField(s, a, entity, id)
	if err != nil || expected < 1 || expected > 1e12 {
		return FieldIndex{}, ErrInvalid
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return FieldIndex{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.build_scalar_index($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, schema, s.Revision(), s.Hash(), expected)
	if err != nil {
		return FieldIndex{}, err
	}
	return scalarIndex(ctx, q, s, a, entity, id)
}

func scalarReady(ctx context.Context, db *sql.DB) error {
	var valid bool
	err := db.QueryRowContext(ctx, `SELECT count(*)=2 AND bool_and(relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole
 AND NOT has_table_privilege(oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') AND NOT has_any_column_privilege(oid,'INSERT,UPDATE,REFERENCES'))
 FROM pg_class WHERE oid IN('runtime.scalar_indexes'::regclass,'runtime.scalar_projections'::regclass)`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT (SELECT count(*)=3 AND bool_and(tgenabled='O') FROM pg_trigger WHERE
 (tgrelid,tgname) IN(('runtime.records'::regclass,'record_scalar_projection'),('runtime.scalar_indexes'::regclass,'retain_scalar_indexes'),('runtime.scalar_projections'::regclass,'retain_scalar_projections')))
 AND (SELECT count(*)=13 AND bool_and(indisvalid AND indisready) FROM pg_index WHERE indrelid='runtime.scalar_projections'::regclass AND indexrelid IN('runtime.scalar_text_asc'::regclass,'runtime.scalar_text_desc'::regclass,'runtime.scalar_date_asc'::regclass,'runtime.scalar_date_desc'::regclass,'runtime.scalar_text_overflow'::regclass,'runtime.scalar_enum_asc'::regclass,'runtime.scalar_enum_desc'::regclass,'runtime.scalar_reference_asc'::regclass,'runtime.scalar_reference_desc'::regclass,'runtime.scalar_datetime_asc'::regclass,'runtime.scalar_datetime_desc'::regclass,'runtime.scalar_bool_asc'::regclass,'runtime.scalar_bool_desc'::regclass))
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='runtime.scalar_indexes'::regclass AND conname='scalar_indexes_custom_field' AND contype='c' AND convalidated)
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='runtime.scalar_indexes'::regclass AND conname='scalar_indexes_options' AND contype='c' AND convalidated)
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='runtime.scalar_projections'::regclass AND conname='scalar_projections_check' AND contype='c' AND convalidated)
 AND (SELECT datcollversion IS NOT DISTINCT FROM pg_database_collation_actual_version(oid) FROM pg_database WHERE datname=current_database())`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT count(*)=9 AND bool_and(p.proowner='framework_schema_owner'::regrole AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
 AND (p.proname<>'scalar_native_value' OR (p.provolatile='i' AND p.proconfig @> ARRAY['TimeZone=UTC','DateStyle=ISO, YMD']))
 AND p.prosecdef=(p.proname IN('register_scalar_index','build_scalar_index','maintain_scalar_projection'))
 AND has_function_privilege(p.oid,'EXECUTE')=(p.proname IN('scalar_collation_safe','register_scalar_index','build_scalar_index'))
 AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE x.grantee=0 AND x.privilege_type='EXECUTE'))
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='runtime' AND p.proname IN('scalar_collation_safe','scalar_value','check_index_budget','register_scalar_index','maintain_scalar_projection','build_scalar_index','scalar_options_safe','scalar_project_value','scalar_native_value')`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	_, err = db.ExecContext(ctx, `SELECT i.schema_hash,i.collation_version,i.enum_values,i.reference_to,i.state,i.checkpoint,i.verified_rows,p.field_type,p.value_text,p.value_date,p.value_instant,p.value_boolean,p.overflow,p.source_version FROM runtime.scalar_indexes i JOIN runtime.scalar_projections p USING(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) LIMIT 0`)
	return err
}
