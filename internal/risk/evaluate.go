package risk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

func (m *ToolMatch) match(t ToolFact) bool {
	return in(m.SideEffect, t.SideEffect) && in(m.Idempotency, t.Idempotency) && in(m.Change, t.Change) && (!m.Unresolved || t.Unresolved)
}

func (m *DeclMatch) match(d ToolDeclFact) bool {
	flag := true
	switch m.Flag {
	case flagGainsDataWrite:
		flag = d.GainsDataWrite
	case flagWeakensApproval:
		flag = d.WeakensApproval
	}
	return flag && in(m.SideEffect, d.SideEffect) && in(m.Change, d.Change)
}

// fires evaluates the predicate over normalized facts. MinLevel is the
// caller's concern: it depends on the level of other rules.
func (t Trigger) fires(f *Facts) bool {
	switch {
	case len(t.Any) > 0:
		for _, name := range t.Any {
			if get, ok := boolFact(name); ok && get(f) {
				return true
			}
		}
	case t.Tool != nil:
		for _, x := range f.Tools {
			if t.Tool.match(x) {
				return true
			}
		}
	case t.Decl != nil:
		for _, x := range f.ToolDeclarations {
			if t.Decl.match(x) {
				return true
			}
		}
	case t.Storage != nil:
		return in(t.Storage.Class, f.Storage.Class) && (t.Storage.Reason == "" || in(f.Storage.Reasons, t.Storage.Reason))
	}
	return false
}

// Evaluate assesses f under p. Facts are validated and normalized first (any
// input order of tool facts, declarations and reasons yields the same result);
// invalid facts are an error, never a default level. The result is a pure
// function of (p, f): level is the maximum over fired level rules and the
// storage x volume cell, blocking rules set Blocked without lowering or raising
// the level, and a rule conditional on a minimum level is judged against the
// level of the unconditional rules.
func (p Policy) Evaluate(f Facts) (Assessment, error) {
	if p.hash == "" {
		return Assessment{}, fmt.Errorf("%w: zero policy", ErrInvalidPolicy)
	}
	n, canon, err := canonicalize(f)
	if err != nil {
		return Assessment{}, err
	}
	level, blocked := Low, false
	fired := map[string]bool{}
	hit := func(id string, o outcome) {
		if id != "" {
			fired[id] = true
		}
		if o.blocked {
			blocked = true
		} else if o.level > level {
			level = o.level
		}
	}
	for _, r := range p.rules {
		if r.min == 0 && r.t.fires(&n) {
			hit(r.id, r.out)
		}
	}
	c := p.cells[indexOf(classes, n.Storage.Class)][indexOf(volumes, n.Volume)]
	hit(c.id, c.out)
	for _, r := range p.rules {
		if r.min != 0 && level >= r.min && r.t.fires(&n) {
			hit(r.id, r.out)
		}
	}
	reasons := make([]string, 0, len(fired))
	for id := range fired {
		reasons = append(reasons, id)
	}
	sort.Strings(reasons)
	rollback, rank := p.spec.Rollback.Order[0], 0
	for _, r := range p.rb {
		if r.rank > rank && r.t.fires(&n) {
			rollback, rank = r.class, r.rank
		}
	}
	sum := sha256.Sum256(canon)
	s := p.sep[level]
	return Assessment{
		PolicyVersion: p.spec.Version,
		PolicyHash:    p.hash,
		Level:         level,
		Blocked:       blocked,
		Reasons:       reasons,
		FactsHash:     hex.EncodeToString(sum[:]),
		Separation:    Separation{Approvals: s.Approvals, Excluded: cloneStrings(s.Excluded)},
		Rollback:      rollback,
	}, nil
}
