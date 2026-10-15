// Package audit appends bounded, secret-free security evidence.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

type State struct {
	PolicyRevision       int64  `json:"policy_revision,omitempty"`
	PolicyOperation      string `json:"policy_operation,omitempty"`
	PolicySubject        string `json:"policy_subject,omitempty"`
	EvolutionID          string `json:"evolution_id,omitempty"`
	EvolutionState       string `json:"evolution_state,omitempty"`
	EvolutionCheckpoint  int64  `json:"evolution_checkpoint,omitempty"`
	EvolutionPlanHash    string `json:"evolution_plan_hash,omitempty"`
	CommandKey           string `json:"command_key,omitempty"`
	CommandReplayed      bool   `json:"command_replayed,omitempty"`
	DatasetID            string `json:"dataset_id,omitempty"`
	DataSchemaHash       string `json:"data_schema_hash,omitempty"`
	DataSchemaGeneration int64  `json:"data_schema_generation,omitempty"`
	DatasetBinding       int64  `json:"dataset_binding,omitempty"`
	DatasetState         string `json:"dataset_state,omitempty"`
	VerifiedRows         int64  `json:"verified_rows,omitempty"`
	ManifestHash         string `json:"manifest_hash,omitempty"`
	WorkflowInstanceID   string `json:"workflow_instance_id,omitempty"`
	WorkflowID           string `json:"workflow_id,omitempty"`
	WorkflowVersion      string `json:"workflow_version,omitempty"`
	WorkflowPlanHash     string `json:"workflow_plan_hash,omitempty"`
	WorkflowSequence     int64  `json:"workflow_sequence,omitempty"`
	WorkflowStep         string `json:"workflow_step,omitempty"`
	EventID              string `json:"event_id,omitempty"`
	ConsumerID           string `json:"consumer_id,omitempty"`
	AggregateSequence    int64  `json:"aggregate_sequence,omitempty"`
	RuleID               string `json:"rule_id,omitempty"`
	RuleHash             string `json:"rule_hash,omitempty"`
	RuleVersion          string `json:"rule_version,omitempty"`
	ArtifactHash         string `json:"artifact_hash,omitempty"`
	ScopeHash            string `json:"scope_hash,omitempty"`
	IRHash               string `json:"ir_hash,omitempty"`
	EntityID             string `json:"entity_id,omitempty"`
	FieldID              string `json:"field_id,omitempty"`
	RecordID             string `json:"record_id,omitempty"`
	RecordVersion        int64  `json:"record_version,omitempty"`
	AccessOperation      string `json:"access_operation,omitempty"`
	AccessTeamID         string `json:"access_team_id,omitempty"`
	TeamVersion          int64  `json:"team_version,omitempty"`
	ToolRef              string `json:"tool_ref,omitempty"`
	ToolVersion          string `json:"tool_version,omitempty"`
	ToolMode             string `json:"tool_mode,omitempty"`
	ToolAttempt          int64  `json:"tool_attempt,omitempty"`
	ToolOutcome          string `json:"tool_outcome,omitempty"`
	MetadataApp          string `json:"metadata_app,omitempty"`
	MetadataRevision     int64  `json:"metadata_revision,omitempty"`
	MetadataHash         string `json:"metadata_hash,omitempty"`
	OperatorRef          string `json:"operator_ref,omitempty"`
	Role                 string `json:"role,omitempty"`
	UserActive           *bool  `json:"user_active,omitempty"`
	MembershipActive     *bool  `json:"membership_active,omitempty"`
}
type Entry struct {
	ActorID, TenantID, Action, Resource, DecisionID, PolicyVersion, RequestID, TraceID string
	Allow                                                                              bool
	State                                                                              State
}
type Writer interface {
	Append(context.Context, postgres.Executor, Entry) error
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

func (Postgres) Append(ctx context.Context, tx postgres.Executor, e Entry) error {
	args, err := encodeEntry(e)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, insertEntry+`(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,$9,$10)`, args...)
	return err
}

