package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Search is plain AND-of-words search with a fixed PostgreSQL configuration.
// Text is query input, never SQL, tsquery syntax or authorization context.
type Search struct {
	Field string `json:"field"`
	Text  string `json:"text"`
}

func searchField(s Scope, a tenant.Access, entity, id string) (compiler.Field, string, error) {
	f, schema, err := scalarField(s, a, entity, id)
	if err != nil || f.Type != "string" {
		return compiler.Field{}, "", ErrInvalid
	}
	return f, schema, nil
}
func searchIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (FieldIndex, error) {
	f, schema, err := searchField(s, a, entity, id)
	if err != nil {
		return FieldIndex{}, err
	}
	var idx FieldIndex
	var typ string
	var req, safe bool
	args := append(base(s, a, entity), id)
	err = q.QueryRowContext(ctx, `SELECT field_id,schema_hash,state,checkpoint,scanned_rows,verified_rows,field_type,required,parser_hash=runtime.search_parser_hash() FROM runtime.search_indexes WHERE `+recordWhere+` AND field_id=$6`, args...).Scan(&idx.FieldID, &idx.SchemaHash, &idx.State, &idx.Checkpoint, &idx.ScannedRows, &idx.VerifiedRows, &typ, &req, &safe)
	if errors.Is(err, sql.ErrNoRows) {
		return FieldIndex{}, ErrNotFound
	}
	if err != nil {
		return FieldIndex{}, err
	}
	if idx.SchemaHash != schema || typ != f.Type || req != f.Required || !safe {
		return FieldIndex{}, ErrUnavailable
	}
	return idx, nil
}
func (Store) SearchIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e, id string) (FieldIndex, error) {
	return searchIndex(ctx, q, s, a, e, id)
}
func (Store) RegisterSearchIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e, id string) (FieldIndex, error) {
	_, schema, err := searchField(s, a, e, id)
	if err != nil {
		return FieldIndex{}, err
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return FieldIndex{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.register_search_index($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), e, id, schema, s.Revision(), s.Hash())
	if err != nil {
		return FieldIndex{}, err
	}
	return searchIndex(ctx, q, s, a, e, id)
}
func (Store) BuildSearchIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e, id string, expected int64) (FieldIndex, error) {
	_, schema, err := searchField(s, a, e, id)
	if err != nil || expected < 1 || expected > 1e12 {
		return FieldIndex{}, ErrInvalid
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return FieldIndex{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.build_search_index($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), e, id, schema, s.Revision(), s.Hash(), expected)
	if err != nil {
		return FieldIndex{}, err
	}
	return searchIndex(ctx, q, s, a, e, id)
}
func validSearch(v Search) bool {
	return utf8.ValidString(v.Text) && len(v.Text) <= 256 && strings.TrimSpace(v.Text) != "" && !strings.ContainsRune(v.Text, 0)
}
func searchReady(ctx context.Context, db *sql.DB) error {
	var valid bool
	err := db.QueryRowContext(ctx, `SELECT count(*)=2 AND bool_and(relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole AND NOT has_table_privilege(oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') AND NOT has_any_column_privilege(oid,'INSERT,UPDATE,REFERENCES')) FROM pg_class WHERE oid IN('runtime.search_indexes'::regclass,'runtime.search_projections'::regclass)`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_index WHERE indexrelid='runtime.search_terms'::regclass AND indisvalid AND indisready)
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='runtime.search_indexes'::regclass AND conname='search_indexes_check' AND convalidated)
 AND (SELECT count(*)=3 AND bool_and(tgenabled='O') FROM pg_trigger WHERE (tgrelid,tgname) IN(('runtime.records'::regclass,'record_search_projection'),('runtime.search_indexes'::regclass,'retain_search_indexes'),('runtime.search_projections'::regclass,'retain_search_projections')))
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid='runtime.search_projections'::regclass AND polname='search_match' AND NOT polpermissive AND polroles=ARRAY['framework_app'::regrole::oid] AND polcmd='*' AND polwithcheck IS NULL AND regexp_replace(pg_get_expr(polqual,polrelid),'[[:space:]]','','g')='(value_search@@(SELECTruntime.search_context()ASsearch_context))')
 AND runtime.search_parser_hash() IS NOT NULL`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT count(*)=6 AND bool_and(p.proowner='framework_schema_owner'::regrole AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
 AND p.provolatile=CASE WHEN p.proname='search_value' THEN 'i'::"char" WHEN p.proname IN('search_context','search_parser_hash') THEN 's'::"char" ELSE 'v'::"char" END
 AND p.prosecdef=(p.proname IN('register_search_index','build_search_index','maintain_search_projection'))
 AND has_function_privilege(p.oid,'EXECUTE')=(p.proname IN('search_context','search_parser_hash','register_search_index','build_search_index'))
 AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE x.grantee=0 AND x.privilege_type='EXECUTE')) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='runtime' AND p.proname IN('search_context','search_parser_hash','search_value','register_search_index','maintain_search_projection','build_search_index')`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	return nil
}

type searchPlan struct{ field, join, from, predicate string }

func (st Store) prepareSearch(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, search Search, guard Guard, b *builder) (p searchPlan, err error) {
	f, _, er := searchField(s, a, entity, search.Field)
	if er != nil {
		return p, er
	}
	if er = guard.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: f.ID}); er != nil {
		return p, er
	}
	p.field = f.ID
	var nodes int
	var parserSafe bool
	if er = q.QueryRowContext(ctx, `SELECT numnode(plainto_tsquery('pg_catalog.simple'::regconfig,$1)),runtime.search_parser_hash() IS NOT NULL`, search.Text).Scan(&nodes, &parserSafe); er != nil {
		return p, er
	}
	if !parserSafe {
		return p, ErrUnavailable
	}
	if nodes < 1 || nodes > 31 {
		return p, ErrInvalid
	}
	useIndex := false
	if !st.legacy {
		index, indexErr := searchIndex(ctx, q, s, a, entity, f.ID)
		if indexErr != nil && indexErr != ErrNotFound {
			return p, indexErr
		}
		useIndex = indexErr == nil && index.State == "READY" && postgres.RuntimeSnapshot(q)
	}
	text := b.bind(search.Text)
	if useIndex {
		if _, er = q.ExecContext(ctx, `SELECT set_config('app.search_text',$1,true)`, search.Text); er != nil {
			return p, er
		}
		predicate := `sp.tenant_id=$1 AND sp.app_id=$2 AND sp.metadata_revision=$3 AND sp.ir_hash=$4 AND sp.entity_id=$5 AND sp.field_id=` + b.bind(f.ID) + ` AND sp.value_search @@ plainto_tsquery('pg_catalog.simple'::regconfig,` + text + `)`
		p.join = ` JOIN runtime.search_projections sp ON ` + predicate + ` AND sp.id=r.id AND sp.source_version=r.row_version`
		p.from = `(SELECT sp.id,sp.source_version FROM runtime.search_projections sp WHERE ` + predicate + ` OFFSET 0) sp JOIN LATERAL (SELECT r.* FROM ` + st.recordsTable() + ` r WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND r.entity_id=$5 AND r.id=sp.id AND r.row_version=sp.source_version OFFSET 0) r ON true`
	} else {
		p.predicate = ` AND to_tsvector('pg_catalog.simple'::regconfig,coalesce(r.custom_data->>` + b.bind(f.ID) + `,'')) @@ plainto_tsquery('pg_catalog.simple'::regconfig,` + text + `)`
	}
	return p, nil
}
