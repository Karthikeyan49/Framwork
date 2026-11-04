package risk

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Registry is an immutable, ordered set of policies, oldest first; the last one
// is the latest. It has no mutators: the built-in registry is fixed at build
// time, so nothing can register a weaker policy at runtime. NewRegistry exists
// for tests and tools that need an independent set.
type Registry struct{ policies []Policy }

// NewRegistry orders policies oldest first. It rejects an empty set, a zero
// Policy and duplicate versions.
func NewRegistry(policies ...Policy) (Registry, error) {
	if len(policies) == 0 {
		return Registry{}, fmt.Errorf("%w: empty registry", ErrInvalidPolicy)
	}
	r := Registry{policies: append([]Policy(nil), policies...)}
	for i, p := range r.policies {
		if p.hash == "" {
			return Registry{}, fmt.Errorf("%w: zero policy", ErrInvalidPolicy)
		}
		for _, q := range r.policies[:i] {
			if q.spec.Version == p.spec.Version {
				return Registry{}, fmt.Errorf("%w: duplicate version", ErrInvalidPolicy)
			}
		}
	}
	return r, nil
}

// Latest returns the newest policy; the zero Policy (which only errors) for a
// zero Registry.
func (r Registry) Latest() Policy {
	if len(r.policies) == 0 {
		return Policy{}
	}
	return r.policies[len(r.policies)-1]
}

// Lookup returns the policy registered under version.
func (r Registry) Lookup(version string) (Policy, bool) {
	for _, p := range r.policies {
		if p.spec.Version == version {
			return p, true
		}
	}
	return Policy{}, false
}

// Versions lists the registered versions, oldest first.
func (r Registry) Versions() []string {
	out := make([]string, 0, len(r.policies))
	for _, p := range r.policies {
		out = append(out, p.spec.Version)
	}
	return out
}

// Evaluate assesses f under the latest policy.
func (r Registry) Evaluate(f Facts) (Assessment, error) { return r.Latest().Evaluate(f) }

// EvaluateWith assesses f under a named policy version. An unregistered version
// is ErrUnknownPolicy, never a fallback to another policy.
func (r Registry) EvaluateWith(version string, f Facts) (Assessment, error) {
	p, ok := r.Lookup(version)
	if !ok {
		return Assessment{}, ErrUnknownPolicy
	}
	return p.Evaluate(f)
}

// Verify re-runs the policy named by a over f and requires byte equality of the
// canonical assessments. An unknown policy, invalid facts or any altered field
// (including an unserializable level) fail.
func (r Registry) Verify(a Assessment, f Facts) error {
	want, err := r.EvaluateWith(a.PolicyVersion, f)
	if err != nil {
		return err
	}
	x, err := json.Marshal(want)
	if err != nil {
		return ErrMismatch
	}
	y, err := json.Marshal(a)
	if err != nil || !bytes.Equal(x, y) {
		return ErrMismatch
	}
	return nil
}

func mustPolicy(s Spec) Policy {
	p, err := NewPolicy(s)
	if err != nil {
		panic("risk: built-in policy is invalid: " + err.Error())
	}
	return p
}

// builtin holds every policy this binary can evaluate. Adding a policy means
// appending a new version here; published versions are never edited or removed,
// because stored candidates re-evaluate under the version they name.
var builtin = func() Registry {
	r, err := NewRegistry(mustPolicy(releaseRiskV1()))
	if err != nil {
		panic("risk: built-in registry is invalid: " + err.Error())
	}
	return r
}()

// Latest returns the newest built-in policy.
func Latest() Policy { return builtin.Latest() }

// Lookup returns a built-in policy by version.
func Lookup(version string) (Policy, bool) { return builtin.Lookup(version) }

// Versions lists the built-in policy versions, oldest first.
func Versions() []string { return builtin.Versions() }

// Evaluate assesses f under the latest built-in policy.
func Evaluate(f Facts) (Assessment, error) { return builtin.Evaluate(f) }

// EvaluateWith assesses f under a named built-in policy version.
func EvaluateWith(version string, f Facts) (Assessment, error) {
	return builtin.EvaluateWith(version, f)
}

// Verify re-evaluates a over f under the built-in policy it names.
func Verify(a Assessment, f Facts) error { return builtin.Verify(a, f) }

// VolumeBucket maps a measured record count to its bucket under the latest
// built-in policy; see Policy.VolumeBucket.
func VolumeBucket(count int64, known bool) string { return Latest().VolumeBucket(count, known) }
