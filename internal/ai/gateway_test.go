package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Tests of the repair loop (ADR104 section 7). Every "model answer" is a scripted
// byte string: nothing here says anything about a real model.

const testDraftHash = "30a8809169a283477a35ef7855493077798a31b44ca0be00ce34dc3b4025a8bd"

type rig struct {
	t     *testing.T
	doc   metadata.Document
	prov  *ScriptedProvider
	g     Gateway
	comp  *compileCounter
	clock *fakeClock
}

func newRig(t *testing.T, opts Options, steps ...ScriptStep) *rig {
	t.Helper()
	r := &rig{t: t, doc: fixtureDoc(t, exWorkflow), prov: NewScriptedProvider(steps...), comp: newCompiler(t), clock: newFakeClock()}
	if opts.Clock == nil {
		opts.Clock = r.clock.Now
	}
	r.g = NewGateway(r.prov, opts)
	return r
}

func (r *rig) context(task Task, request string) Context {
	r.t.Helper()
	c, err := r.g.BuildContext(ContextInput{Task: task, Request: request, Draft: r.doc, DraftRevision: 3, DraftHash: testDraftHash})
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

func (r *rig) patchInput(budget Budget) PatchInput {
	return PatchInput{Context: r.context(TaskApplicationPatch, "Rename the contact entity."), Draft: r.doc, Compile: r.comp.Compile, Budget: budget}
}

func (r *rig) run(budget Budget) PatchRun {
	r.t.Helper()
	run, err := r.g.RunPatch(context.Background(), r.patchInput(budget))
	if err != nil {
		r.t.Fatalf("RunPatch: %v", err)
	}
	return run
}

func okBudget() Budget {
	return Budget{MaxAttempts: 3, MaxOutputTokens: 4096, WallClock: 5 * time.Second, TokensRemaining: 1_000_000}
}

func (r *rig) good() []byte {
	return mustJSON(r.t, proposalMap(updateLabelOp(r.t, r.doc, "entity.contact", "Customer")))
}

// broken compiles badly: a page that references an entity that does not exist.
func (r *rig) broken(label string) []byte {
	d := metadata.Definition{ID: "page.ghost", SchemaVersion: r.doc.SchemaVersion, Namespace: r.doc.Namespace, Label: label, Kind: "page",
		Page: &metadata.Page{EntityRef: "entity.ghost", Components: []metadata.Component{{Type: "section", Label: "S", Children: []metadata.Component{{Type: "table", EntityRef: "entity.ghost"}}}}}}
	return mustJSON(r.t, proposalMap(map[string]any{"op": "add", "target": "page.ghost", "expected_hash": "", "definition": asMap(r.t, d), "rationale": "Add."}))
}

func usage(in, out int64) Usage { return Usage{InputTokens: in, OutputTokens: out} }

func outcomes(a []Attempt) []string {
	out := make([]string, len(a))
	for i, x := range a {
		out[i] = x.Outcome
	}
	return out
}

func TestRunPatchFirstTrySuccess(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Payload: r.good(), Usage: usage(1200, 300), Model: "scripted-model-1"})
	before := mustJSON(t, r.doc)
	run := r.run(okBudget())
	if run.Status != RunReady || run.Proposal == nil || run.Final == nil || len(run.Canonical) == 0 {
		t.Fatalf("run %+v", run)
	}
	if !bytes.Equal(before, mustJSON(t, r.doc)) {
		t.Fatal("RunPatch mutated the draft it was given")
	}
	if run.Hash != sha256Hex(run.Canonical) {
		t.Fatal("hash is not the SHA-256 of the canonical bytes")
	}
	if _, err := ParsePatchProposal(run.Canonical); err != nil {
		t.Fatal("canonical bytes do not re-parse:", err)
	}
	if got := defByID(t, *run.Final, "entity.contact").Label; got != "Customer" {
		t.Fatalf("final label %q", got)
	}
	if got := defByID(t, r.doc, "entity.contact").Label; got == "Customer" {
		t.Fatal("the input draft was changed")
	}
	if len(run.Derived.Sensitive) != 0 || len(run.Derived.Targets) != 1 || run.Derived.Targets[0].Op != "update" {
		t.Fatalf("derived %+v", run.Derived)
	}
	if len(run.Attempts) != 1 {
		t.Fatalf("attempts %+v", run.Attempts)
	}
	a := run.Attempts[0]
	if a.Outcome != OutcomeOK || a.N != 1 || a.Provider != "scripted" || a.Model != "scripted-model-1" || a.TemplateVersion != TemplateVersion ||
		len(a.RequestHash) != 64 || a.OutputHash != sha256Hex(r.good()) || a.ContextHash != r.patchInput(okBudget()).Context.Hash || a.DiagnosticsHash != "" {
		t.Fatalf("attempt %+v", a)
	}
	reqs := r.prov.Requests()
	if len(reqs) != 1 || reqs[0].Attempt != 1 || reqs[0].Task != TaskApplicationPatch || reqs[0].MaxOutputTokens != 4096 || len(reqs[0].OutputSchema) == 0 {
		t.Fatalf("request %+v", reqs)
	}
}

