package data

import (
	"context"
	"errors"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// MaxRecordShares bounds explicit SHARE scope evaluation per record.
const MaxRecordShares = 64

// AccessCommand changes record ownership or one explicit share. It never
// carries business values; ExpectedVersion is the record conflict witness.
type AccessCommand struct {
	Operation       string `json:"operation"` // owner, share, unshare
	ExpectedVersion int64  `json:"expected_version"`
	UserID          string `json:"user_id"`
	TeamID          string `json:"team_id,omitempty"`
	CanWrite        *bool  `json:"can_write,omitempty"`
}
type Share struct {
	UserID   string `json:"user_id"`
	CanWrite bool   `json:"can_write"`
}
type Access struct {
	RecordID string  `json:"record_id"`
	Version  int64   `json:"version"`
	OwnerID  string  `json:"owner_user_id"`
	TeamID   string  `json:"owner_team_id,omitempty"`
	Shares   []Share `json:"shares,omitempty"`
}

func accessAction(op string) string {
	if op == "owner" {
		return "data.row.assign"
	}
	return "data.row.share"
}

func (c AccessCommand) valid() bool {
	if c.ExpectedVersion < 1 || c.ExpectedVersion >= 9007199254740991 || !tenant.ValidID(c.UserID) {
		return false
	}
	switch c.Operation {
	case "owner":
		return c.CanWrite == nil && (c.TeamID == "" || tenant.ValidID(c.TeamID))
	case "share":
		return c.CanWrite != nil && c.TeamID == ""
	case "unshare":
		return c.CanWrite == nil && c.TeamID == ""
	}
	return false
}

// authorizeAccess requires live read and write DataScope over the exact record
// plus the administrative row capability, before any lock or mutation.
func authorizeAccess(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id, action string, write bool, g Guard, resolve []ScopeResolver) (Record, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return Record{}, err
	}
	if !tenant.ValidID(id) {
		return Record{}, ErrInvalid
	}
	if err = g.check(ctx, "data.entity.read", Resource{EntityID: entity}); err != nil {
		return Record{}, err
	}
	if write {
		if err = g.check(ctx, "data.entity.write", Resource{EntityID: entity}); err != nil {
			return Record{}, err
		}
	}
	existing, err := current(ctx, q, s, a, e, id, resolve...)
	if err != nil {
		return Record{}, err
	}
	r := Resource{EntityID: entity, RecordID: id, OwnerID: existing.OwnerID}
	if err = g.check(ctx, "data.row.read", r); err != nil {
		return Record{}, err
	}
	if write {
		if err = g.check(ctx, "data.row.write", r); err != nil {
			return Record{}, err
		}
		if err = authorizeExistingWrite(ctx, q, s, a, e, existing, resolve); err != nil {
			return Record{}, err
		}
	}
	if err = g.check(ctx, action, r); err != nil {
		return Record{}, err
	}
	return existing, nil
}

// RecordAccess reads the owner and explicit shares of a visible record. Share
// recipients are only disclosed to actors holding data.row.share on it.
func (Store) RecordAccess(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, g Guard, resolve ...ScopeResolver) (Access, error) {
	existing, err := authorizeAccess(ctx, q, s, a, entity, id, "data.row.share", false, g, resolve)
	if err != nil {
		return Access{}, err
	}
	out := Access{RecordID: existing.ID, Version: existing.Version, OwnerID: existing.OwnerID, TeamID: existing.TeamID, Shares: []Share{}}
	rows, err := q.QueryContext(ctx, `SELECT user_id::text,can_write FROM runtime.read_shares WHERE `+recordWhere+` AND id=$6 ORDER BY user_id LIMIT 65`, append(base(s, a, entity), id)...)
	if err != nil {
		return Access{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var share Share
		if err = rows.Scan(&share.UserID, &share.CanWrite); err != nil {
			return Access{}, err
		}
		out.Shares = append(out.Shares, share)
	}
	if err = rows.Err(); err != nil {
		return Access{}, err
	}
	if len(out.Shares) > MaxRecordShares {
		return Access{}, ErrBudget
	}
	return out, nil
}

// AdministerAccess applies one access change in the caller's transaction. The
// SQL port re-checks the exact version, recipient membership/team, share
// budget and dataset fence, and advances the record version as the witness.
// The result reports the committed witness only: after an ownership transfer
// the actor may no longer be authorized to read the record.
func (Store) AdministerAccess(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, c AccessCommand, g Guard, resolve ...ScopeResolver) (Access, error) {
	if !c.valid() {
		return Access{}, ErrInvalid
	}
	ctx, span := otel.Tracer("framework/data").Start(ctx, "data.access")
	defer span.End()
	span.SetAttributes(attribute.String("entity_id", entity), attribute.String("ir_hash", s.Hash()), attribute.String("access_operation", c.Operation))
	existing, err := authorizeAccess(ctx, q, s, a, entity, id, accessAction(c.Operation), true, g, resolve)
	if err != nil {
		return Access{}, err
	}
	if existing.Version != c.ExpectedVersion {
		return Access{}, ErrConflict
	}
	if err = lockDataset(ctx, q, s, a); err != nil {
		return Access{}, err
	}
	var team, write any
	if c.TeamID != "" {
		team = c.TeamID
	}
	if c.CanWrite != nil {
		write = *c.CanWrite
	}
	var version int64
	err = q.QueryRowContext(ctx, `SELECT runtime.administer_record_access($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::uuid,$11::boolean)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, c.ExpectedVersion, c.Operation, c.UserID, team, write).Scan(&version)
	if err != nil {
		return Access{}, err
	}
	if version != c.ExpectedVersion+1 {
		return Access{}, errors.New("record access witness mismatch")
	}
	out := Access{RecordID: id, Version: version, OwnerID: existing.OwnerID, TeamID: existing.TeamID}
	if c.Operation == "owner" {
		out.OwnerID, out.TeamID = c.UserID, c.TeamID
	}
	return out, nil
}
