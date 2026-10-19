// Package metadata owns validated authoring definitions and immutable source revisions.
package metadata

import (
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/erp"
)

const SchemaVersion = "1.0.0"
const RuleSchemaVersion = "1.1.0"
const WorkflowSchemaVersion = "1.2.0"

// ToolWorkflowSchemaVersion is the opt-in ADR100 authoring contract: identical
// to WorkflowSchemaVersion except that an activity may reference a tool.
const ToolWorkflowSchemaVersion = "1.3.0"

// WorkflowSchema reports the authoring versions with rule/workflow V2 (DSL2)
// semantics. 1.3.0 behaves exactly like 1.2.0 except for tool activities.
func WorkflowSchema(v string) bool {
	return v == WorkflowSchemaVersion || v == ToolWorkflowSchemaVersion
}

const MaxBytes = 256 << 10

type Document struct {
	SchemaVersion string       `json:"schema_version"`
	AppID         string       `json:"app_id"`
	Namespace     string       `json:"namespace"`
	BaseVersion   *string      `json:"base_version"`
	Definitions   []Definition `json:"definitions"`
}
type Definition struct {
	ID            string        `json:"id"`
	SchemaVersion string        `json:"schema_version"`
	Namespace     string        `json:"namespace"`
	Label         string        `json:"label"`
	Kind          string        `json:"kind"`
	Entity        *Entity       `json:"entity,omitempty"`
	Field         *Field        `json:"field,omitempty"`
	Relationship  *Relationship `json:"relationship,omitempty"`
	Page          *Page         `json:"page,omitempty"`
	Rule          *Rule         `json:"rule,omitempty"`
	Workflow      *Workflow     `json:"workflow,omitempty"`
	Tool          *Tool         `json:"tool,omitempty"`
}
type Entity struct {
	Fields []string `json:"fields"`
}
type Field struct {
	EntityRef   string   `json:"entity_ref"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	EnumValues  []string `json:"enum_values,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	ReferenceTo string   `json:"reference_to,omitempty"`
}
type Relationship struct {
	FromEntity  string `json:"from_entity"`
	ToEntity    string `json:"to_entity"`
	FromField   string `json:"from_field"`
	ToField     string `json:"to_field"`
	Cardinality string `json:"cardinality"`
}
type Page struct {
	EntityRef  string      `json:"entity_ref"`
	Components []Component `json:"components"`
}
type Component struct {
	Type      string      `json:"type"`
	Label     string      `json:"label,omitempty"`
	FieldRef  string      `json:"field_ref,omitempty"`
	EntityRef string      `json:"entity_ref,omitempty"`
	Children  []Component `json:"children,omitempty"`
}
type Expression struct {
	Op        string          `json:"op"`
	Value     json.RawMessage `json:"value,omitempty"`
	FieldRef  string          `json:"field_ref,omitempty"`
	Args      []Expression    `json:"args,omitempty"`
	ValueType string          `json:"value_type,omitempty"`
}
type Rule struct {
	EntityRef  string                 `json:"entity_ref"`
	ResultType string                 `json:"result_type"`
	Expression Expression             `json:"expression"`
	DSLVersion string                 `json:"dsl_version,omitempty"`
	Calendars  []erp.BusinessCalendar `json:"calendars,omitempty"`
}
type Retry struct {
	MaxAttempts      int `json:"max_attempts"`
	InitialBackoffMS int `json:"initial_backoff_ms"`
	MaxBackoffMS     int `json:"max_backoff_ms"`
}

// Activity is a local or (schema 1.3.0) tool activity. Every published schema
// requires timeout_ms >= 1 for local/legacy activities, so omitting the zero
// value never changes the bytes of an existing valid document or plan; 1.3.0
// tool activities have no timeout (the trusted catalog bounds it). Inputs maps
// a tool input name to a field of the workflow entity.
type Activity struct {
	ToolRef              string            `json:"tool_ref,omitempty"`
	LocalRef             string            `json:"local_ref,omitempty"`
	Version              string            `json:"version,omitempty"`
	Inputs               map[string]string `json:"inputs,omitempty"`
	TimeoutMS            int               `json:"timeout_ms,omitempty"`
	Retry                Retry             `json:"retry"`
	CompensationToolRef  string            `json:"compensation_tool_ref,omitempty"`
	CompensationLocalRef string            `json:"compensation_local_ref,omitempty"`
}
type Transition struct {
	To        string      `json:"to"`
	On        string      `json:"on,omitempty"`
	Condition *Expression `json:"condition,omitempty"`
}
type WorkflowState struct {
	ID                string       `json:"id"`
	Type              string       `json:"type"`
	Transitions       []Transition `json:"transitions"`
	Activity          *Activity    `json:"activity,omitempty"`
	ApprovalRoles     []string     `json:"approval_roles,omitempty"`
	TimerMS           int          `json:"timer_ms,omitempty"`
	ApprovalTimeoutMS int          `json:"approval_timeout_ms,omitempty"`
}
type Workflow struct {
	DSLVersion   string          `json:"dsl_version,omitempty"`
	Version      string          `json:"version,omitempty"`
	TimeoutMS    int             `json:"timeout_ms,omitempty"`
	EntityRef    string          `json:"entity_ref"`
	Trigger      string          `json:"trigger"`
	InitialState string          `json:"initial_state"`
	States       []WorkflowState `json:"states"`
}

// ValueSchema is a bounded local JSON Schema subset for future tool payloads.
// It has no remote reference, arbitrary keyword, default credential or executable value.
type ValueSchema struct {
	Type                 string                  `json:"type"`
	Properties           *map[string]ValueSchema `json:"properties,omitempty"`
	Required             *[]string               `json:"required,omitempty"`
	AdditionalProperties *bool                   `json:"additionalProperties,omitempty"`
	Items                *ValueSchema            `json:"items,omitempty"`
	MaxLength            *int                    `json:"maxLength,omitempty"`
	MaxItems             *int                    `json:"maxItems,omitempty"`
}
type Tool struct {
	InputSchema  ValueSchema `json:"input_schema"`
	OutputSchema ValueSchema `json:"output_schema"`
	Permissions  []string    `json:"permissions"`
	SideEffect   string      `json:"side_effect"`
	SandboxMode  string      `json:"sandbox_mode"`
	Approval     string      `json:"approval"`
	TimeoutMS    int         `json:"timeout_ms"`
	Retry        Retry       `json:"retry"`
	Audit        string      `json:"audit"`
	TenantScope  string      `json:"tenant_scope"`
}
