// Package ai is the model gateway, the semantic context builder and the
// proposal pipeline of ADR104. A model holds no authority and has no identity:
// everything it returns is parsed into a closed, typed, hashed proposal that an
// authenticated human applies or rejects through an ordinary endpoint.
//
// This file is the shared contract between the pure pipeline (gateway, providers,
// context, schemas, repair loop, apply) and the persistence/API layer (store,
// ports, routes). Change it only through /home/user/m14-env/CONTRACT_CHANGES.md.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Versions of the closed vocabularies. A change to a schema bumps its version.
const (
	PatchProposalVersion       = "ai-patch-proposal-v1"
	RequirementProposalVersion = "ai-requirement-proposal-v1"
	ProviderConfigVersion      = "ai-provider-v1"
	ContextVersion             = "ai-context-v1"
	TemplateVersion            = "ai-templates-v1"
)

// Server-side ceilings. None of them is caller-controlled; configuration may
// lower a ceiling, never raise it.
const (
	MaxRequestRunes   = 4000
	MaxOperations     = 50
	MaxContextBytes   = 64 << 10
	MaxOutputBytes    = 256 << 10
	MaxAttempts       = 3
	MaxListItems      = 50
	MaxTextRunes      = 2000
	MaxDiagnostics    = 40
	MaxProviderBytes  = 1 << 20
	MaxJSONDepth      = 32
	DefaultWallClock  = 60 * time.Second
	DefaultDailyToken = 200_000
)

// Task names what a model is asked to do. The set is closed.
type Task string

const (
	TaskApplicationPatch   Task = "application_patch"
	TaskRequirementCapture Task = "requirement_capture"
)

// Trust labels a prompt segment. Only template text and server-built structure are
// TRUSTED; anything an author, a tool description, a document or a model wrote is
// UNTRUSTED data, never an instruction.
type Trust string

const (
	TrustTrusted   Trust = "TRUSTED"
	TrustUntrusted Trust = "UNTRUSTED"
)

// Segment is one labelled part of a provider request.
type Segment struct {
	Name  string `json:"name"`
	Trust Trust  `json:"trust"`
	Text  string `json:"text"`
}