func TestRunPatchRepairsOnTheSecondAttempt(t *testing.T) {
	r := newRig(t, Options{})
	bad := r.broken("Ghost page")
	r.prov.Push(ScriptStep{Payload: bad, Usage: usage(1200, 300)}, ScriptStep{Payload: r.good(), Usage: usage(1300, 200)})
	run := r.run(okBudget())
	if run.Status != RunReady || fmt.Sprint(outcomes(run.Attempts)) != "[COMPILE_ERROR OK]" {
		t.Fatalf("status %s outcomes %v", run.Status, outcomes(run.Attempts))
	}
	if run.Attempts[0].DiagnosticsHash == "" || run.Attempts[1].DiagnosticsHash != "" {
		t.Fatal("diagnostics hash belongs to the failed attempt only")
	}
	reqs := r.prov.Requests()
	if len(reqs) != 2 || len(reqs[1].Segments) != len(reqs[0].Segments)+2 {
		t.Fatalf("repair request must add exactly the repair and diagnostics segments: %d vs %d", len(reqs[1].Segments), len(reqs[0].Segments))
	}
	repair, diags := reqs[1].Segments[len(reqs[1].Segments)-2], reqs[1].Segments[len(reqs[1].Segments)-1]
	if repair.Name != SegmentRepair || repair.Trust != TrustTrusted || repair.Text != DefaultTemplates().Repair {
		t.Fatalf("repair segment %+v", repair)
	}
	_, badHash, err := CanonicalPatch(parseOK(t, bad))
	if err != nil {
		t.Fatal(err)
	}
	if diags.Name != SegmentDiagnostics || diags.Trust != TrustUntrusted || !strings.Contains(diags.Text, badHash) {
		t.Fatalf("diagnostics segment must be UNTRUSTED data naming the previous hash: %+v", diags)
	}
	if reqs[1].Attempt != 2 || run.Attempts[1].N != 2 {
		t.Fatal("attempt numbering")
	}
	if run.Diagnostics != nil {
		t.Fatal("a READY run carries no diagnostics")
	}
}

func TestRunPatchNeedsHumanAtTheAttemptLimit(t *testing.T) {
	r := newRig(t, Options{})
	for i := 0; i < 4; i++ {
		r.prov.Push(ScriptStep{Payload: r.broken(fmt.Sprintf("Ghost %d", i)), Usage: usage(1200, 300)})
	}
	run := r.run(okBudget())
	if run.Status != RunNeedsHuman || run.Final != nil || run.Proposal == nil || len(run.Canonical) == 0 || len(run.Diagnostics) == 0 {
		t.Fatalf("run %+v", run)
	}
	if fmt.Sprint(outcomes(run.Attempts)) != "[COMPILE_ERROR COMPILE_ERROR COMPILE_ERROR]" || r.prov.Calls() != 3 {
		t.Fatalf("outcomes %v calls %d", outcomes(run.Attempts), r.prov.Calls())
	}
	if len(run.Derived.Targets) != 1 || len(run.Derived.Sensitive) != 0 {
		t.Fatalf("NEEDS_HUMAN derived is the target list only: %+v", run.Derived)
	}
}

