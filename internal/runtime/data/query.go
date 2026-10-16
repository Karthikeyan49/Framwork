package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"math/big"
	"slices"
	"sort"
	"strings"
)

type builder struct{ args []any }

func (b *builder) bind(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", len(b.args))
}
func physical(f compiler.Field, alias string, b *builder) (string, string) {
	if f.CoreID {
		return alias + ".id", "uuid"
	}
	key := b.bind(f.ID)
	expr := alias + ".custom_data->>" + key
	typ := "text"
	switch f.Type {
	case "integer", "decimal":
		typ = "numeric"
	case "money":
		expr = alias + ".custom_data->" + key + "->>'amount'"
		typ = "numeric"
	case "bool":
		typ = "boolean"
	case "date":
		typ = "date"
	case "datetime":
		typ = "timestamptz"
	}
	return "(" + expr + ")::" + typ, typ
}
func sqlValue(f compiler.Field, v any) any {
	if f.Type == "money" && v != nil {
		return v.(map[string]any)["amount"]
	}
	return v
}
func filterSQL(ctx context.Context, e compiler.Entity, f Filter, b *builder, g Guard, depth int, nodes *int) (string, error) {
	*nodes++
	if depth > 3 || *nodes > 32 {
		return "", ErrBudget
	}
	if f.Op == "and" || f.Op == "or" {
		if f.Field != "" || len(f.Value) > 0 || len(f.Args) < 2 || len(f.Args) > 8 {
			return "", ErrInvalid
		}
		parts := []string{}
		for _, child := range f.Args {
			p, err := filterSQL(ctx, e, child, b, g, depth+1, nodes)
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		return "(" + strings.Join(parts, " "+strings.ToUpper(f.Op)+" ") + ")", nil
	}
	if len(f.Args) > 0 {
		return "", ErrInvalid
	}
	field, ok := field(e, f.Field)
	if !ok {
		return "", ErrInvalid
	}
	if err := g.check(ctx, "data.field.read", Resource{EntityID: e.ID, FieldID: field.ID}); err != nil {
		return "", err
	}
	expr, typ := physical(field, "r", b)
	if f.Op == "is_null" {
		if len(f.Value) > 0 {
			return "", ErrInvalid
		}
		return expr + " IS NULL", nil
	}
	value, err := metadata.DecodeJSON(f.Value)
	if err != nil {
		return "", ErrInvalid
	}
	operators := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
	if f.Op == "in" || f.Op == "not_in" {
		list, ok := value.([]any)
		if !ok || len(list) == 0 || len(list) > 32 {
			return "", ErrInvalid
		}
		values := []string{}
		for _, item := range list {
			v, err := fieldValue(field, item)
			if err != nil || v == nil {
				return "", ErrInvalid
			}
			values = append(values, b.bind(sqlValue(field, v))+"::"+typ)
		}
		op := " IN "
		if f.Op == "not_in" {
			op = " NOT IN "
		}
		return expr + op + "(" + strings.Join(values, ",") + ")", nil
	}
	v, err := fieldValue(field, value)
	if err != nil || v == nil {
		return "", ErrInvalid
	}
	if f.Op == "contains" {
		if field.Type != "string" && !field.CoreID {
			return "", ErrInvalid
		}
		if field.CoreID {
			return "", ErrInvalid
		}
		return "strpos(" + expr + "," + b.bind(v) + "::text)>0", nil
	}
	op, ok := operators[f.Op]
	if !ok {
		return "", ErrInvalid
	}
	if f.Op != "eq" && f.Op != "neq" && field.Type == "bool" {
		return "", ErrInvalid
	}
	return expr + op + b.bind(sqlValue(field, v)) + "::" + typ, nil
}

type cursor struct {
	ID          string          `json:"id"`
	Value       json.RawMessage `json:"value"`
	Fingerprint string          `json:"fingerprint"`
}

// Includes worst-case JSON escaping of the existing 4096-byte string contract.
const maxCursorBytes = 32 << 10

func fingerprint(s Scope, a tenant.Access, e string, q Query) string {
	q.After = ""
	raw, _ := json.Marshal(q)
	identity := a.TenantID() + s.AppID() + fmt.Sprint(s.Revision()) + s.Hash() + e + string(raw)
	if s.DatasetID() != "" {
		identity += "/dataset/" + s.DatasetID() + "/binding/" + fmt.Sprint(s.BindingVersion())
	}
	h := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(h[:])
}
func project(r Record, ids []string) Record {
	out := map[string]any{}
	for _, id := range ids {
		if v, ok := r.Values[id]; ok {
			out[id] = v
		}
	}
	r.Values = out
	if r.FieldAccess != nil {
		a := &FieldAccess{Masked: []string{}, Writable: []string{}}
		for _, id := range ids {
			if slices.Contains(r.FieldAccess.Masked, id) {
				a.Masked = append(a.Masked, id)
			}
			if slices.Contains(r.FieldAccess.Writable, id) {
				a.Writable = append(a.Writable, id)
			}
		}
		r.FieldAccess = a
	}
	return r
}
func (st Store) Query(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, query Query, g Guard, resolve ...ScopeResolver) (Page, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return Page{}, err
	}
	ctx, span := otel.Tracer("framework/data").Start(ctx, "data.query")
	defer span.End()
	span.SetAttributes(attribute.String("entity_id", entity), attribute.String("ir_hash", s.Hash()), attribute.Int64("metadata_version", s.Revision()), attribute.String("data_mode", "preview"))
	if err = g.check(ctx, "data.entity.read", Resource{EntityID: entity}); err != nil {
		return Page{}, err
	}
	if err = g.check(ctx, "data.row.read", Resource{EntityID: entity}); err != nil {
		return Page{}, err
	}
	if query.Limit < 1 || query.Limit > MaxPage || len(query.After) > base64.RawURLEncoding.EncodedLen(maxCursorBytes) || len(query.Projection) == 0 || len(query.Projection) > 32 {
		return Page{}, ErrBudget
	}
	if st.visible && !st.sqlFields || !st.visible && query.RecordID != "" {
		if !postgres.RuntimeSnapshot(q) {
			return Page{}, ErrDenied
		}
		var stable bool
		if err = q.QueryRowContext(ctx, `SELECT current_setting('transaction_isolation') IN ('repeatable read','serializable')`).Scan(&stable); err != nil {
			return Page{}, err
		}
		if !stable {
			return Page{}, ErrDenied
		}
	}
	seen := map[string]bool{}
	projected := map[string]bool{}
	for _, id := range query.Projection {
		if _, ok := field(e, id); !ok || projected[id] {
			return Page{}, ErrInvalid
		}
		seen[id] = !st.visible
		projected[id] = true
		if !st.visible && query.RecordID == "" {
			if err = g.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: id}); err != nil {
				return Page{}, err
			}
		}
	}
	b := builder{args: base(s, a, entity)}
	where := `r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND r.entity_id=$5`
	if query.RecordID != "" {
		if !tenant.ValidID(query.RecordID) {
			return Page{}, ErrInvalid
		}
		where += " AND r.id=" + b.bind(query.RecordID) + "::uuid"
	}
	accessScope, err := resolveScope(ctx, resolve, "data.read", Resource{EntityID: entity})
	if err != nil {
		return Page{}, err
	}
	n := 0
	scopePredicate, err := scopeSQL(ctx, e, accessScope, a, "r", &b, 0, &n)
	if err != nil {
		return Page{}, err
	}
	where += " AND (" + scopePredicate + ")"
	predicateGuard := g
	if !st.visible && query.RecordID != "" {
		var found bool
		predicateGuard, found, err = checkPointInputActions(ctx, q, st, s, e, query, where, b, g)
		if err != nil {
			return Page{}, err
		}
		if !found {
			return Page{Records: []Record{}}, nil
		}
	}
	if st.visible {
		predicateGuard = func(ctx context.Context, action string, r Resource) error {
			if st.sqlFields {
				if er := g.check(ctx, action, r); er != nil {
					return er
				}
			}
			fs, er := resolveScope(ctx, resolve, "data.read", r)
			if er != nil {
				return er
			}
			if !coversEntity(fs, accessScope) {
				return ErrDenied
			}
			return nil
		}
	}
	if st.visible && !st.sqlFields {
		ids, er := predicateFields(s, e, query)
		if er != nil {
			return Page{}, er
		}
		for _, id := range ids {
			if er = predicateGuard.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: id}); er != nil {
				return Page{}, er
			}
		}
		if er = checkPredicateActions(ctx, q, st, e, where, b, ids, g); er != nil {
			return Page{}, er
		}
	}
	if query.Filter != nil {
		nodes := 0
		condition, err := filterSQL(ctx, e, *query.Filter, &b, predicateGuard, 0, &nodes)
		if err != nil {
			return Page{}, err
		}
		where += " AND " + condition
		var collect func(Filter)
		collect = func(f Filter) {
			if f.Field != "" {
				seen[f.Field] = true
			}
			for _, child := range f.Args {
				collect(child)
			}
		}
		collect(*query.Filter)
	}
	sf := compiler.Field{Type: "string", CoreID: true}
	sortExpr, sortType := "r.id", "uuid"
	indexJoin := ""
	scalarFrom, scalarPredicate := "", ""
	desc := false
	if query.Sort != nil {
		var ok bool
		sf, ok = field(e, query.Sort.Field)
		if !ok {
			return Page{}, ErrInvalid
		}
		if err = predicateGuard.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: sf.ID}); err != nil {
			return Page{}, err
		}
		desc = query.Sort.Desc
		seen[sf.ID] = true
		if !st.workerSource && !st.legacy && (sf.Type == "integer" || sf.Type == "decimal" || sf.Type == "money") {
			index, indexErr := numericIndex(ctx, q, s, a, entity, sf.ID)
			if indexErr != nil && indexErr != ErrNotFound {
				return Page{}, indexErr
			}
			if indexErr == nil && index.State == "READY" {
				// Same tenant/physical namespace, source row version and snapshot.
				// All authorization/filter/cursor predicates remain before LIMIT.
				indexJoin = ` JOIN runtime.numeric_projections ni ON ni.tenant_id=$1 AND ni.app_id=$2 AND ni.metadata_revision=$3 AND ni.ir_hash=$4 AND ni.entity_id=$5 AND ni.field_id=` + b.bind(sf.ID) + ` AND ni.id=r.id AND ni.source_version=r.row_version`
				sortExpr = "ni.value_number"
				sortType = "numeric"
				span.SetAttributes(attribute.String("query_index", "numeric-v1"))
			}
		}
		if !st.workerSource && !st.legacy && scalarType(sf.Type) && !sf.CoreID && postgres.RuntimeSnapshot(q) {
			index, indexErr := scalarIndex(ctx, q, s, a, entity, sf.ID)
			if indexErr != nil && indexErr != ErrNotFound {
				return Page{}, indexErr
			}
			if indexErr == nil && index.State == "READY" {
				usable, indexErr := scalarSortUsable(ctx, q, s, a, entity, sf)
				if indexErr != nil {
					return Page{}, indexErr
				}
				if usable {
					scalarPredicate = `si.tenant_id=$1 AND si.app_id=$2 AND si.metadata_revision=$3 AND si.ir_hash=$4 AND si.entity_id=$5 AND si.field_id=` + b.bind(sf.ID)
					switch sf.Type {
					case "string":
						scalarPredicate += ` AND si.field_type='string' AND NOT si.overflow`
						sortExpr, sortType = "si.value_text", "text"
					case "enum":
						scalarPredicate += ` AND si.field_type='enum'`
						sortExpr, sortType = "si.value_text", "text"
					case "reference":
						scalarPredicate += ` AND si.field_type='reference'`
						sortExpr, sortType = "si.value_text", "text"
					case "date":
						scalarPredicate += ` AND si.field_type='date'`
						sortExpr, sortType = "si.value_date", "date"
					}
					indexJoin = ` JOIN runtime.scalar_projections si ON ` + scalarPredicate + ` AND si.id=r.id AND si.source_version=r.row_version`
					// Prevent the live ACL view from being flattened into a dataset
					// scan for every projection candidate under cold statistics.
					// OFFSET 0 keeps namespace/ID/version inside the point lookup.
					scalarFrom = `runtime.scalar_projections si JOIN LATERAL (SELECT r.* FROM ` + st.recordsTable() + ` r WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND r.entity_id=$5 AND r.id=si.id AND r.row_version=si.source_version OFFSET 0) r ON true`
					span.SetAttributes(attribute.String("query_index", "scalar-v1-"+sf.Type))
				}
			}
		}
		if indexJoin == "" {
			sortExpr, sortType = physical(sf, "r", &b)
		}
	}
	fp := fingerprint(s, a, entity, query)
	span.SetAttributes(attribute.String("query_fingerprint", fp))
	if query.After != "" {
		if len(query.After) > base64.RawURLEncoding.EncodedLen(maxCursorBytes) {
			return Page{}, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(query.After)
		if err != nil || len(raw) > maxCursorBytes {
			return Page{}, ErrInvalid
		}
		var c cursor
		if json.Unmarshal(raw, &c) != nil || !tenant.ValidID(c.ID) || c.Fingerprint != fp {
			return Page{}, ErrInvalid
		}
		idArg := b.bind(c.ID) + "::uuid"
		cursorID := "r.id"
		if scalarFrom != "" {
			// The point lookup proves equality. Keep cursor pruning on the
			// projection so skipped keys do not fetch business records.
			cursorID = "si.id"
		}
		value, err := metadata.DecodeJSON(c.Value)
		if err != nil {
			return Page{}, ErrInvalid
		}
		if value == nil {
			where += " AND (" + sortExpr + " IS NULL AND " + cursorID + ">" + idArg + ")"
		} else {
			v, err := fieldValue(sf, value)
			if err != nil {
				return Page{}, ErrInvalid
			}
			val := b.bind(sqlValue(sf, v)) + "::" + sortType
			op := ">"
			if desc {
				op = "<"
			}
			where += " AND (" + sortExpr + op + val + " OR (" + sortExpr + "=" + val + " AND " + cursorID + ">" + idArg + ") OR " + sortExpr + " IS NULL)"
		}
	}
	joinSQL := ""
	joinedColumns := `NULL::text,NULL::bigint,NULL::text,NULL::timestamptz,NULL::timestamptz,NULL::jsonb`
	var target compiler.Entity
	if query.Join != nil {
		if query.Aggregate != nil || len(query.Join.Projection) == 0 || len(query.Join.Projection) > 32 {
			return Page{}, ErrInvalid
		}
		var relation *metadata.Relationship
		for _, d := range s.program.IR().Application.Definitions {
			if d.ID == query.Join.Relationship {
				relation = d.Relationship
			}
		}
		if relation == nil || relation.FromEntity != entity {
			return Page{}, ErrInvalid
		}
		target, _ = s.program.Entity(relation.ToEntity)
		seen[relation.FromField] = true
		if err = g.check(ctx, "data.entity.read", Resource{EntityID: target.ID}); err != nil {
			return Page{}, err
		}
		if err = g.check(ctx, "data.row.read", Resource{EntityID: target.ID}); err != nil {
			return Page{}, err
		}
		if err = predicateGuard.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: relation.FromField}); err != nil {
			return Page{}, err
		}
		joinSeen := map[string]bool{}
		for _, id := range query.Join.Projection {
			if _, ok := field(target, id); !ok || joinSeen[id] {
				return Page{}, ErrInvalid
			}
			joinSeen[id] = true
			if !st.visible && query.RecordID == "" {
				if err = g.check(ctx, "data.field.read", Resource{EntityID: target.ID, FieldID: id}); err != nil {
					return Page{}, err
				}
			}
		}
		joinSQL = ` LEFT JOIN ` + st.recordsTable() + ` j ON j.tenant_id=r.tenant_id AND j.app_id=r.app_id AND j.metadata_revision=r.metadata_revision AND j.ir_hash=r.ir_hash AND j.entity_id=` + b.bind(target.ID) + ` AND j.id=(r.custom_data->>` + b.bind(relation.FromField) + `)::uuid`
		targetScope, er := resolveScope(ctx, resolve, "data.read", Resource{EntityID: target.ID})
		if er != nil {
			return Page{}, er
		}
		ns := 0
		jp, er := scopeSQL(ctx, target, targetScope, a, "j", &b, 0, &ns)
		if er != nil {
			return Page{}, er
		}
		joinSQL += " AND (" + jp + ")"
		if !st.visible {
			for _, id := range query.Join.Projection {
				fs, er := resolveScope(ctx, resolve, "data.read", Resource{EntityID: target.ID, FieldID: id})
				if er != nil {
					return Page{}, er
				}
				nodes := 0
				pred, er := scopeSQL(ctx, target, fs, a, "j", &b, 0, &nodes)
				if er != nil {
					return Page{}, er
				}
				joinSQL += " AND (" + pred + ")"
			}
		}
		joinedData := "j.custom_data"
		joinedColumns = `j.id::text,j.row_version,j.created_by::text,j.created_at,j.updated_at,` + joinedData
	}
	if query.Aggregate != nil {
		if query.After != "" || query.Sort != nil {
			return Page{}, ErrInvalid
		}
		if (query.Aggregate.Op == "count" && query.Aggregate.Field != "") || (query.Aggregate.Op != "count" && query.Aggregate.Op != "sum") {
			return Page{}, ErrInvalid
		}
		if query.Aggregate.Op == "sum" {
			f, ok := field(e, query.Aggregate.Field)
			if !ok || (f.Type != "integer" && f.Type != "decimal" && f.Type != "money") {
				return Page{}, ErrInvalid
			}
			if st.visible {
				if er := predicateGuard.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: f.ID}); er != nil {
					return Page{}, er
				}
				seen[f.ID] = true
			}
			if !projected[f.ID] {
				return Page{}, ErrInvalid
			}
		}
		if err = g.check(ctx, "data.aggregate.read", Resource{EntityID: entity}); err != nil {
			return Page{}, err
		}
	}
	keys := []string{}
	for key, used := range seen {
		if used {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		fs, er := resolveScope(ctx, resolve, "data.read", Resource{EntityID: entity, FieldID: key})
		if er != nil {
			return Page{}, er
		}
		if st.visible {
			if !coversEntity(fs, accessScope) {
				return Page{}, ErrDenied
			}
			continue
		}
		n := 0
		fp, er := scopeSQL(ctx, e, fs, a, "r", &b, 0, &n)
		if er != nil {
			return Page{}, er
		}
		where += " AND (" + fp + ")"
	}
	readIDs := append([]string(nil), query.Projection...)
	if query.Sort != nil && !slices.Contains(readIDs, sf.ID) {
		readIDs = append(readIDs, sf.ID)
	}
	order := "ASC"
	if desc {
		order = "DESC"
	}
	if !st.visible && query.RecordID != "" && query.Join != nil {
		if _, er := projectActions(ctx, q, st, e, target, query, readIDs, where, indexJoin, joinSQL, sortExpr, order, b, g, true); er != nil {
			return Page{}, er
		}
	}
	selectedData := "r.custom_data"
	if !st.visible && query.RecordID != "" {
		selectedData = projectedJSON(e, readIDs, "r", &b)
		if query.Join != nil {
			joinedColumns = `j.id::text,j.row_version,j.created_by::text,j.created_at,j.updated_at,` + projectedJSON(target, query.Join.Projection, "j", &b)
		}
	}

	if st.visible {
		var actions fieldActions
		if !st.sqlFields {
			actions, err = projectActions(ctx, q, st, e, target, query, readIDs, where, indexJoin, joinSQL, sortExpr, order, b, g, false)
			if err != nil {
				return Page{}, err
			}
		}
		selectedData, err = visibleJSON(ctx, e, readIDs, a, "r", &b, g, resolve, actions)
		if err != nil {
			return Page{}, err
		}
		if query.Join != nil {
			joinedData, er := visibleJSON(ctx, target, query.Join.Projection, a, "j", &b, g, resolve, actions)
			if er != nil {
				return Page{}, er
			}
			joinedColumns = `j.id::text,j.row_version,j.created_by::text,j.created_at,j.updated_at,` + joinedData
		}
	}

	statement := `SELECT r.id::text,r.row_version,r.created_by::text,r.created_at,r.updated_at,` + selectedData + `,r.owner_user_id::text,COALESCE(r.owner_team_id::text,''),` + joinedColumns + ` FROM ` + st.recordsTable() + ` r` + indexJoin + joinSQL + ` WHERE ` + where + ` ORDER BY ` + sortExpr + ` ` + order + ` NULLS LAST,r.id ASC LIMIT ` + b.bind(query.Limit+1)
	if query.Sort != nil && !sf.CoreID && !st.workerSource {
		// Bound the sort tuple to ID/key before fetching wide JSONB records. Both
		// stages share one statement/snapshot and forced RLS; entity/field/row
		// DataScope and cursor predicates select the page before LIMIT. The outer
		// fetch is constrained to the same namespace and those selected IDs.
		limit := fmt.Sprintf("$%d", len(b.args))
		pageFrom, pageWhere, pageID := st.recordsTable()+" r"+indexJoin, where, "r.id"
		if scalarFrom != "" {
			pageFrom, pageWhere, pageID = scalarFrom, where+" AND "+scalarPredicate, "si.id"
		}
		statement = `WITH selected_page AS MATERIALIZED (SELECT ` + pageID + ` AS id,` + sortExpr + ` AS sort_value FROM ` + pageFrom + ` WHERE ` + pageWhere + ` ORDER BY ` + sortExpr + ` ` + order + ` NULLS LAST,` + pageID + ` ASC LIMIT ` + limit + `)
 SELECT r.id::text,r.row_version,r.created_by::text,r.created_at,r.updated_at,` + selectedData + `,r.owner_user_id::text,COALESCE(r.owner_team_id::text,''),` + joinedColumns + ` FROM selected_page p JOIN LATERAL (SELECT r.* FROM ` + st.recordsTable() + ` r WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND r.entity_id=$5 AND r.id=p.id OFFSET 0) r ON true` + joinSQL + ` ORDER BY p.sort_value ` + order + ` NULLS LAST,r.id ASC`
	}
	rows, err := q.QueryContext(ctx, statement, b.args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	records := []Record{}
	var lastValue any
	hasMore := false
	for rows.Next() {
		var r Record
		var raw, joinedRaw []byte
		var jid, owner sql.NullString
		var version sql.NullInt64
		var created, updated sql.NullTime
		if err = rows.Scan(&r.ID, &r.Version, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &raw, &r.OwnerID, &r.TeamID, &jid, &version, &owner, &created, &updated, &joinedRaw); err != nil {
			return Page{}, err
		}
		readEntity := e
		if st.workerSource || !st.visible && query.RecordID != "" {
			readEntity.Fields = nil
			for _, f := range e.Fields {
				if f.CoreID || slices.Contains(readIDs, f.ID) {
					readEntity.Fields = append(readEntity.Fields, f)
				}
			}
		}
		if st.visible {
			err = readVisibleValues(e, readIDs, &r, raw)
		} else {
			err = readValues(readEntity, &r, raw)
		}
		if err != nil {
			return Page{}, err
		}
		if query.Join != nil {
			r.Related = map[string]*Record{query.Join.Relationship: nil}
			if jid.Valid {
				joined := Record{ID: jid.String, Version: version.Int64, CreatedBy: owner.String, CreatedAt: created.Time, UpdatedAt: updated.Time}
				if st.visible {
					err = readVisibleValues(target, query.Join.Projection, &joined, joinedRaw)
				} else {
					joinedEntity := target
					if query.RecordID != "" {
						joinedEntity.Fields = nil
						for _, f := range target.Fields {
							if f.CoreID || slices.Contains(query.Join.Projection, f.ID) {
								joinedEntity.Fields = append(joinedEntity.Fields, f)
							}
						}
					}
					err = readValues(joinedEntity, &joined, joinedRaw)
				}
				if err != nil {
					return Page{}, err
				}
				joined = project(joined, query.Join.Projection)
				r.Related[query.Join.Relationship] = &joined
			}
		}
		if len(records) == query.Limit {
			hasMore = true
			break
		}
		if sf.ID == "" || sf.CoreID {
			lastValue = r.ID
		} else {
			lastValue = r.Values[sf.ID]
		}
		records = append(records, project(r, query.Projection))
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	if err = rows.Close(); err != nil {
		return Page{}, err
	}
	if hasMore && query.Aggregate != nil {
		return Page{}, ErrBudget
	}
	page := Page{Records: records, Next: ""}
	// The cursor is generated only when an extra result was observed.
	if hasMore && query.Aggregate == nil {
		value, _ := json.Marshal(lastValue)
		encoded, _ := json.Marshal(cursor{records[len(records)-1].ID, value, fp})
		page.Next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	if query.Aggregate != nil {
		sum := new(big.Rat)
		for _, r := range records {
			if query.Aggregate.Op == "sum" && r.Values[query.Aggregate.Field] != nil {
				f, _ := field(e, query.Aggregate.Field)
				v := sqlValue(f, r.Values[f.ID])
				amount := fmt.Sprint(v)
				n, ok := new(big.Rat).SetString(amount)
				if !ok {
					return Page{}, ErrUnavailable
				}
				sum.Add(sum, n)
			}
		}
		result := AggregateResult{Count: len(records)}
		if query.Aggregate.Op == "sum" {
			result.Sum = strings.TrimRight(strings.TrimRight(sum.FloatString(18), "0"), ".")
			if result.Sum == "" || result.Sum == "-0" {
				result.Sum = "0"
			}
		}
		page.Aggregate = &result
		page.Records = []Record{}
	}
	return page, nil
}
