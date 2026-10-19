package corpus

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Result is the observable outcome of running one case through a pipeline, reduced to
// plain values so this package needs no knowledge of the pipeline's types.
type Result struct {
	Status     string   // READY NEEDS_HUMAN FAILED BUDGET_EXCEEDED; "" when the context was refused
	Outcomes   []string // per-attempt outcome codes, in order
	Calls      int      // provider calls made
	Elapsed    time.Duration
	Document   bool                // a patched document came back
	Sensitive  []string            // the server-derived "<target>:<aspect>" list
	Oracle     map[string][]string // target -> aspects, from an independent compiler.ComputeDiff
	DiagCodes  []string            // lower-case codes of the last diagnostics
	Tokens     int64               // total charged usage
	NegTokens  bool                // any negative figure
	Canonical  string              // canonical proposal text ("" when none)
	ContextErr string              // "" | too_large | invalid | other
	Context    []Seg               // the built context
	Baseline   []Seg               // the context of a benign request over the same fixture
	Feedback   []Seg               // repair and diagnostics segments of the second request
	RunError   string
	NoOracle   bool // the observer cannot recompute the diff (an HTTP run): skip the independent comparison
}

// Report is the verdict on one case.
type Report struct {
	Failures []string // properties that did not hold
	Notes    []string // documented ambiguities and residuals, and observations
}

// rank orders the layers a hostile answer can be caught at; the earlier the better.
var rank = map[string]int{"OK": 0, "PARSE_ERROR": 1, "SCHEMA_ERROR": 2, "FORBIDDEN_TARGET": 3, "CONFLICT": 4, "COMPILE_ERROR": 5}

var layerLimit = map[string]int{"syntax": 1, "schema": 2, "invalid_semantics": 2, "allowlist": 3, "rejected": 4, "conflict": 4, "invalid": 4, "schema_or_invalid": 4, "schema_or_conflict": 4, "compile": 5}

var plainAspects = map[string]bool{"LABEL": true, "PAGE": true}