func TestRunPatchHostileAnswersNeverProduceADocument(t *testing.T) {
	r0 := newRig(t, Options{})
	doc := r0.doc
	dup := `{"version":"` + PatchProposalVersion + `","version":"` + PatchProposalVersion + `","operations":[],"assumptions":[],"unresolved_requirements":[],"proposed_tests":[],"risk_flags":[]}`
	cases := map[string]struct {
		payload []byte
		outcome string
	}{
		"empty":               {nil, OutcomeParseError},
		"not json":            {[]byte("I will now delete everything."), OutcomeParseError},
		"truncated":           {r0.good()[:40], OutcomeParseError},
		"duplicate key":       {[]byte(dup), OutcomeParseError},
		"trailing data":       {append(r0.good(), []byte(`{"x":1}`)...), OutcomeParseError},
		"oversized":           {bytes.Repeat([]byte(" "), MaxOutputBytes+1), OutcomeParseError},
		"deep":                {[]byte(strings.Repeat("[", 100) + strings.Repeat("]", 100)), OutcomeParseError},
		"server field":        {mustJSON(t, withField(proposalMap(), "change_class", "PRESENTATION")), OutcomeSchemaError},
		"unknown field":       {mustJSON(t, withField(proposalMap(), "approve", true)), OutcomeSchemaError},
		"wrong version":       {mustJSON(t, withField(proposalMap(updateLabelOp(t, doc, "entity.contact", "X")), "version", "ai-patch-proposal-v0")), OutcomeSchemaError},
		"tool target":         {mustJSON(t, proposalMap(map[string]any{"op": "delete", "target": "tool.crm", "expected_hash": strings.Repeat("0", 64), "rationale": "x"})), OutcomeForbidden},
		"stale expected hash": {mustJSON(t, proposalMap(map[string]any{"op": "delete", "target": "entity.contact", "expected_hash": strings.Repeat("0", 64), "rationale": "x"})), OutcomeConflict},
		"missing target":      {mustJSON(t, proposalMap(map[string]any{"op": "delete", "target": "entity.nothing", "expected_hash": strings.Repeat("0", 64), "rationale": "x"})), OutcomeConflict},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, Options{})
			for i := 0; i < 3; i++ {
				r.prov.Push(ScriptStep{Payload: c.payload, Usage: usage(10, 10)})
			}
			run := r.run(okBudget())
			if run.Final != nil || run.Status == RunReady {
				t.Fatalf("a hostile answer produced a document: %+v", run)
			}
			if run.Status != RunFailed && run.Status != RunNeedsHuman {
				t.Fatalf("status %s", run.Status)
			}
			for _, o := range outcomes(run.Attempts) {
				if o != c.outcome {
					t.Fatalf("outcome %s, want %s (all: %v)", o, c.outcome, outcomes(run.Attempts))
				}
			}
			if r.prov.Calls() != MaxAttempts {
				t.Fatalf("%d provider calls, want exactly %d", r.prov.Calls(), MaxAttempts)
			}
			if len(run.Diagnostics) == 0 {
				t.Fatal("the last problems must be reported")
			}
		})
	}
}

func withField(m map[string]any, k string, v any) map[string]any {
	m[k] = v
	return m
}

func TestRunPatchProviderFailures(t *testing.T) {
	cases := []struct {
		name    string
		steps   []ScriptStep
		status  RunStatus
		calls   int
		outcome string // outcome of the last attempt
	}{
		{"refused", []ScriptStep{{Err: ErrProviderRefused}, {Err: ErrProviderRefused}, {Err: ErrProviderRefused}}, RunFailed, 1, OutcomeProviderError},
		{"malformed provider", []ScriptStep{{Err: ErrProviderMalformed}, {Err: ErrProviderMalformed}}, RunFailed, 1, OutcomeProviderError},
		{"untyped", []ScriptStep{{Err: errors.New("boom")}, {Err: errors.New("boom")}}, RunFailed, 1, OutcomeProviderError},
		{"unavailable always: one retry", []ScriptStep{{Err: ErrProviderUnavailable}, {Err: ErrProviderUnavailable}, {Err: ErrProviderUnavailable}}, RunFailed, 2, OutcomeProviderError},
		{"timeout is not retried", []ScriptStep{{Err: ErrProviderTimeout}, {Err: ErrProviderTimeout}}, RunFailed, 1, OutcomeTimeout},
		{"script exhausted", nil, RunFailed, 1, OutcomeProviderError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, Options{}, c.steps...)
			run := r.run(okBudget())
			if run.Status != c.status || r.prov.Calls() != c.calls || run.Final != nil || len(run.Attempts) != c.calls {
				t.Fatalf("status %s calls %d attempts %d", run.Status, r.prov.Calls(), len(run.Attempts))
			}
			if got := run.Attempts[len(run.Attempts)-1].Outcome; got != c.outcome {
				t.Fatalf("last outcome %s, want %s", got, c.outcome)
			}
		})
	}
}

