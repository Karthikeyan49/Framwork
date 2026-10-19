package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai/corpus"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// The unit-level attack suite (M14 adversarial plan, ATT-* rows). Every item of the
// corpus runs through the real context builder, the real gateway with a ScriptedProvider,
// the real strict parser, ApplyProposal, the real compiler and DeriveSensitiveAspects;
// the property each item declares is checked by internal/ai/corpus. This is builder-side
// evidence from scripted bytes: it says nothing about any real model, and it is not an
// independent review.

const corpusDir = "testdata/corpus"

var (
	libOnce sync.Once
	lib     *corpus.Library
	libErr  error
)

func loadCorpus(t testing.TB) *corpus.Library {
	t.Helper()
	libOnce.Do(func() { lib, libErr = corpus.Load(corpusDir) })
	if libErr != nil {
		t.Fatal(libErr)
	}
	return lib
}

var wrapper = corpus.Wrapper{Begin: wrapperBegin, End: wrapperEnd}

// corpusCompile is the real validator and compiler with the case's catalog.
func corpusCompile(t testing.TB, cat compiler.Catalog) func(metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
	t.Helper()
	v, err := sharedValidator() // cached: compiling the schemas for every case is the slow part
	if err != nil {
		t.Fatal(err)
	}
	return func(doc metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
		raw, err := json.Marshal(doc)
		if err != nil {
			return compiler.Program{}, nil, err
		}
		src, err := v.Parse(raw)
		if err != nil {
			var invalid *metadata.Invalid
			if errors.As(err, &invalid) {
				out := []compiler.Diagnostic{}
				for _, i := range invalid.Issues {
					out = append(out, compiler.Diagnostic{Path: i.Path, Code: i.Code, Message: i.Message})
				}
				return compiler.Program{}, out, nil
			}
			return compiler.Program{}, []compiler.Diagnostic{{Code: "invalid_source", Message: "A validated source is required"}}, nil
		}
		p, ds := compiler.Compile(src, cat)
		if len(ds) > 0 {
			return compiler.Program{}, ds, nil
		}
		return p, nil, nil
	}
}

// minScriptSteps pads a short script: a provider that was scripted once keeps giving
// that answer, so a one-step item still exercises the whole attempt budget instead of
// ending at "script exhausted". The padding repeats the last step only.
func paddedPayloads(c corpus.Case) []corpus.ExpandedStep {
	out := append([]corpus.ExpandedStep(nil), c.Payloads...)
	for len(out) > 0 && len(out) < MaxAttempts {
		out = append(out, out[len(out)-1])
	}
	return out
}

func scriptStepsOf(c corpus.Case) []ScriptStep {
	payloads := paddedPayloads(c)
	steps := make([]ScriptStep, 0, len(payloads))
	for _, p := range payloads {
		s := ScriptStep{Payload: p.Payload, Usage: Usage{InputTokens: p.Step.Usage.InputTokens, OutputTokens: p.Step.Usage.OutputTokens}, Model: p.Step.Model, Delay: time.Duration(p.Step.DelayMS) * time.Millisecond}
		switch p.Step.Error {
		case "":
		case "refused":
			s.Err = ErrProviderRefused
		case "unavailable":
			s.Err = ErrProviderUnavailable
		case "timeout":
			s.Err = ErrProviderTimeout
		case "malformed":
			s.Err = ErrProviderMalformed
		case "other":
			s.Err = errors.New("unclassified adapter failure")
		case "panic":
			s.Func = func(ProviderRequest) ([]byte, error) { panic("adapter bug") }
		}
		steps = append(steps, s)
	}
	return steps
}

func toolsOf(in []corpus.Tool) []ToolSummary {
	out := make([]ToolSummary, 0, len(in))
	for _, t := range in {
		out = append(out, ToolSummary{Ref: t.Ref, Version: t.Version, SideEffect: t.SideEffect, Approval: t.Approval, Input: t.Input, Output: t.Output})
	}
	return out
}

func taskOf(c corpus.Case) Task {
	if c.Item.Task == string(TaskRequirementCapture) {
		return TaskRequirementCapture
	}
	return TaskApplicationPatch
}

func segsOf(c Context) []corpus.Seg {
	out := make([]corpus.Seg, len(c.Segments))
	for i, s := range c.Segments {
		out[i] = corpus.Seg{Name: s.Name, Trust: string(s.Trust), Text: s.Text}
	}
	return out
}

func budgetOf(c corpus.Case) Budget {
	b := Budget{MaxAttempts: 3, MaxOutputTokens: 4096, WallClock: 5 * time.Second, TokensRemaining: 1_000_000}
	if s := c.Item.Budget; s != nil {
		switch {
		case s.ZeroValue:
			return Budget{}
		}
		if s.WallClockMS > 0 {
			b.WallClock = time.Duration(s.WallClockMS) * time.Millisecond
		}
		if s.MaxAttempts != 0 {
			b.MaxAttempts = s.MaxAttempts
		}
		if s.TokensRemaining != nil {
			b.TokensRemaining = *s.TokensRemaining
		}
	}
	return b
}

func contextErrClass(err error) string {
	switch {
	case errors.Is(err, ErrContextTooLarge):
		return "too_large"
	case errors.Is(err, ErrInvalidInput):
		return "invalid"
	}
	return "other"
}

func baselineContext(t testing.TB, g Gateway, c corpus.Case) []corpus.Seg {
	t.Helper()
	clean, err := g.BuildContext(ContextInput{Task: taskOf(c), Request: "Rename the order list page to All orders.", Draft: c.Clean, DraftRevision: 1, DraftHash: c.DraftHash})
	if err != nil {
		// The wide fixture cannot even build its baseline; use the orders fixture's shape.
		return nil
	}
	return segsOf(clean)
}

