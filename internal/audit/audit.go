// Package audit appends bounded, secret-free security evidence.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type State struct {
	OperatorRef      string `json:"operator_ref,omitempty"`
	Role             string `json:"role,omitempty"`
	UserActive       *bool  `json:"user_active,omitempty"`
	MembershipActive *bool  `json:"membership_active,omitempty"`
}
type Entry struct {
	ActorID, TenantID, Action, Resource, DecisionID, PolicyVersion, RequestID, TraceID string
	Allow                                                                              bool
	State                                                                              State
}
type Writer interface {
	Append(context.Context, *sql.Tx, Entry) error
}
type Postgres struct{}

// Ready checks the insert-only schema contract without reading evidence or
// writing a synthetic event. Missing migrations/column grants fail closed.
func (Postgres) Ready(ctx context.Context, db *sql.DB) error {
	var ready bool
	err := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=10 AND bool_and(has_column_privilege('audit.entries',attname,'INSERT'))
  FROM pg_attribute WHERE attrelid='audit.entries'::regclass AND NOT attisdropped
  AND attname IN ('actor_id','tenant_id','action','resource','allowed','decision_id','policy_version','request_id','trace_id','state'))
 AND EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='audit.entries'::regclass AND attname='writer_role' AND NOT attisdropped)
 AND EXISTS(SELECT 1 FROM pg_class WHERE oid='audit.entries'::regclass AND relrowsecurity AND relforcerowsecurity)`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("audit schema unavailable")
	}
	return nil
}

func (Postgres) Append(ctx context.Context, tx *sql.Tx, e Entry) error {
	if e.Action == "" || len(e.Action) > 80 || len(e.Resource) > 80 || len(e.DecisionID) > 64 || len(e.RequestID) > 64 || len(e.TraceID) > 32 || len(e.PolicyVersion) > 80 {
		return errors.New("invalid audit evidence")
	}
	if len(e.State.OperatorRef) > 64 || len(e.State.Role) > 20 {
		return errors.New("invalid audit state")
	}
	state, err := json.Marshal(e.State)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit.entries(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state) VALUES(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,$9,$10)`, e.ActorID, e.TenantID, e.Action, e.Resource, e.Allow, e.DecisionID, e.PolicyVersion, e.RequestID, e.TraceID, state)
	return err
}