func TestRunPatchTransientFailureThenSuccess(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Err: ErrProviderUnavailable}, ScriptStep{Payload: r.good(), Usage: usage(10, 10)})
	run := r.run(okBudget())
	if run.Status != RunReady || fmt.Sprint(outcomes(run.Attempts)) != "[PROVIDER_ERROR OK]" || r.prov.Calls() != 2 {
		t.Fatalf("status %s outcomes %v", run.Status, outcomes(run.Attempts))
	}
	// A retry after a transient failure repeats the request: it carries no repair segment.
	reqs := r.prov.Requests()
	if len(reqs[1].Segments) != len(reqs[0].Segments) {
		t.Fatal("a transient retry must not claim the previous answer was rejected")
	}
}

func TestRunPatchRecoversFromAProviderPanic(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Func: func(ProviderRequest) ([]byte, error) { panic("adapter bug") }})
	run := r.run(okBudget())
	if run.Status != RunFailed || r.prov.Calls() != 1 || run.Attempts[0].Outcome != OutcomeProviderError {
		t.Fatalf("run %+v", run)
	}
}

func TestRunPatchBudgets(t *testing.T) {
	t.Run("zero value budget fails closed", func(t *testing.T) {
		r := newRig(t, Options{}, ScriptStep{Payload: nil})
		run := r.run(Budget{})
		if run.Status != RunBudgetExceeded || r.prov.Calls() != 0 || len(run.Attempts) != 0 {
			t.Fatalf("run %+v calls %d", run.Status, r.prov.Calls())
		}
	})
	t.Run("negative tokens fail closed", func(t *testing.T) {
		r := newRig(t, Options{}, ScriptStep{Payload: nil})
		b := okBudget()
		b.TokensRemaining = -5
		if run := r.run(b); run.Status != RunBudgetExceeded || r.prov.Calls() != 0 {
			t.Fatalf("status %s", run.Status)
		}
	})
	t.Run("a call larger than the remaining budget stops the loop", func(t *testing.T) {
		r := newRig(t, Options{})
		for i := 0; i < 3; i++ {
			r.prov.Push(ScriptStep{Payload: r.broken(fmt.Sprintf("G%d", i)), Usage: usage(1200, 300)})
		}
		b := okBudget()
		probe := r.patchInput(b)
		req, _ := r.g.(*gateway).request(runSpec{task: TaskApplicationPatch, context: probe.Context, schema: mustSchema(t, r.doc)}, DefaultTemplates(), 1, b.MaxOutputTokens, nil)
		raw, _ := json.Marshal(req)
		// Enough for exactly one call: its pessimistic need plus a little, then less than a second call needs.
		b.TokensRemaining = estimateTokens(len(raw)) + int64(b.MaxOutputTokens) + 100
		run := r.run(b)
		if run.Status != RunBudgetExceeded || r.prov.Calls() != 1 || len(run.Attempts) != 1 {
			t.Fatalf("status %s calls %d attempts %d", run.Status, r.prov.Calls(), len(run.Attempts))
		}
	})
	t.Run("attempts: the server ceiling wins and a lower value is honoured", func(t *testing.T) {
		for _, c := range []struct{ asked, calls int }{{100, 3}, {1, 1}, {2, 2}, {0, 3}, {-4, 3}} {
			r := newRig(t, Options{})
			for i := 0; i < 5; i++ {
				r.prov.Push(ScriptStep{Payload: r.broken(fmt.Sprintf("G%d", i)), Usage: usage(10, 10)})
			}
			b := okBudget()
			b.MaxAttempts = c.asked
			if run := r.run(b); r.prov.Calls() != c.calls || len(run.Attempts) != c.calls {
				t.Fatalf("asked %d: %d calls", c.asked, r.prov.Calls())
			}
		}
	})
	t.Run("gateway option lowers attempts", func(t *testing.T) {
		r := newRig(t, Options{MaxAttempts: 2})
		for i := 0; i < 5; i++ {
			r.prov.Push(ScriptStep{Payload: r.broken(fmt.Sprintf("G%d", i)), Usage: usage(10, 10)})
		}
		r.run(okBudget())
		if r.prov.Calls() != 2 {
			t.Fatal("calls", r.prov.Calls())
		}
	})
	t.Run("wall clock ends a slow provider", func(t *testing.T) {
		r := newRig(t, Options{}, ScriptStep{Payload: nil, Delay: 5 * time.Second}, ScriptStep{Payload: nil, Delay: 5 * time.Second})
		b := okBudget()
		b.WallClock = 80 * time.Millisecond
		start := time.Now()
		run := r.run(b)
		if time.Since(start) > 2*time.Second {
			t.Fatal("the wall clock was not honoured")
		}
		if run.Status != RunFailed || run.Attempts[0].Outcome != OutcomeTimeout || r.prov.Calls() != 1 {
			t.Fatalf("status %s outcomes %v calls %d", run.Status, outcomes(run.Attempts), r.prov.Calls())
		}
		if run.Usage.InputTokens == 0 {
			t.Fatal("a timed-out call is charged its input estimate")
		}
	})
	t.Run("a canceled parent context is an error", func(t *testing.T) {
		r := newRig(t, Options{})
		r.prov.Push(ScriptStep{Payload: r.good()})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.g.RunPatch(ctx, r.patchInput(okBudget())); !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
		if r.prov.Calls() != 0 {
			t.Fatal("a canceled run called the provider")
		}
	})
	t.Run("an expired parent deadline is a budget outcome, not an error", func(t *testing.T) {
		r := newRig(t, Options{}, ScriptStep{Payload: nil, Delay: 2 * time.Second})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		run, err := r.g.RunPatch(ctx, r.patchInput(okBudget()))
		if err != nil || run.Status == RunReady {
			t.Fatalf("status %s err %v", run.Status, err)
		}
	})
}

