package data

import (
	"context"
	"database/sql"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"sort"
)

// MaxCandidates bounds the unfiltered visibility proof. Larger datasets need a
// trusted policy-predicate adapter; silently filtering before ABAC is unsafe.
const MaxCandidates = 1000

func authorizeCandidates(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, fields map[string]bool, join *Join, target compiler.Entity, g Guard) error {
	args := base(s, a, entity)
	joinedSQL, columns := "", "NULL::text,NULL::text"
	if join != nil {
		var from string
		for _, d := range s.program.IR().Application.Definitions {
			if d.ID == join.Relationship {
				from = d.Relationship.FromField
			}
		}
		args = append(args, target.ID, from)
		joinedSQL = ` LEFT JOIN runtime.preview_records j ON j.tenant_id=r.tenant_id AND j.app_id=r.app_id AND j.metadata_revision=r.metadata_revision AND j.ir_hash=r.ir_hash AND j.entity_id=$6 AND j.id=(r.custom_data->>$7)::uuid`
		columns = "j.id::text,j.created_by::text"
	}
	rows, err := q.QueryContext(ctx, `SELECT r.id::text,r.created_by::text,`+columns+` FROM runtime.preview_records r`+joinedSQL+` WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND r.entity_id=$5 ORDER BY r.id LIMIT 1001`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	pending := []Resource{}
	keys := []string{}
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	count := 0
	for rows.Next() {
		var id, owner string
		var jid, jowner sql.NullString
		if err = rows.Scan(&id, &owner, &jid, &jowner); err != nil {
			return err
		}
		count++
		pending = append(pending, Resource{EntityID: entity, RecordID: id, OwnerID: owner})
		for _, key := range keys {
			pending = append(pending, Resource{EntityID: entity, FieldID: key, RecordID: id, OwnerID: owner})
		}
		if jid.Valid {
			pending = append(pending, Resource{EntityID: target.ID, RecordID: jid.String, OwnerID: jowner.String})
			for _, key := range join.Projection {
				pending = append(pending, Resource{EntityID: target.ID, FieldID: key, RecordID: jid.String, OwnerID: jowner.String})
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, r := range pending {
		action := "data.row.read"
		if r.FieldID != "" {
			action = "data.field.read"
		}
		if err = g.check(ctx, action, r); err != nil {
			return err
		}
	}
	if count > MaxCandidates {
		return ErrBudget
	}
	return nil
}
