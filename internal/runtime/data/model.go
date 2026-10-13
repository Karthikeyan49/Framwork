// Package data executes bounded typed plans against isolated preview datasets.
package data

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"time"
)

var ErrInvalid = errors.New("invalid data request")
var ErrDenied = errors.New("data access denied")
var ErrConflict = errors.New("record version conflict")
var ErrBinding = errors.New("dataset binding is stale or sealed")
var ErrNotFound = errors.New("record not found")
var ErrBudget = errors.New("query budget exceeded")
var ErrUnavailable = errors.New("data contract unavailable")

const MaxRecordBytes = 64 << 10
const MaxPage = 100

// Scope pins both immutable source revision and actual compiled content.
type Scope struct {
	program        compiler.Program
	revision       int64
	anchorRevision int64
	anchorHash     string
	datasetID      string
	binding        int64
}

func NewScope(p compiler.Program, revision int64) (Scope, error) {
	if !p.Valid() || revision < 1 || revision > 1e9 || p.IR().SchemaSnapshot != compiler.SchemaSnapshot {
		return Scope{}, ErrInvalid
	}
	return Scope{program: p, revision: revision, anchorRevision: revision, anchorHash: p.Hash()}, nil
}
func (s Scope) AppID() string             { return s.program.IR().Application.AppID }
func (s Scope) Hash() string              { return s.program.Hash() }
func (s Scope) Revision() int64           { return s.revision }
func (s Scope) DataRevision() int64       { return s.anchorRevision }
func (s Scope) DataHash() string          { return s.anchorHash }
func (s Scope) DatasetID() string         { return s.datasetID }
func (s Scope) BindingVersion() int64     { return s.binding }
func (s Scope) Program() compiler.Program { return s.program }

type Resource struct{ EntityID, FieldID, RecordID, OwnerID string }
type Guard func(context.Context, string, Resource) error

func (g Guard) check(ctx context.Context, action string, r Resource) error {
	if g == nil {
		return ErrDenied
	}
	return g(ctx, action, r)
}

type Record struct {
	ID        string             `json:"id"`
	Version   int64              `json:"version"`
	CreatedBy string             `json:"created_by"`
	OwnerID   string             `json:"owner_user_id,omitempty"`
	TeamID    string             `json:"owner_team_id,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Values    map[string]any     `json:"values"`
	Related   map[string]*Record `json:"related,omitempty"`
}
type Filter struct {
	Op    string          `json:"op"`
	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
	Args  []Filter        `json:"args,omitempty"`
}
type Sort struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc"`
}
type Join struct {
	Relationship string   `json:"relationship"`
	Projection   []string `json:"projection"`
}
type Aggregate struct {
	Op    string `json:"op"`
	Field string `json:"field,omitempty"`
}
type Query struct {
	RecordID   string     `json:"record_id,omitempty"`
	Projection []string   `json:"projection"`
	Filter     *Filter    `json:"filter,omitempty"`
	Sort       *Sort      `json:"sort,omitempty"`
	Limit      int        `json:"limit"`
	After      string     `json:"after,omitempty"`
	Join       *Join      `json:"join,omitempty"`
	Aggregate  *Aggregate `json:"aggregate,omitempty"`
}
type Page struct {
	Records   []Record         `json:"records"`
	Next      string           `json:"next"`
	Aggregate *AggregateResult `json:"aggregate,omitempty"`
}
type AggregateResult struct {
	Count int    `json:"count"`
	Sum   string `json:"sum,omitempty"`
}
type Command struct {
	Operation       string         `json:"operation"`
	ID              string         `json:"id,omitempty"`
	ExpectedVersion int64          `json:"expected_version,omitempty"`
	Values          map[string]any `json:"values,omitempty"`
}

// RecordStore is independent of physical storage; Domain Packs supply typed adapters.
type RecordStore interface {
	Query(context.Context, postgres.Executor, Scope, tenant.Access, string, Query, Guard, ...ScopeResolver) (Page, error)
	Command(context.Context, postgres.Executor, Scope, tenant.Access, string, Command, Guard, ...ScopeResolver) (Record, error)
}
type StorageClass string

const DynamicStorage StorageClass = "DYNAMIC_JSONB"
const DomainPackStorage StorageClass = "TYPED_DOMAIN_PACK"
const LegacyStorage StorageClass = "LEGACY_PREVIEW"
