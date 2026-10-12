// Package workflow owns the bounded business workflow model and durability ports.
package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
)

const Version = "2.0.0"
const ActivityVersion = "1.0.0"
const MaxPlanBytes = 1 << 20

var ErrInvalid = errors.New("invalid workflow contract")
var ErrConflict = errors.New("workflow version or idempotency conflict")
var ErrDenied = errors.New("workflow access denied")
var ErrNotFound = errors.New("workflow instance not found")
var ErrLease = errors.New("workflow lease is stale")
var idPattern = regexp.MustCompile(`^(workflow|entity|state|local)\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
var versionPattern = regexp.MustCompile(`^[1-9][0-9]{0,3}\.[0-9]{1,4}\.[0-9]{1,4}$`)

type Transition struct {
	To        string      `json:"to"`
	On        string      `json:"on"`
	Condition *rules.Plan `json:"condition,omitempty"`
}
type State struct {
	ID                string             `json:"id"`
	Type              string             `json:"type"`
	Transitions       []Transition       `json:"transitions"`
	Activity          *metadata.Activity `json:"activity,omitempty"`
	ApprovalRoles     []string           `json:"approval_roles,omitempty"`
	TimerMS           int                `json:"timer_ms,omitempty"`
	ApprovalTimeoutMS int                `json:"approval_timeout_ms,omitempty"`
}
type Plan struct {
	DSLVersion   string        `json:"dsl_version"`
	ID           string        `json:"id"`
	EntityID     string        `json:"entity_id"`
	Version      string        `json:"version"`
	Trigger      string        `json:"trigger"`
	TimeoutMS    int           `json:"timeout_ms"`
	InitialState string        `json:"initial_state"`
	Fields       []rules.Field `json:"fields"`
	States       []State       `json:"states"`
}
type Program struct {
	raw  []byte
	hash string
}

func Hash(raw []byte) string   { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func (p Program) Valid() bool  { return len(p.raw) > 0 && p.hash != "" }
func (p Program) JSON() []byte { return append([]byte(nil), p.raw...) }
func (p Program) Hash() string { return p.hash }
func (p Program) Plan() Plan   { var x Plan; _ = json.Unmarshal(p.raw, &x); return x }
func (p Program) State(id string) (State, bool) {
	for _, s := range p.Plan().States {
		if s.ID == id {
			return s, true
		}
	}
	return State{}, false
}
func (p Program) FieldIDs() []string {
	seen := map[string]bool{}
	for _, s := range p.Plan().States {
		for _, t := range s.Transitions {
			if t.Condition != nil {
				raw, _ := json.Marshal(t.Condition)
				r, e := rules.Restore(raw, rules.Hash(raw))
				if e == nil {
					for _, id := range r.FieldIDs() {
						seen[id] = true
					}
				}
			}
		}
	}
	out := []string{}
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func CompileDefinition(id string, w metadata.Workflow, fields []rules.Field, capabilities []string) (Program, error) {
	fs, _, e := rules.CanonicalInputs(fields, nil)
	if e != nil {
		return Program{}, ErrInvalid
	}
	p := Plan{Version: w.Version, DSLVersion: w.DSLVersion, ID: id, EntityID: w.EntityRef, Trigger: w.Trigger, TimeoutMS: w.TimeoutMS, InitialState: w.InitialState, Fields: fs, States: []State{}}
	for _, s := range w.States {
		x := State{ID: s.ID, Type: s.Type, Transitions: []Transition{}, Activity: s.Activity, ApprovalRoles: append([]string(nil), s.ApprovalRoles...), TimerMS: s.TimerMS, ApprovalTimeoutMS: s.ApprovalTimeoutMS}
		sort.Strings(x.ApprovalRoles)
		if x.Activity != nil {
			if !includes(capabilities, x.Activity.LocalRef) || (x.Activity.CompensationLocalRef != "" && !includes(capabilities, x.Activity.CompensationLocalRef)) {
				return Program{}, ErrInvalid
			}
		}
		for i, t := range s.Transitions {
			on := t.On
			if on == "" {
				on = "always"
			}
			tr := Transition{To: t.To, On: on}
			if t.Condition != nil {
				raw, _ := json.Marshal(t.Condition)
				var expression rules.Expression
				_ = json.Unmarshal(raw, &expression)
				condition, e := rules.Compile(rules.Definition{ID: conditionID(id, s.ID, i), EntityID: w.EntityRef, Version: rules.Version, ResultType: "bool", Fields: fs, Expression: expression})
				if e != nil {
					return Program{}, ErrInvalid
				}
				rp := condition.Plan()
				tr.Condition = &rp
			}
			x.Transitions = append(x.Transitions, tr)
		}
		p.States = append(p.States, x)
	}
	sort.Slice(p.States, func(i, j int) bool { return p.States[i].ID < p.States[j].ID })
	raw, e := json.Marshal(p)
	if e != nil {
		return Program{}, ErrInvalid
	}
	return Restore(raw, Hash(raw))
}
func conditionID(w, s string, i int) string {
	return fmt.Sprintf("rule.workflow_%s", Hash([]byte(fmt.Sprintf("%s/%s/%d", w, s, i)))[:24])
}
func Restore(raw []byte, hash string) (Program, error) {
	if len(raw) == 0 || len(raw) > MaxPlanBytes || Hash(raw) != hash {
		return Program{}, ErrInvalid
	}
	var p Plan
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return Program{}, ErrInvalid
	}
	canonical, _ := json.Marshal(p)
	if !bytes.Equal(canonical, raw) || validatePlan(p) != nil {
		return Program{}, ErrInvalid
	}
	return Program{append([]byte(nil), raw...), hash}, nil
}
func validatePlan(p Plan) error {
	if p.DSLVersion != Version || !validID(p.ID, "workflow") || !validID(p.EntityID, "entity") || !versionPattern.MatchString(p.Version) || p.TimeoutMS < 1 || p.TimeoutMS > 604800000 || !includes([]string{"manual", "record.created", "record.updated"}, p.Trigger) || len(p.States) < 2 || len(p.States) > 100 {
		return ErrInvalid
	}
	fs, _, e := rules.CanonicalInputs(p.Fields, nil)
	if e != nil || !reflect.DeepEqual(fs, p.Fields) || len(fs) < 1 || len(fs) > 32 {
		return ErrInvalid
	}
	states := map[string]State{}
	last := ""
	total := 0
	for _, s := range p.States {
		if !validID(s.ID, "state") || s.ID <= last || !includes([]string{"start", "activity", "approval", "timer", "complete", "failed"}, s.Type) {
			return ErrInvalid
		}
		last = s.ID
		states[s.ID] = s
		if (s.Type == "activity") != (s.Activity != nil) || (s.Type == "approval") != (len(s.ApprovalRoles) > 0) || (s.Type == "timer") != (s.TimerMS > 0) || (s.Type == "approval") != (s.ApprovalTimeoutMS > 0) {
			return ErrInvalid
		}
		if s.TimerMS < 0 || s.TimerMS > 86400000 || s.ApprovalTimeoutMS < 0 || s.ApprovalTimeoutMS > p.TimeoutMS {
			return ErrInvalid
		}
		if s.Activity != nil {
			a := s.Activity
			r := a.Retry
			if !validID(a.LocalRef, "local") || a.Version != ActivityVersion || a.ToolRef != "" || a.CompensationToolRef != "" || (a.CompensationLocalRef != "" && !validID(a.CompensationLocalRef, "local")) || a.TimeoutMS < 1 || a.TimeoutMS > 1000 || r.MaxAttempts < 1 || r.MaxAttempts > 10 || r.InitialBackoffMS < 100 || r.InitialBackoffMS > 60000 || r.MaxBackoffMS < r.InitialBackoffMS || r.MaxBackoffMS > 3600000 {
				return ErrInvalid
			}
		}
		for i, role := range s.ApprovalRoles {
			if !includes([]string{"owner", "admin", "member", "viewer"}, role) || (i > 0 && s.ApprovalRoles[i-1] >= role) {
				return ErrInvalid
			}
		}
		if s.Type == "complete" || s.Type == "failed" {
			if len(s.Transitions) > 0 {
				return ErrInvalid
			}
			continue
		}
		if len(s.Transitions) < 1 || len(s.Transitions) > 20 {
			return ErrInvalid
		}
		total += len(s.Transitions)
		if total > 256 {
			return ErrInvalid
		}
		defaults := map[string]bool{}
		for i, t := range s.Transitions {
			if !validID(t.To, "state") || !includes([]string{"always", "approved", "rejected"}, t.On) || (s.Type == "approval" && t.On == "always") || (s.Type != "approval" && t.On != "always") || defaults[t.On] {
				return ErrInvalid
			}
			if t.Condition == nil {
				defaults[t.On] = true
			} else {
				rp := *t.Condition
				raw, _ := json.Marshal(rp)
				if rp.ID != conditionID(p.ID, s.ID, i) || rp.EntityID != p.EntityID || rp.Root.Type.Kind != "bool" || !reflect.DeepEqual(rp.Fields, p.Fields) || len(rp.Calendars) != 0 {
					return ErrInvalid
				}
				if _, e := rules.Restore(raw, rules.Hash(raw)); e != nil {
					return ErrInvalid
				}
			}
		}
		if s.Type == "approval" {
			if !defaults["approved"] || !defaults["rejected"] {
				return ErrInvalid
			}
		} else if !defaults["always"] {
			return ErrInvalid
		}
	}
	initial, ok := states[p.InitialState]
	if !ok || initial.Type != "start" {
		return ErrInvalid
	}
	colors := map[string]int{}
	var walk func(string) error
	walk = func(id string) error {
		if colors[id] == 1 {
			return ErrInvalid
		}
		if colors[id] == 2 {
			return nil
		}
		s, ok := states[id]
		if !ok {
			return ErrInvalid
		}
		colors[id] = 1
		for _, t := range s.Transitions {
			if e := walk(t.To); e != nil {
				return e
			}
		}
		colors[id] = 2
		return nil
	}
	if walk(p.InitialState) != nil || len(colors) != len(states) {
		return ErrInvalid
	}
	return nil
}
func validID(id, kind string) bool {
	return len(id) <= 160 && idPattern.MatchString(id) && len(id) > len(kind)+1 && id[:len(kind)+1] == kind+"."
}
func includes(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
func (p Program) Next(ctx context.Context, state, on string, values map[string]json.RawMessage) (string, error) {
	s, ok := p.State(state)
	if !ok {
		return "", ErrInvalid
	}
	for _, t := range s.Transitions {
		if t.On != on {
			continue
		}
		if t.Condition == nil {
			return t.To, nil
		}
		raw, _ := json.Marshal(t.Condition)
		r, e := rules.Restore(raw, rules.Hash(raw))
		if e != nil {
			return "", e
		}
		v, e := r.Evaluate(ctx, values)
		if e != nil {
			return "", e
		}
		yes, e := v.Bool()
		if e != nil {
			return "", e
		}
		if yes {
			return t.To, nil
		}
	}
	return "", ErrInvalid
}

// MatchesDefinition compares the persisted versioned model with authored source;
// no current compiler is invoked on restore.
func (p Program) MatchesDefinition(id string, w metadata.Workflow, fields []rules.Field) bool {
	x := p.Plan()
	fs, _, e := rules.CanonicalInputs(fields, nil)
	if e != nil || x.ID != id || x.EntityID != w.EntityRef || x.DSLVersion != w.DSLVersion || x.Version != w.Version || x.Trigger != w.Trigger || x.TimeoutMS != w.TimeoutMS || x.InitialState != w.InitialState || !reflect.DeepEqual(x.Fields, fs) || len(x.States) != len(w.States) {
		return false
	}
	for _, s := range w.States {
		stored, ok := p.State(s.ID)
		roles := append([]string(nil), s.ApprovalRoles...)
		sort.Strings(roles)
		if !ok || stored.Type != s.Type || !reflect.DeepEqual(stored.Activity, s.Activity) || !reflect.DeepEqual(stored.ApprovalRoles, roles) || stored.TimerMS != s.TimerMS || stored.ApprovalTimeoutMS != s.ApprovalTimeoutMS || len(stored.Transitions) != len(s.Transitions) {
			return false
		}
		for i, t := range s.Transitions {
			on := t.On
			if on == "" {
				on = "always"
			}
			st := stored.Transitions[i]
			if st.To != t.To || st.On != on || (st.Condition == nil) != (t.Condition == nil) {
				return false
			}
			if t.Condition != nil {
				raw, _ := json.Marshal(t.Condition)
				var authored rules.Expression
				_ = json.Unmarshal(raw, &authored)
				want, _ := json.Marshal(authored)
				got, _ := json.Marshal(rules.ExpressionOf(st.Condition.Root))
				if !bytes.Equal(want, got) {
					return false
				}
			}
		}
	}
	return true
}