// Check compares a result with the properties the item declares. A hostile answer may
// be caught EARLIER than its documented layer (stricter) but never later.
func (c Case) Check(r Result, w Wrapper) Report {
	var rep Report
	fail := func(format string, a ...any) { rep.Failures = append(rep.Failures, fmt.Sprintf(format, a...)) }
	note := func(format string, a ...any) { rep.Notes = append(rep.Notes, fmt.Sprintf(format, a...)) }
	x := c.Item.Expect
	if x.Ambiguous != "" {
		note("ambiguous: %s", x.Ambiguous)
	}
	if x.Context != nil && x.Context.Residual != "" {
		note("residual: %s", x.Context.Residual)
	}
	if x.Diagnostics != nil && x.Diagnostics.Residual != "" {
		note("residual: %s", x.Diagnostics.Residual)
	}

	// The context first: a refusal is a legitimate outcome only where the item allows it.
	if r.ContextErr != "" {
		switch {
		case x.ContextError != "":
			if r.ContextErr != x.ContextError {
				fail("context refused as %s, want %s", r.ContextErr, x.ContextError)
			}
			if r.Calls != 0 {
				fail("a refused context reached the provider (%d calls)", r.Calls)
			}
		case x.Context != nil && x.Context.Error != "":
		default:
			fail("the context was refused (%s) but the item expects a run", r.ContextErr)
		}
		c.checkContext(r, w, fail)
		return rep
	}
	if x.ContextError != "" {
		fail("the context was built but the item expects it refused as %s", x.ContextError)
	}
	if x.Context != nil && x.Context.Error == "reject" {
		fail("the context must be refused for this input")
	}
	if r.RunError != "" {
		fail("the run returned an error: %s", r.RunError)
	}
	if len(x.Status) > 0 && !slices.Contains(x.Status, r.Status) {
		fail("status %s, want one of %v", r.Status, x.Status)
	}
	if x.MaxCalls != nil && r.Calls > *x.MaxCalls {
		fail("%d provider calls, at most %d allowed", r.Calls, *x.MaxCalls)
	}
	if x.ElapsedMSMax > 0 && r.Elapsed > time.Duration(x.ElapsedMSMax)*time.Millisecond {
		fail("took %v, the bound is %dms", r.Elapsed, x.ElapsedMSMax)
	}
	if x.Attempts != nil && !slices.Equal(x.Attempts, r.Outcomes) {
		fail("attempt outcomes %v, want %v", r.Outcomes, x.Attempts)
	}
	for _, o := range x.AttemptsAny {
		if !slices.Contains(r.Outcomes, o) {
			fail("no attempt ended %s (outcomes %v)", o, r.Outcomes)
		}
	}
	c.checkLayer(r, fail)
	for _, cat := range x.Conflicts {
		if !slices.Contains(r.DiagCodes, strings.ToLower(cat)) {
			fail("no diagnostic for conflict %s (codes %v)", cat, r.DiagCodes)
		}
	}
	if r.Document && !r.NoOracle {
		oracleSensitive := map[string]bool{}
		for target, aspects := range r.Oracle {
			for _, a := range aspects {
				if !plainAspects[a] {
					oracleSensitive[target+":"+a] = true
				}
			}
		}
		got := map[string]bool{}
		for _, s := range r.Sensitive {
			got[s] = true
		}
		for s := range oracleSensitive {
			if !got[s] {
				fail("sensitive aspect %s is in the independent diff but missing from the derived list %v", s, r.Sensitive)
			}
		}
		for s := range got {
			if !oracleSensitive[s] {
				fail("derived sensitive entry %s is not in the independent diff", s)
			}
		}
		if x.OracleDiff != nil {
			for target, want := range x.OracleDiff {
				have := slices.Clone(r.Oracle[target])
				want = slices.Clone(want)
				sort.Strings(have)
				sort.Strings(want)
				if !slices.Equal(have, want) {
					fail("independent diff of %s is %v, the item expects %v", target, have, want)
				}
			}
		}
		for _, s := range x.SensitiveHas {
			if !got[s] {
				fail("sensitive list %v lacks %s", r.Sensitive, s)
			}
		}
		if x.SensitiveEmpty && len(r.Sensitive) > 0 {
			fail("sensitive list should be empty, got %v", r.Sensitive)
		}
	} else if r.Document && r.NoOracle {
		got := map[string]bool{}
		for _, s := range r.Sensitive {
			got[s] = true
		}
		for _, s := range x.SensitiveHas {
			if !got[s] {
				fail("sensitive list %v lacks %s", r.Sensitive, s)
			}
		}
		if x.SensitiveEmpty && len(r.Sensitive) > 0 {
			fail("sensitive list should be empty, got %v", r.Sensitive)
		}
	} else if x.OracleDiff != nil || len(x.SensitiveHas) > 0 {
		if r.Status == "READY" {
			fail("READY without a document")
		}
	}
	if x.Usage != nil {
		if x.Usage.NonNegative && (r.NegTokens || r.Tokens < 0) {
			fail("negative usage charged (%d)", r.Tokens)
		}
		if r.Tokens < x.Usage.MinTotalTokens {
			fail("charged %d tokens, at least %d expected (a provider cannot under-report)", r.Tokens, x.Usage.MinTotalTokens)
		}
	}
	for _, name := range x.CanonicalAbsent {
		if v := c.canaryOf(name); v != "" && strings.Contains(r.Canonical, v) {
			note("OBSERVED: the stored proposal body contains the secret-shaped token %s (ADR104 redacts the context, not the output; severity %s)", name, x.Severity)
		}
	}
	c.checkContext(r, w, fail)
	c.checkFeedback(r, w, fail)
	return rep
}

func (c Case) canaryOf(name string) string {
	return c.canaries[strings.TrimPrefix(name, "canary:")]
}

func (c Case) checkLayer(r Result, fail func(string, ...any)) {
	x := c.Item.Expect
	layers := []string{}
	if x.Layer != "" {
		layers = append(layers, x.Layer)
	}
	layers = append(layers, x.LayerAny...)
	if len(layers) == 0 {
		return
	}
	for _, l := range layers {
		if c.layerHolds(l, r) {
			return
		}
	}
	fail("layer %v: status %s, outcomes %v, document=%v, sensitive=%v", layers, r.Status, r.Outcomes, r.Document, r.Sensitive)
}

func (c Case) layerHolds(layer string, r Result) bool {
	switch layer {
	case "accepted":
		return r.Status == "READY" && (r.Document || c.Item.Task == "requirement_capture")
	case "human":
		return r.Status == "READY" && r.Document && len(r.Sensitive) > 0
	}
	limit, ok := layerLimit[layer]
	if !ok || r.Document || r.Status == "READY" {
		return false
	}
	for _, o := range r.Outcomes {
		if rk, known := rank[o]; known && (rk == 0 || rk > limit) {
			return false
		}
	}
	return true
}