func mustSchema(t *testing.T, doc metadata.Document) json.RawMessage {
	t.Helper()
	s, err := PatchOutputSchema(doc.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunPatchUsageAccounting(t *testing.T) {
	big := bytes.Repeat([]byte(" "), 40_000)
	cases := []struct {
		name     string
		reported Usage
		payload  []byte
		min, max int64
	}{
		{"reported", usage(5000, 800), nil, 5800, 100_000},
		{"reports nothing: pessimistic estimate", usage(0, 0), big, 10_000, 1 << 40},
		{"under-reports a large answer", usage(1, 1), big, 10_000, 1 << 40},
		{"negative numbers do not refill the budget", usage(-1_000_000, -1_000_000), nil, 0, 1 << 40},
		// A report above twice the request (its bytes plus the output asked for) is a fault, not a measurement (M14R-06): the
		// charge stays at the order of the reservation instead of the 100 million that locked a tenant out for the day.
		{"huge numbers are capped near the size of the request, the sum does not wrap", usage(1<<62, 1<<62), nil, 0, 100_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, Options{})
			payload := c.payload
			if payload == nil {
				payload = r.good()
			}
			r.prov.Push(ScriptStep{Payload: payload, Usage: c.reported}, ScriptStep{Payload: r.good(), Usage: usage(10, 10)})
			run := r.run(okBudget())
			total := run.Usage.InputTokens + run.Usage.OutputTokens
			if run.Usage.InputTokens < 0 || run.Usage.OutputTokens < 0 || total < c.min || total > c.max {
				t.Fatalf("usage %+v, want total in [%d,%d]", run.Usage, c.min, c.max)
			}
			var sum int64
			for _, a := range run.Attempts {
				if a.Usage.InputTokens < 0 || a.Usage.OutputTokens < 0 {
					t.Fatal("negative attempt usage")
				}
				sum += a.Usage.InputTokens + a.Usage.OutputTokens
			}
			if sum != total {
				t.Fatalf("attempt usage %d does not add up to the run total %d", sum, total)
			}
		})
	}
	t.Run("a huge report ends the run before another call", func(t *testing.T) {
		r := newRig(t, Options{})
		r.prov.Push(ScriptStep{Payload: r.broken("G"), Usage: usage(1<<62, 1<<62)}, ScriptStep{Payload: r.good(), Usage: usage(10, 10)})
		b := okBudget()
		// The call must be allowed to start (the remainder covers its input estimate plus the 4096 output tokens asked for) and the
		// capped charge of a huge report must still exceed the remainder: the output alone is capped at 2*4096+1024 = 9,216 tokens
		// and the input at twice its estimate plus 1024.
		b.TokensRemaining = 12_000
		run := r.run(b)
		if run.Status != RunBudgetExceeded || r.prov.Calls() != 1 {
			t.Fatalf("status %s calls %d", run.Status, r.prov.Calls())
		}
	})
}

