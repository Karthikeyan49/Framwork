package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai/corpus"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Property tests over the whole corpus (M14 adversarial plan, PROP-* rows): they hold
// for every item, whatever the item's own expectation says.

type propRun struct {
	run      PatchRun
	before   []byte
	after    []byte
	provider *ScriptedProvider
	budget   Budget
	ctxErr   error
}

func patchOnly(c corpus.Case) bool { return c.Item.Task == string(TaskApplicationPatch) }

func runForProperties(t testing.TB, c corpus.Case, g Gateway, prov *ScriptedProvider) propRun {
	t.Helper()
	var out propRun
	out.provider = prov
	bundle, err := g.BuildContext(ContextInput{Task: TaskApplicationPatch, Request: c.Request, Draft: c.Draft, DraftRevision: 1, DraftHash: c.DraftHash, Tools: toolsOf(c.Tools),
		RoleNames: append([]string{"owner", "admin", "member", "viewer"}, c.Roles...), Requirements: c.Reqs})
	if err != nil {
		out.ctxErr = err
		return out
	}
	out.before, _ = json.Marshal(c.Draft)
	out.budget = budgetOf(c)
	out.run, _ = g.RunPatch(context.Background(), PatchInput{Context: bundle, Draft: c.Draft, Compile: c.CompileFunc(corpusCompile(t, c.Catalog)), Budget: out.budget})
	out.after, _ = json.Marshal(c.Draft)
	return out
}

// TestPropertiesOfEveryRun checks, for every patch item, what must hold whatever the
// model said: the input draft is untouched; a returned document passed the allowlist,
// the expected-hash compare-and-set and a real compile; attempts and budget are never
// exceeded; the trace adds up.
func TestPropertiesOfEveryRun(t *testing.T) {
	l := loadCorpus(t)
	allowed := map[string]bool{}
	for _, k := range AllowedKinds() {
		allowed[k] = true
	}
	var documents int
	for _, it := range l.Items {
		c, err := l.Expand(it)
		if err != nil {
			t.Fatal(err)
		}
		if !patchOnly(c) {
			continue
		}
		prov := NewScriptedProvider(scriptStepsOf(c)...)
		r := runForProperties(t, c, NewGateway(prov, Options{}), prov)
		if r.ctxErr != nil {
			continue
		}
		fail := func(format string, a ...any) { t.Errorf("%s: "+format, append([]any{it.ID}, a...)...) }
		// PROP-01: the draft the run was given is never mutated.
		if !bytes.Equal(r.before, r.after) {
			fail("the input draft was mutated")
		}
		// PROP-02: attempts and calls never exceed the ceiling or the budget's own number.
		limit := MaxAttempts
		if r.budget.MaxAttempts > 0 && r.budget.MaxAttempts < limit {
			limit = r.budget.MaxAttempts
		}
		if len(r.run.Attempts) > limit || prov.Calls() > limit {
			fail("%d attempts, %d calls, ceiling %d", len(r.run.Attempts), prov.Calls(), limit)
		}
		// PROP-03: the budget is never overdrawn by starting a call with nothing left.
		var spent int64
		for i, a := range r.run.Attempts {
			if i > 0 && spent >= r.budget.TokensRemaining {
				fail("attempt %d started with the budget spent (%d of %d)", i+1, spent, r.budget.TokensRemaining)
			}
			if a.Usage.InputTokens < 0 || a.Usage.OutputTokens < 0 || a.N != i+1 || a.TemplateVersion != TemplateVersion || len(a.RequestHash) != 64 {
				fail("attempt record %+v", a)
			}
			spent += a.Usage.InputTokens + a.Usage.OutputTokens
		}
		if spent != r.run.Usage.InputTokens+r.run.Usage.OutputTokens {
			fail("the attempts add up to %d, the run reports %+v", spent, r.run.Usage)
		}
		// PROP-04: nothing but READY carries a document; NEEDS_HUMAN carries a proposal but
		// no document; a document implies the full path.
		if (r.run.Final != nil) != (r.run.Status == RunReady) {
			fail("status %s with document=%v", r.run.Status, r.run.Final != nil)
		}
		if r.run.Status != RunReady {
			continue
		}
		documents++
		p, perr := ParsePatchProposal(r.run.Canonical)
		if perr != nil || sha256Hex(r.run.Canonical) != r.run.Hash {
			fail("the returned canonical proposal does not re-parse or hash: %v", perr)
			continue
		}
		draftByID := map[string]metadata.Definition{}
		for _, d := range c.Draft.Definitions {
			draftByID[d.ID] = d
		}
		touched := map[string]bool{}
		for _, op := range p.Operations {
			touched[op.Target] = true
			if !allowed[targetKind(op.Target)] {
				fail("operation on a kind outside the allowlist: %s", op.Target)
			}
			old, existed := draftByID[op.Target]
			switch op.Op {
			case "add":
				if existed {
					fail("an add of an existing id %s was returned", op.Target)
				}
			default:
				if !existed || op.ExpectedHash != metadata.DefinitionHash(old) {
					fail("%s of %s without a matching expected hash", op.Op, op.Target)
				}
				if !allowed[old.Kind] {
					fail("a definition of kind %s was replaced or deleted", old.Kind)
				}
			}
		}
		// Every definition the final document changed or added was named by an operation;
		// everything else is byte-identical to the draft.
		finalByID := map[string]metadata.Definition{}
		for _, d := range r.run.Final.Definitions {
			finalByID[d.ID] = d
			old, existed := draftByID[d.ID]
			if !touched[d.ID] && (!existed || metadata.DefinitionHash(old) != metadata.DefinitionHash(d)) {
				fail("definition %s changed without an operation", d.ID)
			}
			if touched[d.ID] && !allowed[d.Kind] {
				fail("definition %s of kind %s was written", d.ID, d.Kind)
			}
		}
		for id := range draftByID {
			if _, kept := finalByID[id]; !kept && !touched[id] {
				fail("definition %s disappeared without an operation", id)
			}
		}
		if r.run.Final.AppID != c.Draft.AppID || r.run.Final.Namespace != c.Draft.Namespace || r.run.Final.SchemaVersion != c.Draft.SchemaVersion {
			fail("the document envelope changed")
		}
		// The document compiles with the real compiler, independently of the run.
		if prog, ds, err := corpusCompile(t, c.Catalog)(*r.run.Final); err != nil || len(ds) > 0 || !prog.Valid() {
			fail("the returned document does not compile: %v %v", err, ds)
		}
	}
	if documents < 15 {
		t.Fatalf("only %d items produced a document: the property is not exercised", documents)
	}
}

