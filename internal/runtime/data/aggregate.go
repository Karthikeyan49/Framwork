package data

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

const SQLAggregateV1 = "SQL_V1"
const MaxSQLAggregateRows = 100000

// Expanded aggregation is an explicit interactive contract. Strict execution
// inputs, workers, opaque policies and legacy requests keep their existing path.
func (st Store) aggregateSQL(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e compiler.Entity, query Query, g Guard, resolve []ScopeResolver) (page Page, resultErr error) {
	if !st.visible || !st.sqlFields || st.legacy || st.workerSource || !postgres.RuntimeSnapshot(q) {
		return Page{}, ErrDenied
	}
	if err := g.check(ctx, "data.aggregate.read", Resource{EntityID: e.ID}); err != nil {
		return Page{}, err
	}
	f, err := aggregateShape(e, query)
	if err != nil {
		return Page{}, err
	}
	entityScope, err := resolveScope(ctx, resolve, "data.read", Resource{EntityID: e.ID})
	if err != nil {
		return Page{}, err
	}
	predicateGuard := Guard(func(ctx context.Context, action string, r Resource) error {
		if err := g.check(ctx, action, r); err != nil {
			return err
		}
		fs, err := resolveScope(ctx, resolve, "data.read", r)
		if err != nil {
			return err
		}
		if !coversEntity(fs, entityScope) {
			return ErrDenied
		}
		return nil
	})
	// Authorize every value-dependent predicate before validating its value or
	// examining data. Projection-only fields never shrink an entity-visible count.
	ids, err := predicateFields(s, e, query)
	if err != nil {
		return Page{}, err
	}
	for _, id := range ids {
		if err = predicateGuard.check(ctx, "data.field.read", Resource{EntityID: e.ID, FieldID: id}); err != nil {
			return Page{}, err
		}
	}
	b := builder{args: base(s, a, e.ID)}
	nodes := 0
	pred, err := scopeSQL(ctx, e, entityScope, a, "r", &b, 0, &nodes)
	if err != nil {
		return Page{}, err
	}
	where := `r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND r.entity_id=$5 AND (` + pred + `)`
	if query.Filter != nil {
		nodes = 0
		filter, err := filterSQL(ctx, e, *query.Filter, &b, predicateGuard, 0, &nodes)
		if err != nil {
			return Page{}, err
		}
		where += " AND (" + filter + ")"
	}
	source, err := aggregateSource(ctx, q, s, a, &b)
	if err != nil {
		return Page{}, err
	}
	input, result := "1", "count(*),0::bigint,''::text"
	var normalized string
	if query.Aggregate.Op == "sum" {
		fieldKey := b.bind(f.ID) + "::text"
		rawValue := "r.custom_data->" + fieldKey
		input = rawValue + " AS v"
		valid, number := aggregateValueSQL(f, &b, "v")
		normalized = "CASE WHEN " + valid + " THEN COALESCE(" + number + ",0::numeric) ELSE NULL END AS n"
		index, indexErr := numericIndex(ctx, q, s, a, e.ID, f.ID)
		if indexErr != nil && !errors.Is(indexErr, ErrNotFound) {
			return Page{}, indexErr
		}
		if indexErr == nil && index.State == "READY" {
			// Versioned per-write certification binds raw validity to this exact
			// projected value/source version. Legacy NULL flags retain raw checks.
			source += ` LEFT JOIN (SELECT id,source_version,aggregate_valid_v1,value_number FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5 AND field_id=` + b.bind(f.ID) + ` OFFSET 0) p ON p.id=r.id`
			// Late exact point lookup keeps raw JSON out of certified header
			// scans. JSON null means an optional missing value in an existing
			// row; SQL NULL means the lookup itself is missing and must deny.
			late := `(SELECT COALESCE(l.custom_data->` + fieldKey + `,'null'::jsonb) FROM runtime.records l WHERE l.tenant_id=$1 AND l.app_id=$2 AND l.metadata_revision=$3 AND l.ir_hash=$4 AND l.entity_id=$5 AND l.id=r.id AND l.row_version=r.row_version)`
			input = "CASE WHEN p.id IS NOT NULL AND p.source_version=r.row_version AND p.aggregate_valid_v1 IS NULL THEN " + late + " ELSE NULL END AS v,r.row_version AS rv,p.id AS pid,p.source_version AS pv,p.aggregate_valid_v1 AS certified,p.value_number AS projected"
			normalized = "CASE WHEN pid IS NULL OR pv IS DISTINCT FROM rv THEN NULL WHEN certified IS TRUE THEN COALESCE(projected,0::numeric) WHEN certified IS FALSE OR v IS NULL THEN NULL ELSE " + strings.TrimSuffix(normalized, " AS n") + " END AS n"
		}
		result = "count(*),count(*)-count(n),COALESCE(sum(n),0)::text"
	}
	selection := `SELECT ` + input + ` FROM ` + source + ` WHERE ` + where + ` LIMIT ` + b.bind(MaxSQLAggregateRows+1)
	if normalized != "" {
		// LIMIT preserves the raw-scalar projection boundary; OFFSET preserves
		// the single guarded normalization before both outer aggregate inputs.
		selection = "SELECT " + normalized + " FROM (" + selection + ") aggregate_values OFFSET 0"
	}
	statement := `WITH aggregate_input AS (` + selection + `) SELECT ` + result + ` FROM aggregate_input`
	// This closed OLTP kernel avoids LLVM and unsuitable generic plans (ADR053).
	// Change only the certified transaction, preserving successful composition
	// and allowing WithRuntime rollback to reset SQL-aborted/cancelled paths.
	var jit, planMode string
	if err = q.QueryRowContext(ctx, `SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&jit, &planMode); err != nil {
		return Page{}, err
	}
	if _, err = q.ExecContext(ctx, `SELECT set_config('jit','off',true),set_config('plan_cache_mode','force_custom_plan',true)`); err != nil {
		return Page{}, err
	}
	defer func() {
		if cause := recover(); cause != nil {
			// Preserve driver/tracer panic; WithRuntime owns the rollback.
			// Do not issue restoration SQL while unwinding.
			panic(cause)
		}
		var state interface{ SQLState() string }
		if errors.As(resultErr, &state) {
			return
		}
		if _, err := q.ExecContext(ctx, `SELECT set_config('jit',$1,true),set_config('plan_cache_mode',$2,true)`, jit, planMode); err != nil && resultErr == nil {
			page, resultErr = Page{}, ErrUnavailable
		}
	}()
	var count, invalid int64
	var sum string
	if err = q.QueryRowContext(ctx, statement, b.args...).Scan(&count, &invalid, &sum); err != nil {
		return Page{}, err
	}
	if count > MaxSQLAggregateRows {
		return Page{}, ErrBudget
	}
	if count < 0 || invalid != 0 {
		return Page{}, ErrUnavailable
	}
	value := AggregateResult{Count: int(count)}
	if query.Aggregate.Op == "sum" {
		value.Sum, err = canonicalAggregateSum(sum)
		if err != nil {
			return Page{}, err
		}
	}
	return Page{Records: []Record{}, Aggregate: &value}, nil
}

// Resolve the view branch in the SAME certified snapshot as the aggregate.
// Scratch URIs can have adopted/evolved mappings too. A retained pin is a read
// witness, not a requirement to match the latest generation or active binding.
func aggregateSource(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, b *builder) (string, error) {
	// Bound physical access independently of outer ACL/filter joins. There is no
	// inner cap: all value-dependent predicates still precede LIMIT100001.
	physical := `(SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,row_version,owner_user_id,owner_team_id FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5 OFFSET 0)`
	var dataset string
	var generation int64
	var canonical bool
	err := q.QueryRowContext(ctx, `SELECT d.id::text,g.generation,d.canonical_access FROM runtime.dataset_generations g JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id
 WHERE g.tenant_id=$1 AND g.app_id=$2 AND g.anchor_revision=$3 AND g.anchor_hash=$4`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash()).Scan(&dataset, &generation, &canonical)
	if errors.Is(err, sql.ErrNoRows) {
		if s.DatasetID() != "" {
			return "", ErrDenied
		}
		return physical + " r", nil
	}
	if err != nil {
		return "", err
	}
	if s.DatasetID() != "" && (dataset != s.DatasetID() || generation != s.Generation()) {
		return "", ErrDenied
	}
	if !canonical {
		return physical + " r", nil
	}
	id := b.bind(dataset) + "::uuid"
	return `(SELECT src.tenant_id,src.app_id,src.metadata_revision,src.ir_hash,src.entity_id,src.id,src.custom_data,src.row_version,c.owner_user_id,c.owner_team_id
 FROM ` + physical + ` src JOIN (SELECT id,owner_user_id,owner_team_id FROM runtime.record_identity WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=` + id + ` AND entity_id=$5 AND NOT tombstone OFFSET 0) c ON c.id=src.id
 WHERE src.tenant_id=$1 AND src.app_id=$2 AND src.metadata_revision=$3 AND src.ir_hash=$4 AND src.entity_id=$5) r`, nil
}

func aggregateShape(e compiler.Entity, query Query) (compiler.Field, error) {
	if query.Aggregate == nil || query.Aggregate.Execution != SQLAggregateV1 || query.Limit != MaxPage || len(query.Projection) == 0 || len(query.Projection) > 32 || query.RecordID != "" || query.After != "" || query.Sort != nil || query.Join != nil || query.Search != nil {
		return compiler.Field{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range query.Projection {
		if _, ok := field(e, id); !ok || seen[id] {
			return compiler.Field{}, ErrInvalid
		}
		seen[id] = true
	}
	if query.Aggregate.Op == "count" && query.Aggregate.Field == "" {
		return compiler.Field{}, nil
	}
	f, ok := field(e, query.Aggregate.Field)
	if query.Aggregate.Op != "sum" || !ok || !seen[f.ID] || f.CoreID || f.Type != "integer" && f.Type != "decimal" && f.Type != "money" {
		return compiler.Field{}, ErrInvalid
	}
	return f, nil
}

// JSONB is not a type-validation witness. Validate EACH summed input before a
// guarded cast and count invalid inputs separately, including cancelling pairs.
func aggregateValueSQL(f compiler.Field, b *builder, value string) (valid, number string) {
	text := "(" + value + " #>> '{}')"
	kind := "string"
	pattern, limit, length := `^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`, "1000000000000000000", "64"
	if f.Type == "integer" {
		kind, pattern, limit, length = "number", `^-?(0|[1-9][0-9]*)$`, "9007199254740991", "17"
	}
	if f.Type == "money" {
		text = "(" + value + "->>'amount')"
	}
	lexical := b.bind(pattern) + "::text"
	numeric := "CASE WHEN length(" + text + ")<=" + length + " AND " + text + " ~ " + lexical + " THEN abs(" + text + "::numeric)<=" + limit + "::numeric ELSE false END"
	typed := "CASE WHEN jsonb_typeof(" + value + ")='" + kind + "' THEN (" + numeric + ") ELSE false END"
	if f.Type == "money" {
		currency := b.bind(f.Currency) + "::text"
		typed = "CASE WHEN jsonb_typeof(" + value + ")='object' THEN (" + value + "-'amount'-'currency'='{}'::jsonb AND " + value + "->'currency'=to_jsonb(" + currency + ") AND CASE WHEN jsonb_typeof(" + value + "->'amount')='string' THEN (" + numeric + ") ELSE false END) ELSE false END"
	}
	null := "true"
	if f.Required {
		null = "false"
	}
	valid = "CASE WHEN " + value + " IS NULL OR " + value + "='null'::jsonb THEN " + null + " ELSE (" + typed + ") END"
	number = text + "::numeric"
	return valid, number
}

func canonicalAggregateSum(value string) (string, error) {
	if len(value) == 0 || len(value) > 128 || !decimalPattern.MatchString(value) {
		return "", ErrUnavailable
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", ErrUnavailable
	}
	result := strings.TrimRight(strings.TrimRight(n.FloatString(18), "0"), ".")
	if result == "" || result == "-0" {
		result = "0"
	}
	return result, nil
}