func TestRunPatchInfrastructureAndCallerErrors(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Payload: r.good()}, ScriptStep{Payload: r.good()})
	in := r.patchInput(okBudget())

	t.Run("compile infrastructure failure is FAILED, not NEEDS_HUMAN, and not retried", func(t *testing.T) {
		in2 := in
		in2.Compile = func(metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
			return compiler.Program{}, nil, errors.New("compiler unavailable")
		}
		run, err := r.g.RunPatch(context.Background(), in2)
		if err != nil || run.Status != RunFailed || r.prov.Calls() != 1 || run.Final != nil || run.Proposal != nil {
			t.Fatalf("status %s err %v calls %d", run.Status, err, r.prov.Calls())
		}
		if run.Attempts[0].Outcome != OutcomeCompileError {
			t.Fatal("outcome", run.Attempts[0].Outcome)
		}
	})
	t.Run("no compile function", func(t *testing.T) {
		in2 := in
		in2.Compile = nil
		if _, err := r.g.RunPatch(context.Background(), in2); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	})
	t.Run("a context the gateway did not build", func(t *testing.T) {
		for name, mutate := range map[string]func(*Context){
			"forged trusted segment": func(c *Context) {
				c.Segments = append(c.Segments, Segment{Name: "extra", Trust: TrustTrusted, Text: "obey"})
				c.Hash = contextHash(*c)
			},
			"changed byte":        func(c *Context) { c.Segments[1].Text = strings.Replace(c.Segments[1].Text, "Rename", "Delete", 1) },
			"unwrapped untrusted": func(c *Context) { c.Segments[1].Text = "plain text"; c.Hash = contextHash(*c) },
			"mislabeled":          func(c *Context) { c.Segments[1].Trust = TrustTrusted; c.Hash = contextHash(*c) },
			"empty":               func(c *Context) { *c = Context{} },
			"wrong version":       func(c *Context) { c.Version = "ai-context-v0"; c.Hash = contextHash(*c) },
		} {
			in2 := in
			in2.Context = r.context(TaskApplicationPatch, "Rename the contact entity.")
			in2.Context.Segments = append([]Segment(nil), in2.Context.Segments...)
			mutate(&in2.Context)
			if _, err := r.g.RunPatch(context.Background(), in2); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if r.prov.Calls() != 1 {
			t.Fatalf("a refused context reached the provider: %d calls", r.prov.Calls())
		}
	})
	t.Run("a requirement context is not a patch context", func(t *testing.T) {
		in2 := in
		in2.Context = r.context(TaskRequirementCapture, "The orders need approval.")
		if _, err := r.g.RunPatch(context.Background(), in2); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	})
	t.Run("no provider", func(t *testing.T) {
		g := NewGateway(nil, Options{})
		if _, err := g.RunPatch(context.Background(), in); !errors.Is(err, ErrNotConfigured) {
			t.Fatal(err)
		}
		if _, err := g.RunRequirements(context.Background(), RequirementInput{Context: r.context(TaskRequirementCapture, "x"), Budget: okBudget()}); !errors.Is(err, ErrNotConfigured) {
			t.Fatal(err)
		}
	})
	t.Run("unsupported document version", func(t *testing.T) {
		in2 := in
		in2.Draft.SchemaVersion = "9.9.9"
		if _, err := r.g.RunPatch(context.Background(), in2); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	})
}

func TestRunPatchIsDeterministic(t *testing.T) {
	var runs [2]PatchRun
	for i := range runs {
		r := newRig(t, Options{})
		r.prov.Push(ScriptStep{Payload: r.broken("G"), Usage: usage(1200, 300), Model: "m"}, ScriptStep{Payload: r.good(), Usage: usage(1300, 200), Model: "m"})
		runs[i] = r.run(okBudget())
	}
	a, b := runs[0], runs[1]
	if a.Hash != b.Hash || !bytes.Equal(a.Canonical, b.Canonical) || a.Status != b.Status || a.Usage != b.Usage {
		t.Fatal("two runs over the same bytes differ")
	}
	if !bytes.Equal(mustJSON(t, a.Attempts), mustJSON(t, b.Attempts)) {
		t.Fatalf("traces differ:\n%+v\n%+v", a.Attempts, b.Attempts)
	}
	if !bytes.Equal(mustJSON(t, a.Derived), mustJSON(t, b.Derived)) {
		t.Fatal("derived facts differ")
	}
}

