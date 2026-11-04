package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// NumericIndex describes a governed physical plan, never a metadata uniqueness
// guarantee. Customer values are not returned through the management port.
type NumericIndex struct {
	FieldID      string `json:"field_id"`
	SchemaHash   string `json:"schema_hash"`
	State        string `json:"state"`
	Checkpoint   int64  `json:"checkpoint"`
	ScannedRows  int64  `json:"scanned_rows"`
	VerifiedRows int64  `json:"verified_rows"`
}

func indexField(s Scope, a tenant.Access, entity, id string) (compiler.Field, string, error) {
	e, err := plan(s, a, entity)
	if err != nil {
		return compiler.Field{}, "", err
	}
	f, ok := field(e, id)
	if !ok || f.CoreID || (f.Type != "integer" && f.Type != "decimal" && f.Type != "money") {
		return compiler.Field{}, "", ErrInvalid
	}
	_, schema, err := compiler.Storage(s.Program())
	if err != nil {
		return compiler.Field{}, "", ErrInvalid
	}
	return f, schema, nil
}

func numericIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (NumericIndex, error) {
	f, schema, err := indexField(s, a, entity, id)
	if err != nil {
		return NumericIndex{}, err
	}
	var index NumericIndex
	var typ, currency string
	var required bool
	args := append(base(s, a, entity), id)
	err = q.QueryRowContext(ctx, `SELECT field_id,schema_hash,state,checkpoint,scanned_rows,verified_rows,field_type,currency,required FROM runtime.numeric_indexes WHERE `+recordWhere+` AND field_id=$6`, args...).Scan(&index.FieldID, &index.SchemaHash, &index.State, &index.Checkpoint, &index.ScannedRows, &index.VerifiedRows, &typ, &currency, &required)
	if errors.Is(err, sql.ErrNoRows) {
		return NumericIndex{}, ErrNotFound
	}
	if err != nil {
		return NumericIndex{}, err
	}
	if index.SchemaHash != schema || typ != f.Type || currency != f.Currency || required != f.Required {
		return NumericIndex{}, ErrUnavailable
	}
	return index, nil
}

func (Store) NumericIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (NumericIndex, error) {
	return numericIndex(ctx, q, s, a, entity, id)
}

// Register/Build require the central management decision at the API and an
// independent live owner/admin check in PostgreSQL. Database-owned routines
// derive types from immutable source and maintain every projection atomically.
func (Store) RegisterNumericIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string) (NumericIndex, error) {
	_, schema, err := indexField(s, a, entity, id)
	if err != nil {
		return NumericIndex{}, err
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return NumericIndex{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.register_numeric_index($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, schema, s.Revision(), s.Hash())
	if err != nil {
		return NumericIndex{}, err
	}
	return numericIndex(ctx, q, s, a, entity, id)
}

func (Store) BuildNumericIndex(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity, id string, expected int64) (NumericIndex, error) {
	_, schema, err := indexField(s, a, entity, id)
	if err != nil || expected < 1 || expected > 1e12 {
		return NumericIndex{}, ErrInvalid
	}
	if err = lockBinding(ctx, q, s, a); err != nil {
		return NumericIndex{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.build_numeric_index($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash(), entity, id, schema, s.Revision(), s.Hash(), expected)
	if err != nil {
		return NumericIndex{}, err
	}
	return numericIndex(ctx, q, s, a, entity, id)
}