const insertEntry = `INSERT INTO audit.entries(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state) VALUES`

func encodeEntry(e Entry) ([]any, error) {
	if e.State.PolicyRevision < 0 || e.State.PolicyRevision > 1e9 || len(e.State.PolicyOperation) > 36 || len(e.State.PolicySubject) > 36 {
		return nil, errors.New("invalid policy evidence")
	}
	if (e.State.AccessOperation != "" && e.State.AccessOperation != "owner" && e.State.AccessOperation != "share" && e.State.AccessOperation != "unshare") || len(e.State.AccessTeamID) > 36 || e.State.TeamVersion < 0 || e.State.TeamVersion > 1e9 {
		return nil, errors.New("invalid record access evidence")
	}
	if len(e.State.ToolRef) > 160 || len(e.State.ToolVersion) > 20 || len(e.State.ToolMode) > 16 || e.State.ToolAttempt < 0 || e.State.ToolAttempt > 20 || len(e.State.ToolOutcome) > 80 {
		return nil, errors.New("invalid tool evidence")
	}
	if len(e.State.CommandKey) > 36 {
		return nil, errors.New("invalid command evidence")
	}
	if len(e.State.EvolutionID) > 36 || len(e.State.EvolutionState) > 16 || e.State.EvolutionCheckpoint < 0 || len(e.State.EvolutionPlanHash) > 64 {
		return nil, errors.New("invalid evolution evidence")
	}
	if len(e.State.DatasetID) > 36 || len(e.State.DataSchemaHash) > 64 || e.State.DataSchemaGeneration < 0 || e.State.DatasetBinding < 0 || e.State.DatasetBinding > 1e9 || e.State.VerifiedRows < 0 || len(e.State.ManifestHash) > 64 || (e.State.DatasetState != "" && e.State.DatasetState != "OPEN" && e.State.DatasetState != "ADOPTING" && e.State.DatasetState != "FROZEN") {
		return nil, errors.New("invalid dataset evidence")
	}
	if len(e.State.WorkflowInstanceID) > 36 || len(e.State.WorkflowID) > 160 || len(e.State.WorkflowVersion) > 20 || len(e.State.WorkflowPlanHash) > 64 || e.State.WorkflowSequence < 0 || len(e.State.WorkflowStep) > 200 {
		return nil, errors.New("invalid workflow evidence")
	}
	if len(e.State.EventID) > 36 || len(e.State.ConsumerID) > 80 || e.State.AggregateSequence < 0 {
		return nil, errors.New("invalid event evidence")
	}
	if len(e.State.RuleID) > 160 || len(e.State.RuleHash) > 64 || len(e.State.RuleVersion) > 20 {
		return nil, errors.New("invalid rule evidence")
	}
	if e.Action == "" || len(e.Action) > 80 || len(e.Resource) > 80 || len(e.DecisionID) > 64 || len(e.RequestID) > 64 || len(e.TraceID) > 32 || len(e.PolicyVersion) > 80 {
		return nil, errors.New("invalid audit evidence")
	}
	if len(e.State.ArtifactHash) > 64 || len(e.State.ScopeHash) > 64 || len(e.State.IRHash) > 64 || len(e.State.EntityID) > 160 || len(e.State.FieldID) > 160 || len(e.State.RecordID) > 36 || e.State.RecordVersion < 0 || e.State.RecordVersion > 9007199254740991 || len(e.State.MetadataApp) > 160 || len(e.State.MetadataHash) > 64 || e.State.MetadataRevision < 0 || e.State.MetadataRevision > 1000000000 || len(e.State.OperatorRef) > 64 || len(e.State.Role) > 64 {
		return nil, errors.New("invalid audit state")
	}
	state, err := json.Marshal(e.State)
	if err != nil {
		return nil, err
	}
	return []any{e.ActorID, e.TenantID, e.Action, e.Resource, e.Allow, e.DecisionID, e.PolicyVersion, e.RequestID, e.TraceID, state}, nil
}