// Usage is what the provider reports (or what the gateway pessimistically
// estimates when it reports nothing).
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// ProviderRequest is the only thing a provider adapter ever sees.
type ProviderRequest struct {
	Task            Task            `json:"task"`
	TemplateVersion string          `json:"template_version"`
	Segments        []Segment       `json:"segments"`
	OutputSchema    json.RawMessage `json:"output_schema"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	Attempt         int             `json:"attempt"`
}

// ProviderResponse is what an adapter returns. Payload is untrusted bytes.
type ProviderResponse struct {
	Payload  json.RawMessage `json:"payload"`
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Usage    Usage           `json:"usage"`
	Latency  time.Duration   `json:"-"`
}

// Provider is the port every adapter implements. Implementations must honour the
// context deadline, bound what they read and never log request or response bodies.
type Provider interface {
	Complete(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}

// Typed provider failures. Adapters wrap them; the gateway classifies with errors.Is.
var (
	ErrProviderUnavailable = errors.New("ai: provider unavailable")
	ErrProviderTimeout     = errors.New("ai: provider timeout")
	ErrProviderRefused     = errors.New("ai: provider refused the request")
	ErrProviderMalformed   = errors.New("ai: provider response malformed")
	ErrNotConfigured       = errors.New("ai: no provider configured")
	ErrBudget              = errors.New("ai: budget exhausted")
	ErrContextTooLarge     = errors.New("ai: context exceeds the bundle limit")
)

// Attempt outcomes recorded in the trace.
const (
	OutcomeOK            = "OK"
	OutcomeParseError    = "PARSE_ERROR"
	OutcomeSchemaError   = "SCHEMA_ERROR"
	OutcomeForbidden     = "FORBIDDEN_TARGET"
	OutcomeConflict      = "CONFLICT"
	OutcomeCompileError  = "COMPILE_ERROR"
	OutcomeProviderError = "PROVIDER_ERROR"
	OutcomeTimeout       = "TIMEOUT"
	OutcomeBudget        = "BUDGET"
)

// RunStatus is the result of one repair-loop run. No status mutates the draft.
type RunStatus string

const (
	RunReady          RunStatus = "READY"
	RunNeedsHuman     RunStatus = "NEEDS_HUMAN"
	RunFailed         RunStatus = "FAILED"
	RunBudgetExceeded RunStatus = "BUDGET_EXCEEDED"
)

// Attempt is the traceability record of one provider call (hashes, never prompts).
type Attempt struct {
	N               int
	Provider, Model string
	TemplateVersion string
	ContextHash     string
	RequestHash     string
	OutputHash      string
	Usage           Usage
	Latency         time.Duration
	Outcome         string
	DiagnosticsHash string
}

// ---------------------------------------------------------------- patch proposal

// PatchOperation is one model-proposed change to one definition. The model supplies
// content only: it cannot declare a change class, a risk or an actor.
type PatchOperation struct {
	Op           string               `json:"op"` // add | update | delete
	Target       string               `json:"target"`
	ExpectedHash string               `json:"expected_hash"` // update/delete: metadata.DefinitionHash of what the model was shown
	Definition   *metadata.Definition `json:"definition,omitempty"`
	Rationale    string               `json:"rationale"`
}

// UnresolvedRequirement is a question the model could not settle. It never blocks
// the draft; it is carried to the requirement flow as an open question.
type UnresolvedRequirement struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Category string `json:"category,omitempty"`
}

// RiskFlag is advisory model text. It never lowers a server-derived value.
type RiskFlag struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// PatchProposal is the closed output schema of TaskApplicationPatch.
type PatchProposal struct {
	Version                string                  `json:"version"`
	Operations             []PatchOperation        `json:"operations"`
	Assumptions            []string                `json:"assumptions"`
	UnresolvedRequirements []UnresolvedRequirement `json:"unresolved_requirements"`
	ProposedTests          []intent.AcceptanceCase `json:"proposed_tests"` // advisory; never an acceptance spec
	RiskFlags              []RiskFlag              `json:"risk_flags"`
}

// Derived is what the server computes about a proposal. The model never supplies it.
type Derived struct {
	Targets   []DerivedTarget `json:"targets"`
	Sensitive []string        `json:"sensitive"` // "<target>:<aspect>" from compiler.ComputeDiff against the draft; apply must echo them
	Aspects   []string        `json:"aspects"`
}

// DerivedTarget is one affected definition with the server-derived kind.
type DerivedTarget struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Op   string `json:"op"`
}

// ------------------------------------------------------- requirement proposal

// RequirementDraft is a model-proposed typed requirement. Requiredness, its basis,
// status and provenance are not model fields; the server sets them on confirm.
type RequirementDraft struct {
	ID        string          `json:"id"`
	Category  string          `json:"category"`
	Statement string          `json:"statement"`
	Semantics json.RawMessage `json:"semantics"`
}

// RequirementProposal is the closed output schema of TaskRequirementCapture.
type RequirementProposal struct {
	Version       string                  `json:"version"`
	Requirements  []RequirementDraft      `json:"requirements"`
	OpenQuestions []UnresolvedRequirement `json:"open_questions"`
	ProposedTests []intent.AcceptanceCase `json:"proposed_tests"`
	Assumptions   []string                `json:"assumptions"`
}

// -------------------------------------------------------------------- pipeline

// CompileFunc validates and compiles a document with the operator catalogs of the
// running server. It returns the program (when valid) and machine-readable
// diagnostics. The platform supplies it; the ai package never builds catalogs.
type CompileFunc func(doc metadata.Document) (compiler.Program, []compiler.Diagnostic, error)

// ToolSummary is the only view of a tool the model receives: no endpoint, host,
// credential or sandbox output.
type ToolSummary struct {
	Ref        string   `json:"ref"`
	Version    string   `json:"version"`
	SideEffect string   `json:"side_effect"`
	Approval   string   `json:"approval"`
	Input      []string `json:"input"`
	Output     []string `json:"output"`
}

// ContextInput is everything BuildContext may read. All of it is derived by the
// server inside the caller's tenant scope.
type ContextInput struct {
	Task          Task
	Request       string // the authenticated human's text; UNTRUSTED
	Draft         metadata.Document
	DraftRevision int64
	DraftHash     string
	BaseProgram   *compiler.Program // active release program, when one exists
	DraftProgram  *compiler.Program // compiled draft, when it compiles; with BaseProgram it yields the draft-versus-release diff (addendum 1)
	Tools         []ToolSummary
	RoleNames     []string
	Requirements  *intent.RequirementSet // current set, for requirement capture
}

// Context is the canonical, redacted, size-checked bundle.
type Context struct {
	Version  string
	Segments []Segment
	Hash     string
	Bytes    int
}

// Budget bounds one run. The platform derives it from configuration and the tenant's
// remaining daily budget; the pipeline only enforces it.
type Budget struct {
	MaxAttempts     int
	MaxOutputTokens int
	WallClock       time.Duration
	TokensRemaining int64
}

// PatchInput is one RunPatch call.
type PatchInput struct {
	Context Context
	Draft   metadata.Document
	Compile CompileFunc
	Budget  Budget
	Now     func() time.Time
}

// PatchRun is the result of the repair loop. Final is the patched document that
// compiled (READY only); the ai package never persists or appends it.
type PatchRun struct {
	Status      RunStatus
	Proposal    *PatchProposal
	Canonical   []byte // canonical JSON of Proposal
	Hash        string // SHA-256 hex of Canonical
	Derived     Derived
	Final       *metadata.Document
	Attempts    []Attempt
	Diagnostics []compiler.Diagnostic // last diagnostics when NEEDS_HUMAN
	Usage       Usage
}

// RequirementInput is one RunRequirements call.
type RequirementInput struct {
	Context Context
	Budget  Budget
	Now     func() time.Time
}

// RequirementRun is the result of requirement capture.
type RequirementRun struct {
	Status    RunStatus
	Proposal  *RequirementProposal
	Canonical []byte
	Hash      string
	Attempts  []Attempt
	Usage     Usage
	// Diagnostics are the last semantic violations when the run ended without a
	// valid proposal (addendum 1); capped and sanitized like PatchRun.Diagnostics.
	Diagnostics []compiler.Diagnostic
}

// Conflict is a typed reason a proposal cannot be applied to the current draft.
type Conflict struct {
	Target   string `json:"target"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// Conflict categories of ApplyPatch.
const (
	ConflictStaleHash     = "STALE_EXPECTED_HASH"
	ConflictMissing       = "MISSING_TARGET"
	ConflictCollision     = "ADD_COLLISION"
	ConflictForbiddenKind = "FORBIDDEN_KIND"
	ConflictInvalid       = "INVALID_DEFINITION"
	// ConflictDuplicateTarget: two operations name one target (addendum 1).
	ConflictDuplicateTarget = "DUPLICATE_TARGET"
)

// Gateway runs the pipeline over an injected provider. Implementations are safe for
// concurrent use and never hold a database connection or transaction.
type Gateway interface {
	BuildContext(in ContextInput) (Context, error)
	RunPatch(ctx context.Context, in PatchInput) (PatchRun, error)
	RunRequirements(ctx context.Context, in RequirementInput) (RequirementRun, error)
}

// ApplyPatch applies a proposal to the *current* draft with per-target expected-hash
// CAS. It is the same function the repair loop and the apply endpoint use, so what
// compiled is what is applied. It returns the patched document, or conflicts and no
// document. It never trusts a stored result.
type ApplyPatch func(current metadata.Document, p PatchProposal) (metadata.Document, []Conflict, error)

// DeriveSensitive returns the server-derived sensitive "<target>:<aspect>" list for
// a patched program against the program it was applied to (compiler.ComputeDiff).
type DeriveSensitive func(current, patched compiler.Program) (Derived, error)