func TestRunPatchUsesTheInjectedClockForLatency(t *testing.T) {
	r := newRig(t, Options{})
	tick := newFakeClock()
	r.prov.Push(ScriptStep{Func: func(ProviderRequest) ([]byte, error) { tick.Advance(250 * time.Millisecond); return r.good(), nil }})
	in := r.patchInput(okBudget())
	in.Now = tick.Now
	run, err := r.g.RunPatch(context.Background(), in)
	if err != nil || run.Status != RunReady || run.Attempts[0].Latency != 250*time.Millisecond {
		t.Fatalf("latency %v status %s err %v", run.Attempts[0].Latency, run.Status, err)
	}
}

func TestRunPatchIsSafeForConcurrentUse(t *testing.T) {
	r := newRig(t, Options{})
	good := r.good()
	prov := NewScriptedProvider()
	for i := 0; i < 64; i++ {
		prov.Push(ScriptStep{Payload: good, Usage: usage(100, 100)})
	}
	g := NewGateway(prov, Options{})
	var wg sync.WaitGroup
	hashes := make([]string, 16)
	for i := range hashes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := g.BuildContext(ContextInput{Task: TaskApplicationPatch, Request: "Rename the contact entity.", Draft: r.doc, DraftRevision: 3, DraftHash: testDraftHash})
			if err != nil {
				t.Error(err)
				return
			}
			run, err := g.RunPatch(context.Background(), PatchInput{Context: c, Draft: r.doc, Compile: newCompiler(t).Compile, Budget: okBudget()})
			if err != nil || run.Status != RunReady {
				t.Errorf("status %s err %v", run.Status, err)
				return
			}
			hashes[i] = run.Hash
		}()
	}
	wg.Wait()
	for _, h := range hashes {
		if h != hashes[0] {
			t.Fatal("concurrent runs over the same bytes disagree")
		}
	}
}

func TestRunPatchSensitiveChangeIsListed(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Payload: mustJSON(t, proposalMap(updateLabelOp(t, r.doc, "rule.contact.named", "Is adult (edited)"))), Usage: usage(10, 10)})
	run := r.run(okBudget())
	if run.Status != RunReady || len(run.Derived.Sensitive) != 0 {
		t.Fatalf("a label change is presentation only: status %s sensitive %v", run.Status, run.Derived.Sensitive)
	}
	r2 := newRig(t, Options{})
	rel := defByID(t, r2.doc, "relationship.contact.parent")
	removal := map[string]any{"op": "delete", "target": rel.ID, "expected_hash": metadata.DefinitionHash(rel), "rationale": "Remove."}
	r2.prov.Push(ScriptStep{Payload: mustJSON(t, proposalMap(removal)), Usage: usage(10, 10)})
	run2 := r2.run(okBudget())
	if run2.Status != RunReady || len(run2.Derived.Sensitive) == 0 || !strings.HasPrefix(run2.Derived.Sensitive[0], "relationship.contact.parent:") {
		t.Fatalf("status %s sensitive %v", run2.Status, run2.Derived.Sensitive)
	}
}

// ------------------------------------------------------------------ requirements

