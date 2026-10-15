package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Budget from already-readable values plus public worst-case reservations for
// hidden/unprojected fields. Never inspect hidden lengths to decide admission.
func visiblePatchBudget(e compiler.Entity, visible map[string]any, hidden []string) error {
	raw, _ := json.Marshal(visible)
	total := len(raw)
	if visible == nil {
		total = 2
	}
	for _, f := range e.Fields {
		if !slices.Contains(hidden, f.ID) {
			continue
		}
		key, _ := json.Marshal(f.ID)
		size := 0
		switch {
		case f.CoreID:
			size = 38 // intrinsic public ID, also counted by strict readers
		case f.Type == "string":
			size = 6*4096 + 2
		case f.Type == "enum":
			for _, v := range f.EnumValues {
				raw, _ := json.Marshal(v)
				if len(raw) > size {
					size = len(raw)
				}
			}
		case f.Type == "integer":
			size = 17
		case f.Type == "decimal":
			size = 66
		case f.Type == "money":
			size = 128
		case f.Type == "bool":
			size = 5
		case f.Type == "reference":
			size = 38
		case f.Type == "date":
			size = 12
		case f.Type == "datetime":
			size = 242
		default:
			return ErrInvalid
		}
		if size < 4 {
			size = 4
		}
		total += len(key) + 2 + size
	}
	if total > MaxRecordBytes {
		return ErrBudget
	}
	return nil
}
func patch(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, c Command, g Guard, resolve ...ScopeResolver) (Record, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return Record{}, err
	}
	if !tenant.ValidID(c.ID) || c.ExpectedVersion < 1 || c.ExpectedVersion >= 9007199254740991 || len(c.Values) == 0 || len(c.Values) > 32 {
		return Record{}, ErrInvalid
	}
	for _, action := range []string{"data.entity.read", "data.entity.write"} {
		if err = g.check(ctx, action, Resource{EntityID: entity}); err != nil {
			return Record{}, err
		}
	}
	keys := make([]string, 0, len(c.Values))
	for k := range c.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		f, ok := field(e, id)
		if !ok || f.CoreID {
			return Record{}, ErrInvalid
		}
	}
	b := builder{args: append(base(s, a, entity), c.ID)}
	where := recordWhere + " AND r.id=$6"
	for _, action := range []string{"data.read", "data.write"} {
		ids := append([]string{""}, keys...)
		for _, id := range ids {
			sc, er := resolveScope(ctx, resolve, action, Resource{EntityID: entity, FieldID: id})
			if er != nil {
				return Record{}, er
			}
			// Hidden business-state conditions must not be probed by write success.
			if hasCondition(sc) {
				return Record{}, ErrDenied
			}
			n := 0
			p, er := scopeSQL(ctx, e, sc, a, "r", &b, 0, &n)
			if er != nil {
				return Record{}, er
			}
			where += " AND (" + p + ")"
		}
	}
	var existing Record
	err = q.QueryRowContext(ctx, `SELECT r.id::text,r.row_version,r.owner_user_id::text FROM runtime.read_records r WHERE `+where, b.args...).Scan(&existing.ID, &existing.Version, &existing.OwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	for _, action := range []string{"data.row.read", "data.row.write"} {
		if err = g.check(ctx, action, Resource{EntityID: entity, RecordID: c.ID, OwnerID: existing.OwnerID}); err != nil {
			return Record{}, err
		}
	}
	for _, id := range keys {
		for _, action := range []string{"data.field.read", "data.field.write"} {
			if err = g.check(ctx, action, Resource{EntityID: entity, FieldID: id, RecordID: c.ID, OwnerID: existing.OwnerID}); err != nil {
				return Record{}, err
			}
		}
	}
	if existing.Version != c.ExpectedVersion {
		return Record{}, ErrConflict
	}
	normalized := map[string]any{}
	for _, id := range keys {
		f, _ := field(e, id)
		v, er := fieldValue(f, c.Values[id])
		if er != nil {
			return Record{}, er
		}
		normalized[id] = v
		if f.Type == "reference" && v != nil {
			target, ok := s.program.Entity(f.ReferenceTo)
			if !ok {
				return Record{}, ErrInvalid
			}
			// Only an entity-visible target is needed. Its other field values are not.
			var core string
			for _, tf := range target.Fields {
				if tf.CoreID {
					core = tf.ID
				}
			}
			page, er := (Store{}).QueryVisible(ctx, q, s, a, target.ID, Query{RecordID: v.(string), Projection: []string{core}, Limit: 1}, g, resolve...)
			if er != nil {
				return Record{}, er
			}
			if len(page.Records) != 1 {
				return Record{}, ErrInvalid
			}
		}
	}
	ids := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		ids = append(ids, f.ID)
	}
	before, err := (Store{}).QueryVisible(ctx, q, s, a, entity, Query{RecordID: c.ID, Projection: ids, Limit: 1}, g, resolve...)
	if err != nil {
		return Record{}, err
	}
	if len(before.Records) != 1 {
		return Record{}, ErrDenied
	}
	known := before.Records[0].Values
	for id, value := range normalized {
		known[id] = value
	}
	if err = visiblePatchBudget(e, known, before.Records[0].FieldAccess.Masked); err != nil {
		return Record{}, err
	}
	if err = lockDataset(ctx, q, s, a); err != nil {
		return Record{}, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return Record{}, err
	}
	payload := b.bind(string(raw)) + "::jsonb"
	version := b.bind(c.ExpectedVersion)
	// Recheck the same scopes in the UPDATE; only non-business-state predicates
	// are admitted. Both sides use the trusted namespace and transaction snapshot.
	result, err := q.ExecContext(ctx, `UPDATE runtime.records r SET custom_data=r.custom_data || `+payload+`,row_version=r.row_version+1,updated_at=clock_timestamp(),updated_by=iam.authenticated_actor() WHERE `+recordWhere+` AND r.id=$6 AND r.row_version=`+version+` AND EXISTS(SELECT 1 FROM runtime.read_records live WHERE live.tenant_id=r.tenant_id AND live.app_id=r.app_id AND live.metadata_revision=r.metadata_revision AND live.ir_hash=r.ir_hash AND live.entity_id=r.entity_id AND live.id=r.id AND (`+strings.ReplaceAll(where, "r.", "live.")+`))`, b.args...)
	if err != nil {
		return Record{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Record{}, err
	}
	if n != 1 {
		return Record{}, ErrConflict
	}
	// Touch only changed reference projections, including explicit-null removal.
	for _, id := range keys {
		f, _ := field(e, id)
		if f.Type != "reference" {
			continue
		}
		args := append(base(s, a, entity), c.ID, id)
		if _, err = q.ExecContext(ctx, `DELETE FROM runtime.record_links WHERE `+recordWhere+` AND id=$6 AND field_id=$7`, args...); err != nil {
			return Record{}, err
		}
		if v := normalized[id]; v != nil {
			args = append(args, f.ReferenceTo, v)
			if _, err = q.ExecContext(ctx, `INSERT INTO runtime.record_links(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,field_id,target_entity,target_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, args...); err != nil {
				return Record{}, err
			}
		}
	}
	page, err := (Store{}).QueryVisible(ctx, q, s, a, entity, Query{RecordID: c.ID, Projection: ids, Limit: 1}, g, resolve...)
	if err != nil {
		return Record{}, err
	}
	if len(page.Records) != 1 {
		return Record{}, ErrDenied
	}
	return page.Records[0], nil
}