// runCorpusCase runs one expanded case through a gateway built over provider and
// returns the observable result. The provider is the only thing that differs between
// the scripted run and the HTTP run of the provider-swap evaluation.
func runCorpusCase(t testing.TB, c corpus.Case, provider Provider, calls func() int, requests func() []ProviderRequest) corpus.Result {
	t.Helper()
	g := NewGateway(provider, Options{})
	roles := append([]string{"owner", "admin", "member", "viewer"}, c.Roles...)
	in := ContextInput{Task: taskOf(c), Request: c.Request, Draft: c.Draft, DraftRevision: 1, DraftHash: c.DraftHash, Tools: toolsOf(c.Tools), RoleNames: roles, Requirements: c.Reqs}
	var res corpus.Result
	res.Baseline = baselineContext(t, g, c)
	bundle, err := g.BuildContext(in)
	if err != nil {
		res.ContextErr = contextErrClass(err)
		return res
	}
	res.Context = segsOf(bundle)
	real := corpusCompile(t, c.Catalog)
	compile := c.CompileFunc(real)
	budget := budgetOf(c)
	started := time.Now()
	var attempts []Attempt
	var usage Usage
	switch taskOf(c) {
	case TaskApplicationPatch:
		run, err := g.RunPatch(context.Background(), PatchInput{Context: bundle, Draft: c.Draft, Compile: compile, Budget: budget})
		res.Elapsed = time.Since(started)
		if err != nil {
			res.RunError = err.Error()
		}
		res.Status, attempts, usage = string(run.Status), run.Attempts, run.Usage
		res.Canonical = string(run.Canonical)
		res.Document = run.Final != nil
		res.Sensitive = run.Derived.Sensitive
		for _, d := range run.Diagnostics {
			res.DiagCodes = append(res.DiagCodes, strings.ToLower(d.Code))
		}
		if run.Final != nil {
			res.Oracle = oracleAspects(t, real, c.Draft, *run.Final)
		}
	default:
		run, err := g.RunRequirements(context.Background(), RequirementInput{Context: bundle, Budget: budget})
		res.Elapsed = time.Since(started)
		if err != nil {
			res.RunError = err.Error()
		}
		res.Status, attempts, usage = string(run.Status), run.Attempts, run.Usage
		res.Canonical = string(run.Canonical)
		for _, d := range run.Diagnostics {
			res.DiagCodes = append(res.DiagCodes, strings.ToLower(d.Code))
		}
	}
	for _, a := range attempts {
		res.Outcomes = append(res.Outcomes, a.Outcome)
		if a.Usage.InputTokens < 0 || a.Usage.OutputTokens < 0 {
			res.NegTokens = true
		}
	}
	res.Tokens = usage.InputTokens + usage.OutputTokens
	res.Calls = calls()
	if reqs := requests(); len(reqs) > 1 {
		base := len(bundle.Segments)
		for _, s := range reqs[len(reqs)-1].Segments[min(base, len(reqs[len(reqs)-1].Segments)):] {
			res.Feedback = append(res.Feedback, corpus.Seg{Name: s.Name, Trust: string(s.Trust), Text: s.Text})
		}
	}
	return res
}

// oracleAspects diffs the draft program with the final program straight through the
// compiler, independently of DeriveSensitiveAspects.
func oracleAspects(t testing.TB, compile func(metadata.Document) (compiler.Program, []compiler.Diagnostic, error), before, after metadata.Document) map[string][]string {
	t.Helper()
	var base compiler.Program
	if p, ds, err := compile(before); err == nil && len(ds) == 0 && p.Valid() {
		base = p
	}
	final, ds, err := compile(after)
	if err != nil || len(ds) > 0 || !final.Valid() {
		t.Fatalf("the document the gateway returned does not compile: %v %v", err, ds)
	}
	diff, err := compiler.ComputeDiff(base, final)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	out := map[string][]string{}
	for _, c := range diff.Changes {
		out[c.ID] = append(out[c.ID], c.Aspects...)
	}
	return out
}

func scriptedRun(t testing.TB, c corpus.Case) corpus.Result {
	t.Helper()
	prov := NewScriptedProvider(scriptStepsOf(c)...)
	return runCorpusCase(t, c, prov, prov.Calls, prov.Requests)
}

// TestAdversarialCorpus runs all 412 items through the scripted pipeline. A failure
// quotes the item, the class and the severity the item assigns.
func TestAdversarialCorpus(t *testing.T) {
	l := loadCorpus(t)
	var mu sync.Mutex
	notes := map[string][]string{}
	for _, it := range l.Items {
		it := it
		t.Run(it.ID, func(t *testing.T) {
			if testing.Short() && !it.Tagged("swap") {
				t.Skip("short mode")
			}
			c, err := l.Expand(it)
			if err != nil {
				t.Fatal(err)
			}
			res := scriptedRun(t, c)
			rep := c.Check(res, wrapper)
			mu.Lock()
			if len(rep.Notes) > 0 {
				notes[it.ID] = rep.Notes
			}
			mu.Unlock()
			for _, f := range rep.Failures {
				sev := it.Expect.Severity
				if sev == "" {
					sev = "HIGH"
				}
				t.Errorf("[%s] %s: %s (status %s, outcomes %v, calls %d)", sev, it.Class+"/"+it.Subclass, f, res.Status, res.Outcomes, res.Calls)
			}
		})
	}
	t.Cleanup(func() {
		keys := make([]string, 0, len(notes))
		for k := range notes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Logf("note %s: %s", k, strings.Join(notes[k], " | "))
		}
	})
	_ = fmt.Sprint
}
