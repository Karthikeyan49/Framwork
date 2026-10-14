package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var ErrCommandConflict = errors.New("command key already identifies a different request")

// Receipt exposes only the requesting actor's committed command facts, never
// customer values, current record visibility, or a claim that UNKNOWN aborted.
type Receipt struct {
	State     string `json:"state"`
	Key       string `json:"command_key"`
	EntityID  string `json:"entity_id,omitempty"`
	Operation string `json:"operation,omitempty"`
	RecordID  string `json:"record_id,omitempty"`
	Version   int64  `json:"record_version,omitempty"`
}

func CommandReceipt(ctx context.Context, q postgres.Executor, a tenant.Access, app, key string) (Receipt, error) {
	r := Receipt{State: "UNKNOWN", Key: key}
	if !tenant.ValidID(key) || a.ActorID() == "" || a.TenantID() == "" {
		return r, ErrInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT entity_id,operation,record_id::text,record_version FROM runtime.command_receipts WHERE tenant_id=$1 AND actor_id=$2 AND app_id=$3 AND command_key=$4`, a.TenantID(), a.ActorID(), app, key).Scan(&r.EntityID, &r.Operation, &r.RecordID, &r.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err == nil {
		r.State = "COMMITTED"
	}
	return r, err
}

func commandFingerprint(s Scope, entity string, c Command) (string, error) {
	raw, err := json.Marshal(struct {
		Command                                    Command
		Entity, ExecutionHash, AnchorHash, Dataset string
		Revision, AnchorRevision, Binding          int64
	}{c, entity, s.Hash(), s.DataHash(), s.DatasetID(), s.Revision(), s.DataRevision(), s.BindingVersion()})
	if err != nil || len(raw) > MaxRecordBytes+2048 {
		return "", ErrInvalid
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func receiptedCommand(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, c Command, g Guard, resolve ...ScopeResolver) (Record, error) {
	if !tenant.ValidID(c.Key) {
		return Record{}, ErrInvalid
	}
	if _, err := plan(s, a, entity); err != nil {
		return Record{}, err
	}
	if err := g.check(ctx, "data.entity.write", Resource{EntityID: entity}); err != nil {
		return Record{}, err
	}
	if err := g.check(ctx, "data.entity.read", Resource{EntityID: entity}); err != nil {
		return Record{}, err
	}
	fingerprint, err := commandFingerprint(s, entity, c)
	if err != nil {
		return Record{}, err
	}
	// Locks serialize this key only. They do not refresh an RR snapshot. If the
	// receipt remains invisible after waiting, its PK collision aborts all effects
	// and the outer API retries from a fresh authorized transaction.
	_, err = q.ExecContext(ctx, `SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(jsonb_build_array($1::uuid,$2::uuid,$3::text,$4::uuid)::text,0))`, a.TenantID(), a.ActorID(), s.AppID(), c.Key)
	if err != nil {
		return Record{}, err
	}
	var priorHash, id string
	var version int64
	err = q.QueryRowContext(ctx, `SELECT request_hash,record_id::text,record_version FROM runtime.command_receipts WHERE tenant_id=$1 AND actor_id=$2 AND app_id=$3 AND command_key=$4`, a.TenantID(), a.ActorID(), s.AppID(), c.Key).Scan(&priorHash, &id, &version)
	if err == nil {
		if priorHash != fingerprint {
			return Record{}, ErrCommandConflict
		}
		return Record{ID: id, Version: version, Values: map[string]any{}, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Record{}, err
	}
	r, err := command(ctx, q, s, a, entity, c, g, resolve...)
	if err != nil {
		return Record{}, err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO runtime.command_receipts(tenant_id,actor_id,app_id,command_key,request_hash,entity_id,operation,record_id,record_version,metadata_revision,ir_hash,data_revision,data_hash,dataset_id,binding_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14::text,'')::uuid,NULLIF($15::bigint,0))`, a.TenantID(), a.ActorID(), s.AppID(), c.Key, fingerprint, entity, c.Operation, r.ID, r.Version, s.Revision(), s.Hash(), s.DataRevision(), s.DataHash(), s.DatasetID(), s.BindingVersion())
	return r, err
}