func reqPayload(t *testing.T, mutate func(*RequirementProposal)) []byte {
	t.Helper()
	sem, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: "500", ApproverRoles: []string{"admin"}})
	p := RequirementProposal{Version: RequirementProposalVersion, Requirements: []RequirementDraft{{ID: "po-approval", Category: intent.CategoryApprovalThreshold, Statement: "Orders above 500 USD need admin approval.", Semantics: sem}},
		OpenQuestions: []UnresolvedRequirement{}, ProposedTests: []intent.AcceptanceCase{}, Assumptions: []string{}}
	if mutate != nil {
		mutate(&p)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunRequirements(t *testing.T) {
	newGW := func(steps ...ScriptStep) (Gateway, *ScriptedProvider, Context) {
		prov := NewScriptedProvider(steps...)
		g := NewGateway(prov, Options{Clock: newFakeClock().Now})
		c, err := g.BuildContext(ContextInput{Task: TaskRequirementCapture, Request: "Orders above 500 USD need admin approval.", Draft: fixtureDoc(t, exWorkflow), DraftRevision: 1, DraftHash: testDraftHash})
		if err != nil {
			t.Fatal(err)
		}
		return g, prov, c
	}
	t.Run("first try", func(t *testing.T) {
		g, _, c := newGW(ScriptStep{Payload: reqPayload(t, nil), Usage: usage(900, 200)})
		run, err := g.RunRequirements(context.Background(), RequirementInput{Context: c, Budget: okBudget()})
		if err != nil || run.Status != RunReady || run.Proposal == nil || run.Hash != sha256Hex(run.Canonical) || len(run.Proposal.Requirements) != 1 {
			t.Fatalf("run %+v err %v", run, err)
		}
		if _, err = ParseRequirementProposal(run.Canonical); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("an unknown category is refused, never resolved, and repaired as an open question", func(t *testing.T) {
		bad := reqPayload(t, func(p *RequirementProposal) {
			p.Requirements[0].Category = "pricing"
			p.Requirements[0].Semantics = json.RawMessage(`{}`)
		})
		good := reqPayload(t, func(p *RequirementProposal) {
			p.Requirements = []RequirementDraft{}
			p.OpenQuestions = []UnresolvedRequirement{{ID: "q1", Question: "How is pricing decided?", Category: "pricing"}}
		})
		g, prov, c := newGW(ScriptStep{Payload: bad, Usage: usage(900, 200)}, ScriptStep{Payload: good, Usage: usage(900, 200)})
		run, err := g.RunRequirements(context.Background(), RequirementInput{Context: c, Budget: okBudget()})
		if err != nil || run.Status != RunReady || fmt.Sprint(outcomes(run.Attempts)) != "[SCHEMA_ERROR OK]" || len(run.Proposal.Requirements) != 0 || len(run.Proposal.OpenQuestions) != 1 {
			t.Fatalf("run %+v err %v", run, err)
		}
		reqs := prov.Requests()
		if !strings.Contains(reqs[1].Segments[len(reqs[1].Segments)-1].Text, "invalid_semantics") {
			t.Fatal("the diagnostic must say why")
		}
	})
	t.Run("semantics that never validate end in NEEDS_HUMAN with diagnostics", func(t *testing.T) {
		bad := reqPayload(t, func(p *RequirementProposal) { p.Requirements[0].Semantics = json.RawMessage(`{"workflow_id":"x"}`) })
		g, prov, c := newGW(ScriptStep{Payload: bad}, ScriptStep{Payload: bad}, ScriptStep{Payload: bad})
		run, err := g.RunRequirements(context.Background(), RequirementInput{Context: c, Budget: okBudget()})
		if err != nil || run.Status != RunNeedsHuman || run.Proposal != nil || len(run.Diagnostics) == 0 || prov.Calls() != 3 {
			t.Fatalf("status %s err %v calls %d", run.Status, err, prov.Calls())
		}
	})
	t.Run("malformed output is FAILED", func(t *testing.T) {
		g, _, c := newGW(ScriptStep{Payload: []byte("nope")}, ScriptStep{Payload: []byte("nope")}, ScriptStep{Payload: []byte("nope")})
		run, err := g.RunRequirements(context.Background(), RequirementInput{Context: c, Budget: okBudget()})
		if err != nil || run.Status != RunFailed {
			t.Fatalf("status %s err %v", run.Status, err)
		}
	})
	t.Run("zero budget", func(t *testing.T) {
		g, prov, c := newGW(ScriptStep{Payload: reqPayload(t, nil)})
		run, err := g.RunRequirements(context.Background(), RequirementInput{Context: c})
		if err != nil || run.Status != RunBudgetExceeded || prov.Calls() != 0 {
			t.Fatalf("status %s err %v", run.Status, err)
		}
	})
	t.Run("a patch context is not a requirement context", func(t *testing.T) {
		g, _, _ := newGW()
		pc, err := g.BuildContext(ContextInput{Task: TaskApplicationPatch, Request: "x", Draft: fixtureDoc(t, exWorkflow)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = g.RunRequirements(context.Background(), RequirementInput{Context: pc, Budget: okBudget()}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	})
}