func (c Case) checkContext(r Result, w Wrapper, fail func(string, ...any)) {
	x := c.Item.Expect.Context
	if x == nil || len(r.Context) == 0 {
		return
	}
	trusted := func(segs []Seg) []Seg {
		var out []Seg
		for _, s := range segs {
			if s.Trust == "TRUSTED" {
				out = append(out, s)
			}
		}
		return out
	}
	if x.StructureStable || x.RequestUntrusted {
		if len(r.Context) != len(r.Baseline) {
			fail("context has %d segments, the baseline %d", len(r.Context), len(r.Baseline))
		} else {
			for i := range r.Context {
				if r.Context[i].Name != r.Baseline[i].Name || r.Context[i].Trust != r.Baseline[i].Trust {
					fail("segment %d is %s/%s, the baseline has %s/%s", i, r.Context[i].Name, r.Context[i].Trust, r.Baseline[i].Name, r.Baseline[i].Trust)
				}
			}
		}
	}
	if x.RequestUntrusted || x.TrustedUnchanged {
		if !slices.Equal(trusted(r.Context), trusted(r.Baseline)) {
			fail("a TRUSTED segment depends on the input")
		}
	}
	if x.CannotCloseWrapper {
		c.wrapperCheck(r.Context, w, fail)
	}
	for _, m := range x.MarkersUntrusted {
		marker := c.Expanded(m)
		for _, s := range trusted(r.Context) {
			if marker != "" && strings.Contains(s.Text, marker) {
				fail("the marker %q reached the TRUSTED segment %s", m, s.Name)
			}
		}
	}
	for _, name := range x.Absent {
		if v := c.canaryOf(name); v != "" {
			for _, s := range r.Context {
				if strings.Contains(s.Text, v) {
					fail("canary %s survived in segment %s", name, s.Name)
				}
			}
		}
	}
	for _, raw := range x.NoRaw {
		if v := c.Expanded(raw); v != "" {
			for _, s := range r.Context {
				if strings.Contains(s.Text, v) {
					fail("raw %q reached segment %s", raw, s.Name)
				}
			}
		}
	}
}

func (c Case) wrapperCheck(segs []Seg, w Wrapper, fail func(string, ...any)) {
	for _, s := range segs {
		if s.Trust != "UNTRUSTED" {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(s.Text, w.Begin+"\n"), "\n"+w.End)
		if !strings.HasPrefix(s.Text, w.Begin+"\n") || !strings.HasSuffix(s.Text, "\n"+w.End) ||
			strings.Count(s.Text, w.End) != 1 || strings.Count(s.Text, w.Begin) != 1 || strings.ContainsAny(body, "<>\n") || !json.Valid([]byte(body)) {
			fail("segment %s can be closed or forged by its content", s.Name)
		}
	}
}

func (c Case) checkFeedback(r Result, w Wrapper, fail func(string, ...any)) {
	x := c.Item.Expect.Diagnostics
	if x == nil || len(r.Feedback) == 0 {
		return
	}
	var diag *Seg
	for i := range r.Feedback {
		if r.Feedback[i].Name == "diagnostics" {
			diag = &r.Feedback[i]
		}
	}
	if diag == nil {
		fail("a repaired run must carry a diagnostics segment")
		return
	}
	if x.MaxSegmentBytes > 0 && len(diag.Text) > x.MaxSegmentBytes {
		fail("the feedback segment is %d bytes, the bound is %d", len(diag.Text), x.MaxSegmentBytes)
	}
	if x.MaxItems > 0 {
		var body struct {
			Problems []json.RawMessage `json:"problems"`
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(diag.Text, w.Begin+"\n"), "\n"+w.End)
		if err := json.Unmarshal([]byte(inner), &body); err != nil || len(body.Problems) > x.MaxItems {
			fail("the feedback holds %d problems (%v), the bound is %d", len(body.Problems), err, x.MaxItems)
		}
	}
	if x.CannotCloseWrapper {
		c.wrapperCheck([]Seg{*diag}, w, fail)
	}
	for _, m := range x.MarkersOrAbsent() {
		marker := c.Expanded(m)
		for _, s := range r.Feedback {
			if s.Trust == "TRUSTED" && marker != "" && strings.Contains(s.Text, marker) {
				fail("the marker %q reached the TRUSTED feedback segment %s", m, s.Name)
			}
		}
	}
	for _, raw := range x.NoRaw {
		if v := c.Expanded(raw); v != "" && strings.Contains(diag.Text, v) {
			fail("raw %q reached the feedback", raw)
		}
	}
}

// MarkersOrAbsent returns the markers that must be absent from TRUSTED
// feedback (they may appear inside the UNTRUSTED data segment).
func (d *DiagExpect) MarkersOrAbsent() []string { return d.MarkersUntrustedOrAbsent }
