package data

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// Opaque Go callbacks cannot be translated or proven row-independent. Bound
// both the complete predicate universe and the projected header page. Native
// and versioned capability policies use their closed SQL-scope action kernel.
const MaxOpaqueActionRows = 32

type fieldActions map[string][]string

func actionKey(e, field, action string) string { return e + "/" + field + "/" + action }
func (d fieldActions) allow(e, field, action, id string) {
	k := actionKey(e, field, action)
	if !slices.Contains(d[k], id) {
		d[k] = append(d[k], id)
	}
}
func (d fieldActions) predicate(e, field, action, alias string, b *builder) string {
	ids := d[actionKey(e, field, action)]
	if len(ids) == 0 {
		return "FALSE"
	}
	return alias + ".id = ANY(" + b.bind(ids) + "::uuid[])"
}
func predicateFields(s Scope, e compiler.Entity, query Query) ([]string, error) {
	ids := []string{}
	add := func(id string) error {
		if _, ok := field(e, id); !ok {
			return ErrInvalid
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
		return nil
	}
	nodes := 0
	var visit func(Filter, int) error
	visit = func(f Filter, depth int) error {
		nodes++
		if depth > 3 || nodes > 32 {
			return ErrBudget
		}
		if f.Field != "" {
			if err := add(f.Field); err != nil {
				return err
			}
		}
		for _, c := range f.Args {
			if err := visit(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if query.Filter != nil {
		if err := visit(*query.Filter, 0); err != nil {
			return nil, err
		}
	}
	if query.Sort != nil {
		if err := add(query.Sort.Field); err != nil {
			return nil, err
		}
	}
	if query.Aggregate != nil && query.Aggregate.Op == "sum" {
		if err := add(query.Aggregate.Field); err != nil {
			return nil, err
		}
	}
	if query.Join != nil {
		found := false
		for _, d := range s.program.IR().Application.Definitions {
			if d.ID == query.Join.Relationship && d.Relationship != nil && d.Relationship.FromEntity == e.ID {
				if err := add(d.Relationship.FromField); err != nil {
					return nil, err
				}
				found = true
				break
			}
		}
		if !found {
			return nil, ErrInvalid
		}
	}
	return ids, nil
}

// This query contains only entity scope and an optional exact ID, never a
// caller field predicate, cursor, sort, join or requested result limit.
func checkPredicateActions(ctx context.Context, q postgres.Executor, st Store, e compiler.Entity, where string, b builder, ids []string, g Guard) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT r.id::text,r.owner_user_id::text FROM `+st.recordsTable()+` r WHERE `+where+` ORDER BY r.id LIMIT `+strconv.Itoa(MaxOpaqueActionRows+1), b.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type header struct{ id, owner string }
	headers := []header{}
	for rows.Next() {
		var h header
		if err = rows.Scan(&h.id, &h.owner); err != nil {
			return err
		}
		headers = append(headers, h)
		if len(headers) > MaxOpaqueActionRows {
			return ErrBudget
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, h := range headers {
		for _, fid := range ids {
			if err = g.check(ctx, "data.field.read", Resource{EntityID: e.ID, FieldID: fid, RecordID: h.id, OwnerID: h.owner}); err != nil {
				return err
			}
		}
	}
	return nil
}
func projectActions(ctx context.Context, q postgres.Executor, st Store, e, target compiler.Entity, query Query, readIDs []string, where, indexJoin, joinSQL, sortExpr, order string, b builder, g Guard) (fieldActions, error) {
	d := fieldActions{}
	cols := `NULL::text,NULL::text`
	if query.Join != nil {
		cols = `j.id::text,j.owner_user_id::text`
	}
	rows, err := q.QueryContext(ctx, `SELECT r.id::text,r.owner_user_id::text,`+cols+` FROM `+st.recordsTable()+` r`+indexJoin+joinSQL+` WHERE `+where+` ORDER BY `+sortExpr+` `+order+` NULLS LAST,r.id ASC LIMIT `+strconv.Itoa(query.Limit+1), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Close result rows before guards, which may use this same transaction.
	type header struct {
		id, owner   string
		jid, jowner sql.NullString
	}
	headers := []header{}
	for rows.Next() {
		var h header
		if err = rows.Scan(&h.id, &h.owner, &h.jid, &h.jowner); err != nil {
			return nil, err
		}
		headers = append(headers, h)
		if len(headers) > MaxOpaqueActionRows {
			return nil, ErrBudget
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	admit := func(entity compiler.Entity, ids []string, id, owner string) error {
		resource := Resource{EntityID: entity.ID, RecordID: id, OwnerID: owner}
		if er := g.check(ctx, "data.row.read", resource); er != nil {
			return er
		}
		rowWrite := g.check(ctx, "data.row.write", resource)
		if rowWrite != nil && !errors.Is(rowWrite, ErrDenied) {
			return rowWrite
		}
		for _, fid := range ids {
			resource.FieldID = fid
			er := g.check(ctx, "data.field.read", resource)
			if er != nil {
				if errors.Is(er, ErrDenied) {
					continue
				}
				return er
			}
			d.allow(entity.ID, fid, "data.read", id)
			if rowWrite != nil {
				continue
			}
			f, _ := field(entity, fid)
			if f.CoreID {
				continue
			}
			er = g.check(ctx, "data.field.write", resource)
			if er != nil {
				if errors.Is(er, ErrDenied) {
					continue
				}
				return er
			}
			d.allow(entity.ID, fid, "data.write", id)
		}
		return nil
	}
	for _, h := range headers {
		if err = admit(e, readIDs, h.id, h.owner); err != nil {
			return nil, err
		}
		if query.Join != nil && h.jid.Valid {
			if err = admit(target, query.Join.Projection, h.jid.String, h.jowner.String); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
