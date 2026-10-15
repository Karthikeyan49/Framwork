package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"time"
)

type DataPin struct {
	DatasetID      string `json:"dataset_id,omitempty"`
	BindingVersion int64  `json:"binding_version,omitempty"`
	DataGeneration int64  `json:"data_generation,omitempty"`
	DataRevision   int64  `json:"data_revision,omitempty"`
	DataHash       string `json:"data_hash,omitempty"`
}
type Pin struct {
	DataPin
	AppID           string          `json:"app_id"`
	Revision        int64           `json:"metadata_revision"`
	IRHash          string          `json:"ir_hash"`
	WorkflowID      string          `json:"workflow_id"`
	WorkflowVersion string          `json:"workflow_version"`
	PlanHash        string          `json:"plan_hash"`
	RecordID        string          `json:"record_id"`
	RecordVersion   int64           `json:"record_version"`
	IR              json.RawMessage `json:"-"`
	RequestID       string          `json:"-"`
	TraceParent     string          `json:"-"`
}
type Snapshot struct {
	Fields        []string                   `json:"fields"`
	Values        map[string]json.RawMessage `json:"values"`
	RecordVersion int64                      `json:"record_version"`
}

// RestoreRelease must verify stored application IR and workflow/source agreement,
// never compile source. Capture/Authorize enforce live policy and SQL DataScope.
type RestoreRelease func([]byte, string, string) (Program, error)
type Authorizer func(context.Context, postgres.Executor, tenant.Access, string, Pin, []string) (audit.Entry, error)
type Capture func(context.Context, postgres.Executor, tenant.Access, Pin, []string) (Snapshot, error)
type ActivityRequest struct {
	InstanceID, StepKey, ActivityRef, Version, RequestHash string
	Pin                                                    Pin
	Inputs                                                 Snapshot
	OriginalOutput                                         json.RawMessage
	Compensation                                           bool
}

// LocalActivity is trusted Go, not metadata code. Use the caller's transaction
// and explicit policy/data ports. Network/filesystem/SQL from metadata are forbidden.
type LocalActivity func(context.Context, postgres.Executor, tenant.Access, ActivityRequest) (json.RawMessage, error)
type Failure struct{ Kind string }

// AuthorizationDenied preserves bounded policy evidence across rollback. It
// contains no input/output body and cannot authorize any operation.
type AuthorizationDenied struct{ Evidence audit.Entry }

func (e *AuthorizationDenied) Error() string { return "workflow authorization denied" }
func (e *AuthorizationDenied) Unwrap() error { return ErrDenied }

func (f Failure) Error() string { return "local workflow activity failed" }
func Retryable() error          { return Failure{"retryable"} }
func Unknown() error            { return Failure{"unknown"} }
func Permanent() error          { return Failure{"permanent"} }
func failureKind(err error) string {
	var f Failure
	if errors.As(err, &f) && includes([]string{"retryable", "unknown", "permanent"}, f.Kind) {
		return f.Kind
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "retryable"
	}
	return "permanent"
}
func Noop(ctx context.Context, _ postgres.Executor, _ tenant.Access, r ActivityRequest) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if r.ActivityRef != "local.noop" || r.Version != ActivityVersion {
		return nil, Permanent()
	}
	return json.RawMessage(`{"completed":true}`), nil
}

type Instance struct {
	Mode         string       `json:"mode"`
	ID           string       `json:"id"`
	ActorID      string       `json:"actor_id"`
	Pin          Pin          `json:"pin"`
	StateID      string       `json:"state_id"`
	Status       string       `json:"status"`
	Version      int64        `json:"version"`
	Attempt      int          `json:"attempt"`
	EnteredAt    time.Time    `json:"entered_at"`
	DueAt        time.Time    `json:"due_at"`
	Deadline     time.Time    `json:"deadline"`
	Termination  string       `json:"termination,omitempty"`
	Failure      string       `json:"failure,omitempty"`
	RoutingState string       `json:"routing_state"`
	History      []History    `json:"history,omitempty"`
	Tools        []ToolStatus `json:"tools,omitempty"`
}
type ToolStatus struct {
	StepKey     string `json:"step_key"`
	ToolRef     string `json:"tool_ref"`
	ToolVersion string `json:"tool_version"`
	Mode        string `json:"mode"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	Class       string `json:"outcome_class,omitempty"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	Simulated   bool   `json:"simulated,omitempty"`
}
type History struct {
	Sequence   int64     `json:"sequence"`
	StateID    string    `json:"state_id"`
	Status     string    `json:"status"`
	Event      string    `json:"event"`
	ActorID    string    `json:"actor_id"`
	At         time.Time `json:"occurred_at"`
	StepKey    string    `json:"step_key,omitempty"`
	ResultHash string    `json:"result_hash,omitempty"`
}
type Start struct {
	DataPin
	IR                                                         []byte
	IRHash                                                     string
	Revision                                                   int64
	WorkflowID, RecordID, Key, RequestID, TraceParent, Trigger string
}
type Signal struct {
	Operation       string          `json:"operation"`
	ExpectedVersion int64           `json:"expected_version"`
	StateID         string          `json:"state_id"`
	Key             string          `json:"idempotency_key"`
	OperatorRef     string          `json:"operator_ref,omitempty"`
	Outcome         string          `json:"outcome,omitempty"`
	Output          json.RawMessage `json:"output,omitempty"`
}

// Durability is the business-facing port. The PG adapter owns no provider DSL.
type Durability interface {
	Start(context.Context, postgres.Executor, tenant.Access, Start) (Instance, error)
	Get(context.Context, postgres.Executor, tenant.Access, string) (Instance, error)
	Signal(context.Context, postgres.Executor, tenant.Access, string, Signal) (Instance, error)
}
type Store struct {
	Restore   RestoreRelease
	Authorize Authorizer
	Capture   Capture
	Audit     audit.Writer
	Events    events.LifecycleWriter
	Activity  LocalActivity
	// Tools executes governed external effects outside transactions (ADR036).
	// ToolCatalog is trusted operator configuration; plans pin its hashes.
	Tools       tools.Executor
	ToolCatalog tools.Catalog
	// ToolMode derives the effective mode from server-side pins; nil uses the
	// tool's declared non-production mode.
	ToolMode func(Pin, tools.Definition) string
}

func (s Store) configured() bool {
	return s.Restore != nil && s.Authorize != nil && s.Capture != nil && s.Audit != nil && s.Events != nil
}
func terminal(s string) bool {
	return includes([]string{"SUCCEEDED", "FAILED", "CANCELLED", "TIMED_OUT", "DEAD", "RECONCILE", "COMPENSATION_FAILED"}, s)
}
