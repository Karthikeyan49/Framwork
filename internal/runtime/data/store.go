package data

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type Store struct{ legacy, workerSource bool }

func (Store) Ready(ctx context.Context, db *sql.DB) error {
	var valid bool
	err := db.QueryRowContext(ctx, `SELECT count(*)=4 AND bool_and(relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole) FROM pg_catalog.pg_class WHERE oid IN ('runtime.record_sets'::regclass,'runtime.records'::regclass,'runtime.datasets'::regclass,'runtime.dataset_bindings'::regclass)`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	err = db.QueryRowContext(ctx, `SELECT count(*)=3 AND bool_and(tgenabled='O') FROM pg_catalog.pg_trigger WHERE (tgrelid='runtime.records'::regclass AND tgname='dataset_record_fence') OR (tgrelid='runtime.datasets'::regclass AND tgname='dataset_update_guard') OR (tgrelid='runtime.dataset_bindings'::regclass AND tgname='immutable_dataset_binding')`).Scan(&valid)
	if err != nil || !valid {
		return ErrUnavailable
	}
	_, err = db.ExecContext(ctx, `SELECT d.mode,d.write_version,r.custom_data,r.row_version,r.created_by FROM runtime.record_sets d JOIN runtime.records r USING(tenant_id,app_id,metadata_revision,ir_hash) LIMIT 0`)
	return err
}
func base(s Scope, a tenant.Access, entity string) []any {
	return []any{a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity}
}

const recordWhere = `tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5`

func plan(s Scope, a tenant.Access, id string) (compiler.Entity, error) {
	if !s.program.Valid() || a.TenantID() == "" || a.ActorID() == "" {
		return compiler.Entity{}, ErrDenied
	}
	e, ok := s.program.Entity(id)
	if !ok {
		return compiler.Entity{}, ErrInvalid
	}
	return e, nil
}
func newID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
func readValues(e compiler.Entity, r *Record, raw []byte) error {
	value, err := metadata.DecodeJSON(raw)
	if err != nil {
		return ErrUnavailable
	}
	v, ok := value.(map[string]any)
	if !ok {
		return ErrUnavailable
	}
	r.Values, err = values(e, v, r.ID, false)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func current(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e compiler.Entity, id string, resolve ...ScopeResolver) (Record, error) {
	var r Record
	var raw []byte
	args := append(base(s, a, e.ID), id)
	b := builder{args: args}
	accessScope, err := recordScope(ctx, resolve, e, "data.read")
	if err != nil {
		return r, err
	}
	nodes := 0
	predicate, err := scopeSQL(ctx, e, accessScope, a, "r", &b, 0, &nodes)
	if err != nil {
		return r, err
	}
	err = q.QueryRowContext(ctx, `SELECT id::text,row_version,created_by::text,created_at,updated_at,custom_data,owner_user_id::text,COALESCE(owner_team_id::text,'') FROM runtime.records r WHERE `+recordWhere+` AND id=$6 AND (`+predicate+`)`, b.args...).Scan(&r.ID, &r.Version, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &raw, &r.OwnerID, &r.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = readValues(e, &r, raw)
	return r, err
}

// A dataset counter write serializes all preview commands and rejects stale
// repeatable-read snapshots. Reference checks/deletes cannot race each other.
func lockDataset(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access) error {
	if err := lockBinding(ctx, q, s, a); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash) DO UPDATE SET write_version=runtime.record_sets.write_version+1`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash())
	return err
}
func (Store) Command(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, c Command, g Guard, resolve ...ScopeResolver) (Record, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return Record{}, err
	}
	ctx, span := otel.Tracer("framework/data").Start(ctx, "data.command")
	defer span.End()
	span.SetAttributes(attribute.String("entity_id", entity), attribute.String("ir_hash", s.Hash()), attribute.Int64("metadata_version", s.Revision()), attribute.String("data_mode", "preview"))
	if err = g.check(ctx, "data.entity.write", Resource{EntityID: entity}); err != nil {
		return Record{}, err
	}
	if c.Operation != "create" && c.Operation != "update" && c.Operation != "delete" {
		return Record{}, ErrInvalid
	}
	var existing Record
	if c.Operation == "create" {
		if c.ID != "" || c.ExpectedVersion != 0 {
			return Record{}, ErrInvalid
		}
		c.ID, err = newID()
		if err != nil {
			return Record{}, err
		}
	} else {
		if !tenant.ValidID(c.ID) || c.ExpectedVersion < 1 || c.ExpectedVersion >= 9007199254740991 {
			return Record{}, ErrInvalid
		}
		existing, err = current(ctx, q, s, a, e, c.ID, resolve...)
		if err != nil {
			return Record{}, err
		}
		if err = g.check(ctx, "data.row.write", Resource{EntityID: entity, RecordID: c.ID, OwnerID: existing.OwnerID}); err != nil {
			return Record{}, err
		}
		if er := authorizeExistingWrite(ctx, q, s, a, e, existing, resolve); er != nil {
			return Record{}, er
		}
		if existing.Version != c.ExpectedVersion {
			return Record{}, ErrConflict
		}
	}
	owner := a.ActorID()
	if existing.ID != "" {
		owner = existing.OwnerID
	}
	if err = g.check(ctx, "data.entity.read", Resource{EntityID: entity}); err != nil {
		return Record{}, err
	}
	if c.Operation == "create" {
		if err = g.check(ctx, "data.row.write", Resource{EntityID: entity, RecordID: c.ID, OwnerID: owner}); err != nil {
			return Record{}, err
		}
	}
	if err = g.check(ctx, "data.row.read", Resource{EntityID: entity, RecordID: c.ID, OwnerID: owner}); err != nil {
		return Record{}, err
	}
	for _, f := range e.Fields {
		if err = g.check(ctx, "data.field.read", Resource{EntityID: entity, FieldID: f.ID, RecordID: c.ID, OwnerID: owner}); err != nil {
			return Record{}, err
		}
	}
	if c.Operation == "delete" && len(c.Values) > 0 {
		return Record{}, ErrInvalid
	}
	for _, f := range e.Fields {
		if !f.CoreID {
			if err = g.check(ctx, "data.field.write", Resource{EntityID: entity, FieldID: f.ID, RecordID: c.ID, OwnerID: owner}); err != nil {
				return Record{}, err
			}
		}
	}
	var normalized map[string]any
	if c.Operation != "delete" {
		normalized, err = values(e, c.Values, c.ID, true)
		if err != nil {
			return Record{}, err
		}
	}
	if err = lockDataset(ctx, q, s, a); err != nil {
		return Record{}, err
	}
	if c.Operation == "delete" {
		for _, target := range s.program.IR().Entities {
			for _, f := range target.Fields {
				if f.Type != "reference" || f.ReferenceTo != entity {
					continue
				}
				args := append(base(s, a, target.ID), f.ID, c.ID)
				var used bool
				if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime.records WHERE `+recordWhere+` AND custom_data->>$6=$7)`, args...).Scan(&used); err != nil {
					return Record{}, err
				}
				if used {
					return Record{}, ErrConflict
				}
			}
		}
		args := append(base(s, a, entity), c.ID, c.ExpectedVersion)
		b := builder{args: args}
		writeScope, er := recordScope(ctx, resolve, e, "data.write")
		if er != nil {
			return Record{}, er
		}
		nodes := 0
		pred, er := scopeSQL(ctx, e, writeScope, a, "r", &b, 0, &nodes)
		if er != nil {
			return Record{}, er
		}
		result, err := q.ExecContext(ctx, `DELETE FROM runtime.records r WHERE `+recordWhere+` AND id=$6 AND row_version=$7 AND (`+pred+`)`, b.args...)
		if err != nil {
			return Record{}, err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return Record{}, ErrConflict
		}
		return existing, nil
	}
	for _, f := range e.Fields {
		if f.Type != "reference" || normalized[f.ID] == nil {
			continue
		}
		target, ok := s.program.Entity(f.ReferenceTo)
		if !ok {
			return Record{}, ErrInvalid
		}
		if err = g.check(ctx, "data.entity.read", Resource{EntityID: target.ID}); err != nil {
			return Record{}, err
		}
		referenced, err := current(ctx, q, s, a, target, normalized[f.ID].(string), resolve...)
		if errors.Is(err, ErrNotFound) {
			return Record{}, ErrInvalid
		}
		if err != nil {
			return Record{}, err
		}
		if err = g.check(ctx, "data.row.read", Resource{EntityID: target.ID, RecordID: referenced.ID, OwnerID: referenced.OwnerID}); err != nil {
			return Record{}, err
		}
	}
	// Core identifier is a typed database column, never duplicated into JSONB.
	for _, f := range e.Fields {
		if f.CoreID {
			delete(normalized, f.ID)
		}
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return Record{}, err
	}
	args := append(base(s, a, entity), c.ID, string(raw))
	// Authorize the proposed state before any business mutation, including CREATE.
	// This SQL candidate row is bounded and never contains caller-provided SQL.
	writeScope, er := resolveScope(ctx, resolve, "data.write", Resource{EntityID: entity})
	if er != nil {
		return Record{}, er
	}
	cb := builder{args: []any{a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, c.ID, owner, string(raw), existing.TeamID}}
	n := 0
	wp, er := scopeSQL(ctx, e, writeScope, a, "r", &cb, 0, &n)
	if er != nil {
		return Record{}, er
	}
	readScope, er := recordScope(ctx, resolve, e, "data.read")
	if er != nil {
		return Record{}, er
	}
	n = 0
	rp, er := scopeSQL(ctx, e, readScope, a, "r", &cb, 0, &n)
	if er != nil {
		return Record{}, er
	}
	var permitted bool
	er = q.QueryRowContext(ctx, `SELECT COALESCE((`+wp+`) AND (`+rp+`),false) FROM (SELECT $1::uuid tenant_id,$2::text app_id,$3::bigint metadata_revision,$4::text ir_hash,$5::text entity_id,$6::uuid id,$7::uuid owner_user_id,NULLIF($9::text,'')::uuid owner_team_id,$8::jsonb custom_data) r`, cb.args...).Scan(&permitted)
	if er != nil {
		return Record{}, er
	}
	if !permitted {
		return Record{}, ErrDenied
	}
	if c.Operation == "create" {
		_, err = q.ExecContext(ctx, `INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, args...)
	} else {
		args = append(args, c.ExpectedVersion)
		b := builder{args: args}
		n := 0
		pred, er := scopeSQL(ctx, e, writeScope, a, "r", &b, 0, &n)
		if er != nil {
			return Record{}, er
		}
		var result sql.Result
		result, err = q.ExecContext(ctx, `UPDATE runtime.records r SET custom_data=$7::jsonb,row_version=row_version+1,updated_at=clock_timestamp(),updated_by=iam.authenticated_actor() WHERE `+recordWhere+` AND id=$6 AND row_version=$8 AND (`+pred+`)`, b.args...)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				err = ErrConflict
			}
		}
	}
	if err != nil {
		return Record{}, err
	}
	// Reference projections share the same transaction and tenant-inclusive FKs.
	if _, err = q.ExecContext(ctx, "DELETE FROM runtime.record_links WHERE "+recordWhere+" AND id=$6", append(base(s, a, entity), c.ID)...); err != nil {
		return Record{}, err
	}
	for _, f := range e.Fields {
		if f.Type == "reference" && normalized[f.ID] != nil {
			args := append(base(s, a, entity), c.ID, f.ID, f.ReferenceTo, normalized[f.ID])
			if _, err = q.ExecContext(ctx, "INSERT INTO runtime.record_links(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,field_id,target_entity,target_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", args...); err != nil {
				return Record{}, err
			}
		}
	}
	r, err := current(ctx, q, s, a, e, c.ID, resolve...)
	if err != nil {
		return Record{}, err
	}

	return r, nil
}