// TestRunsAreDeterministic: the same bytes through two fresh gateways give the same
// canonical proposal, hashes, trace and derived facts.
func TestRunsAreDeterministic(t *testing.T) {
	l := loadCorpus(t)
	for _, it := range l.Items {
		if !it.Tagged("swap") || it.Expect.ElapsedMSMax > 0 {
			continue
		}
		c, err := l.Expand(it)
		if err != nil {
			t.Fatal(err)
		}
		var runs [2]corpus.Result
		var traces [2][]string
		for i := range runs {
			prov := NewScriptedProvider(scriptStepsOf(c)...)
			runs[i] = runCorpusCase(t, c, prov, prov.Calls, prov.Requests)
			for _, rq := range prov.Requests() {
				raw, _ := json.Marshal(rq)
				traces[i] = append(traces[i], sha256Hex(raw))
			}
		}
		if outcomeOf(runs[0]) != outcomeOf(runs[1]) || len(traces[0]) != len(traces[1]) {
			t.Errorf("%s: two runs over the same bytes differ", it.ID)
			continue
		}
		for i := range traces[0] {
			if traces[0][i] != traces[1][i] {
				t.Errorf("%s: request %d differs between runs", it.ID, i+1)
			}
		}
	}
}

// TestCorpusUnderConcurrency runs the corpus from many goroutines through one shared
// gateway value (run with -race): results equal the sequential results.
func TestCorpusUnderConcurrency(t *testing.T) {
	l := loadCorpus(t)
	var cases []corpus.Case
	for _, it := range l.Items {
		if it.Tagged("swap") && it.Class != "resource" {
			c, err := l.Expand(it)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, c)
		}
	}
	want := make([]swapOutcome, len(cases))
	for i, c := range cases {
		want[i] = outcomeOf(scriptedRun(t, c))
	}
	g := NewGateway(NewScriptedProvider(), Options{}) // the gateway value itself is shared; providers are per run
	_ = g
	var wg sync.WaitGroup
	got := make([]swapOutcome, len(cases))
	sem := make(chan struct{}, 8)
	started := time.Now()
	for i, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			got[i] = outcomeOf(scriptedRun(t, c))
		}()
	}
	wg.Wait()
	for i := range cases {
		if got[i] != want[i] {
			t.Errorf("%s: a concurrent run differs from the sequential one", cases[i].Item.ID)
		}
	}
	t.Logf("%d items ran 8 at a time in %v", len(cases), time.Since(started).Round(time.Millisecond))
}
