package data

import (
	"context"
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
func (st Store) aggregateSQL(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e compiler.Entity, query Query, g Guard, resolve []ScopeResolver) (Page, error) {
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
	input, result := "1", "count(*),0::bigint,''::text"
	if query.Aggregate.Op == "sum" {
		input = "r.custom_data->" + b.bind(f.ID) + "::text AS v"
		valid, number := aggregateValueSQL(f, &b)
		result = "count(*),count(*) FILTER (WHERE NOT COALESCE((" + valid + "),false)),COALESCE(sum(CASE WHEN " + valid + " THEN " + number + " ELSE NULL END),0)::text"
	}
	statement := `WITH aggregate_input AS MATERIALIZED (SELECT ` + input + ` FROM ` + st.recordsTable() + ` r WHERE ` + where + ` LIMIT ` + b.bind(MaxSQLAggregateRows+1) + `) SELECT ` + result + ` FROM aggregate_input`
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
func aggregateValueSQL(f compiler.Field, b *builder) (valid, number string) {
	text := "(v #>> '{}')"
	kind := "string"
	pattern, limit, length := `^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`, "1000000000000000000", "64"
	if f.Type == "integer" {
		kind, pattern, limit, length = "number", `^-?(0|[1-9][0-9]*)$`, "9007199254740991", "17"
	}
	if f.Type == "money" {
		text = "(v->>'amount')"
	}
	lexical := b.bind(pattern) + "::text"
	numeric := "CASE WHEN length(" + text + ")<=" + length + " AND " + text + " ~ " + lexical + " THEN abs(" + text + "::numeric)<=" + limit + "::numeric ELSE false END"
	typed := "CASE WHEN jsonb_typeof(v)='" + kind + "' THEN (" + numeric + ") ELSE false END"
	if f.Type == "money" {
		currency := b.bind(f.Currency) + "::text"
		typed = "CASE WHEN jsonb_typeof(v)='object' THEN (v-'amount'-'currency'='{}'::jsonb AND v->'currency'=to_jsonb(" + currency + ") AND CASE WHEN jsonb_typeof(v->'amount')='string' THEN (" + numeric + ") ELSE false END) ELSE false END"
	}
	null := "true"
	if f.Required {
		null = "false"
	}
	valid = "CASE WHEN v IS NULL OR v='null'::jsonb THEN " + null + " ELSE (" + typed + ") END"
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
