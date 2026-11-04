// Package rules executes versioned, pure typed plans. It has no host capabilities.
package rules

import (
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/erp"
)

const Version = "2.0.0"
const MaxNodes = 256
const MaxDepth = 16
const MaxString = 4096
const MaxList = 64
const MaxDays = 3660
const MaxWork = 50000

var ErrInvalid = errors.New("invalid rule contract")
var ErrBudget = errors.New("rule budget exceeded")
var ErrInput = errors.New("invalid rule input")
var ErrArithmetic = errors.New("rule arithmetic failed")
var ErrNull = errors.New("rule operand is null")
var ErrDate = errors.New("rule date or calendar operation failed")

type Type struct {
	Kind        string `json:"kind"`
	Currency    string `json:"currency,omitempty"`
	Element     *Type  `json:"element,omitempty"`
	ReferenceTo string `json:"reference_to,omitempty"`
}
type Field struct {
	ID         string   `json:"id"`
	Type       Type     `json:"type"`
	Required   bool     `json:"required"`
	EnumValues []string `json:"enum_values,omitempty"`
}
type Expression struct {
	Op        string          `json:"op"`
	Value     json.RawMessage `json:"value,omitempty"`
	ValueType string          `json:"value_type,omitempty"`
	FieldRef  string          `json:"field_ref,omitempty"`
	Args      []Expression    `json:"args,omitempty"`
}
type Definition struct {
	ID, EntityID, Version, ResultType string
	Fields                            []Field
	Calendars                         []erp.BusinessCalendar
	Expression                        Expression
}
type Node struct {
	Op        string          `json:"op"`
	Type      Type            `json:"type"`
	Value     json.RawMessage `json:"value,omitempty"`
	ValueType string          `json:"value_type,omitempty"`
	FieldRef  string          `json:"field_ref,omitempty"`
	Args      []Node          `json:"args,omitempty"`
}
type Plan struct {
	Version    string                 `json:"version"`
	ID         string                 `json:"id"`
	EntityID   string                 `json:"entity_id"`
	TZDataHash string                 `json:"tzdata_hash"`
	Fields     []Field                `json:"fields"`
	Calendars  []erp.BusinessCalendar `json:"calendars"`
	Root       Node                   `json:"root"`
}

// Program owns only immutable serialized state; all evaluation state is local.
type Program struct {
	raw  []byte
	hash string
}

func (p Program) Hash() string { return p.hash }
func (p Program) JSON() []byte { return append([]byte(nil), p.raw...) }
func (p Program) Plan() Plan   { var x Plan; _ = json.Unmarshal(p.raw, &x); return x }
func (p Program) Valid() bool  { return len(p.raw) > 0 && p.hash != "" }
func (p Program) FieldIDs() []string {
	seen := map[string]bool{}
	var walk func(Node)
	walk = func(n Node) {
		if n.Op == "field" {
			seen[n.FieldRef] = true
		}
		for _, a := range n.Args {
			walk(a)
		}
	}
	walk(p.Plan().Root)
	out := []string{}
	for _, f := range p.Plan().Fields {
		if seen[f.ID] {
			out = append(out, f.ID)
		}
	}
	return out
}
