package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

type UniqueConstraint struct {
	NumericIndex
	Generation int64 `json:"generation"`
}

// IntegrityReady is an advisory UI capability. Commands independently recheck
// the registry and database trigger; a true hint never authorizes a write.
func (Store) IntegrityReady(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access) (bool, error) {
	for _, entity := range s.Program().IR().Entities {
		for _, f := range entity.Fields {
			if !f.Unique {
				continue
			}
			x, err := uniqueConstraint(ctx, q, s, a, entity.ID, f.ID)
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if x.State != "READY" {
				return false, nil
			}
		}
	}
	return true, nil
}

func uniqueConstraint(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (UniqueConstraint, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return UniqueConstraint{}, err
	}
	f, ok := field(e, id)
	if !ok || f.CoreID {
		return UniqueConstraint{}, ErrInvalid
	}
	_, schema, err := compiler.Storage(s.Program())
	if err != nil {
		return UniqueConstraint{}, ErrInvalid
	}
	var x UniqueConstraint
	var typ, cur, target string
	var required bool
	var enums []byte
	err = q.QueryRowContext(ctx, `SELECT field_id,schema_hash,state,checkpoint,scanned_rows,verified_rows,generation,field_type,currency,required,enum_values,reference_to FROM runtime.unique_constraints WHERE `+recordWhere+` AND field_id=$6`, append(base(s, a, entity), id)...).Scan(&x.FieldID, &x.SchemaHash, &x.State, &x.Checkpoint, &x.ScannedRows, &x.VerifiedRows, &x.Generation, &typ, &cur, &required, &enums, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	if err != nil {
		return x, err
	}
	var choices []string
	if json.Unmarshal(enums, &choices) != nil || x.SchemaHash != schema || typ != f.Type || cur != f.Currency || required != f.Required || target != f.ReferenceTo || !slices.Equal(choices, f.EnumValues) {
		return UniqueConstraint{}, ErrUnavailable
	}
	return x, nil
}

func (Store) UniqueConstraint(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (UniqueConstraint, error) {
	return uniqueConstraint(ctx, q, s, a, entity, id)
}

func uniqueManagement(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (string, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return "", err
	}
	f, ok := field(e, id)
	if !ok || !f.Unique || f.CoreID {
		return "", ErrInvalid
	}
	_, schema, err := compiler.Storage(s.Program())
	if err != nil {
		return "", ErrInvalid
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return "", err
	}
	return schema, nil
}

func (Store) RegisterUniqueConstraint(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, expected int64) (UniqueConstraint, error) {
	schema, err := uniqueManagement(ctx, q, s, a, entity, id)
	if err != nil {
		return UniqueConstraint{}, err
	}
	if expected < 0 || expected >= 1e9 {
		return UniqueConstraint{}, ErrInvalid
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.register_unique_constraint($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, schema, s.Revision(), s.Hash(), expected)
	if err != nil {
		return UniqueConstraint{}, err
	}
	return uniqueConstraint(ctx, q, s, a, entity, id)
}

func uniqueStep(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, gen, checkpoint int64, abort bool) (UniqueConstraint, error) {
	schema, err := uniqueManagement(ctx, q, s, a, entity, id)
	if err != nil {
		return UniqueConstraint{}, err
	}
	if gen < 1 || gen > 1e9 || checkpoint < 1 || checkpoint > 1e12 {
		return UniqueConstraint{}, ErrInvalid
	}
	query := `SELECT runtime.build_unique_constraint($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	if abort {
		query = `SELECT runtime.abort_unique_constraint($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	}
	_, err = q.ExecContext(ctx, query, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, schema, s.Revision(), s.Hash(), gen, checkpoint)
	if err != nil {
		return UniqueConstraint{}, err
	}
	return uniqueConstraint(ctx, q, s, a, entity, id)
}
func (Store) BuildUniqueConstraint(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, gen, checkpoint int64) (UniqueConstraint, error) {
	return uniqueStep(ctx, q, s, a, entity, id, gen, checkpoint, false)
}
func (Store) AbortUniqueConstraint(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, gen, checkpoint int64) (UniqueConstraint, error) {
	return uniqueStep(ctx, q, s, a, entity, id, gen, checkpoint, true)
}

// Full-scope proof precedes all key lookups. Opaque policies cannot use conflict
// responses to infer hidden values, even for fresh values that would succeed.
func UniqueAuthority(ctx context.Context, engine policy.Engine, entity, id string, g Guard, resolve ...ScopeResolver) error {
	if !policy.SQLFieldActions(engine) {
		return ErrDenied
	}
	return uniqueAuthority(ctx, entity, id, g, resolve)
}
func uniqueAuthority(ctx context.Context, entity, id string, g Guard, resolve []ScopeResolver) error {
	for _, fid := range []string{"", id} {
		for _, action := range []string{"data.read", "data.write"} {
			sc, err := resolveScope(ctx, resolve, action, Resource{EntityID: entity, FieldID: fid})
			if err != nil {
				return err
			}
			if !scopeAll(sc) {
				return ErrDenied
			}
			kind := "entity"
			if fid != "" {
				kind = "field"
			}
			if err = g.check(ctx, "data."+kind+"."+action[5:], Resource{EntityID: entity, FieldID: fid}); err != nil {
				return err
			}
		}
	}
	return nil
}

type uniquePolicyKey struct{}

func (st Store) CommandWithPolicy(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, c Command, engine policy.Engine, g Guard, resolve ...ScopeResolver) (Record, error) {
	ctx = context.WithValue(ctx, uniquePolicyKey{}, policy.SQLFieldActions(engine))
	return st.Command(ctx, q, s, a, entity, c, g, resolve...)
}

func authorizeUniqueMutation(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e compiler.Entity, id string, values map[string]any, g Guard, resolve []ScopeResolver) error {
	rows, err := q.QueryContext(ctx, `SELECT field_id FROM runtime.unique_constraints WHERE `+recordWhere+` ORDER BY field_id`, base(s, a, e.ID)...)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, f := range e.Fields {
		if f.Unique && !slices.Contains(ids, f.ID) {
			return ErrBinding
		}
	}
	claims := map[string]any{}
	for _, fid := range ids {
		idx, err := uniqueConstraint(ctx, q, s, a, e.ID, fid)
		if err != nil {
			return err
		}
		f, _ := field(e, fid)
		if idx.State == "ABORTED" && !f.Unique {
			continue
		}
		if idx.State != "READY" {
			return ErrBinding
		}
		value, touched := values[fid]
		if !touched || value == nil {
			continue
		}
		proof, _ := ctx.Value(uniquePolicyKey{}).(bool)
		if !proof {
			return ErrDenied
		}
		if err = uniqueAuthority(ctx, e.ID, fid, g, resolve); err != nil {
			return err
		}
		claims[fid] = value
	}
	if len(claims) == 0 {
		return nil
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.authorize_unique_write($1,$2,$3,$4,$5,$6,$7::jsonb)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), e.ID, id, string(raw))
	return err
}
func clearUniqueMutation(ctx context.Context, q postgres.Executor) error {
	_, err := q.ExecContext(ctx, `SELECT runtime.clear_unique_write()`)
	return err
}

func uniqueReady(ctx context.Context, db *sql.DB) error {
	var valid bool
	err := db.QueryRowContext(ctx, `SELECT count(*)=4 AND bool_and(relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole AND NOT has_table_privilege(oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') AND NOT has_any_column_privilege(oid,'INSERT,UPDATE,REFERENCES') AND (relname NOT IN('unique_claims','unique_write_context') OR NOT has_table_privilege(oid,'SELECT') AND NOT has_any_column_privilege(oid,'SELECT'))) FROM pg_class WHERE oid IN('runtime.unique_constraints'::regclass,'runtime.unique_claims'::regclass,'runtime.unique_constraint_history'::regclass,'control.unique_write_context'::regclass)`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_constraint c JOIN pg_index i ON i.indexrelid=c.conindid WHERE c.conrelid='runtime.unique_claims'::regclass AND c.conname='unique_claim_value' AND c.contype='u' AND NOT c.condeferrable AND c.convalidated AND c.conkey=ARRAY[1,2,3,4,5,6,7,10]::smallint[] AND i.indisunique AND i.indisvalid AND i.indisready)
 AND (SELECT count(*)=5 AND bool_and(tgenabled='O') FROM pg_trigger WHERE (tgrelid,tgname) IN(('runtime.records'::regclass,'record_unique_claims'),('runtime.unique_constraints'::regclass,'retain_unique_constraints'),('runtime.unique_claims'::regclass,'retain_unique_claims'),('runtime.unique_constraint_history'::regclass,'immutable_unique_history'),('runtime.dataset_evolutions'::regclass,'dataset_unique_evolution')))
 AND (SELECT count(*)=3 AND bool_and(NOT polpermissive AND polroles=ARRAY['framework_worker'::regrole::oid] AND pg_get_expr(polqual,polrelid)='false' AND pg_get_expr(polwithcheck,polrelid)='false') FROM pg_policy WHERE polrelid IN('runtime.unique_constraints'::regclass,'runtime.unique_claims'::regclass,'runtime.unique_constraint_history'::regclass) AND polname='worker_no_generic')`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT count(*)=9 AND bool_and(p.proowner='framework_schema_owner'::regrole AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
 AND p.provolatile=CASE WHEN p.proname='unique_key' THEN 'i'::"char" ELSE 'v'::"char" END
 AND p.prosecdef=(p.proname<>'unique_key')
 AND (p.proname<>'unique_key' OR p.proconfig @> ARRAY['TimeZone=UTC','DateStyle=ISO, YMD'])
 AND has_function_privilege(p.oid,'EXECUTE')=(p.proname IN('authorize_unique_write','clear_unique_write','register_unique_constraint','build_unique_constraint','abort_unique_constraint'))
 AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE x.grantee=0 AND x.privilege_type='EXECUTE')) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='runtime' AND p.proname IN('unique_key','unique_authorized','authorize_unique_write','clear_unique_write','register_unique_constraint','build_unique_constraint','abort_unique_constraint','maintain_unique_claims','fence_unique_evolution')`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	return nil
}
