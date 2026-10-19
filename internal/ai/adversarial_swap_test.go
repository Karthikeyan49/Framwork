package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/ai/corpus"
)

// Provider-swap evaluation (ADR040 section 14): the corpus items tagged "swap" run
// through (a) the ScriptedProvider and (b) the HTTPJSONProvider against a local TLS test
// server that replays the same bytes. The validation outcomes must be identical and
// the declared properties must hold for both. This demonstrates substitutability of the
// gateway, not model quality: both providers replay scripted bytes.

type swapOutcome struct {
	Status    string
	Outcomes  string
	Calls     int
	Document  bool
	Sensitive string
	Canonical string
	DiagCodes string
}

func outcomeOf(r corpus.Result) swapOutcome {
	return swapOutcome{Status: r.Status, Outcomes: strings.Join(r.Outcomes, ","), Calls: r.Calls, Document: r.Document, Sensitive: strings.Join(r.Sensitive, ","), Canonical: r.Canonical, DiagCodes: strings.Join(r.DiagCodes, ",")}
}

// httpReplay serves the steps of a case from a TLS test server.
func httpReplay(t *testing.T, c corpus.Case) (*HTTPJSONProvider, *atomic.Int64, func()) {
	t.Helper()
	steps := paddedPayloads(c)
	var served atomic.Int64
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		i := int(served.Add(1)) - 1
		if i >= len(steps) {
			w.WriteHeader(http.StatusForbidden) // the scripted provider answers "exhausted" (a refusal) here
			return
		}
		s := steps[i]
		if s.Step.DelayMS > 0 {
			select {
			case <-time.After(time.Duration(s.Step.DelayMS) * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		switch s.Step.Error {
		case "refused":
			w.WriteHeader(http.StatusForbidden)
		case "unavailable":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "timeout":
			w.WriteHeader(http.StatusGatewayTimeout)
		case "malformed":
			_, _ = w.Write([]byte("<<not an envelope>>"))
		default:
			_, _ = w.Write(answer(string(s.Payload), s.Step.Model, Usage{InputTokens: s.Step.Usage.InputTokens, OutputTokens: s.Step.Usage.OutputTokens}))
		}
	})
	return srv.provider(t, ""), &served, srv.Close
}

// swapComparable reports why an item cannot be replayed through the JSON envelope.
func swapComparable(c corpus.Case) string {
	for _, s := range c.Payloads {
		switch {
		case s.Step.Error == "panic" || s.Step.Error == "other":
			return "an adapter panic or an untyped adapter error has no HTTP form"
		case !utf8.Valid(s.Payload):
			return "invalid UTF-8 cannot travel in a JSON envelope"
		case len(s.Payload) > MaxProviderBytes/2:
			return "larger than the response cap of the HTTP adapter"
		}
	}
	return ""
}

func TestProviderSwapEvaluation(t *testing.T) {
	l := loadCorpus(t)
	type row struct {
		name           string
		scripted, http swapOutcome
	}
	var compared, skipped, mismatched int
	var skippedWhy = map[string]int{}
	var scriptedFigures, httpFigures []EvalResult
	for _, it := range l.Items {
		if !it.Tagged("swap") {
			continue
		}
		it := it
		c, err := l.Expand(it)
		if err != nil {
			t.Fatal(err)
		}
		if why := swapComparable(c); why != "" {
			skipped++
			skippedWhy[why]++
			continue
		}
		t.Run(it.ID, func(t *testing.T) {
			want := scriptedRun(t, c)
			prov, served, closeFn := httpReplay(t, c)
			defer closeFn()
			got := runCorpusCase(t, c, prov, func() int { return int(served.Load()) }, func() []ProviderRequest { return nil })
			// The feedback segments are only observable on the scripted provider (it records
			// requests); compare the rest.
			w, g := outcomeOf(want), outcomeOf(got)
			if w != g {
				mismatched++
				t.Errorf("the HTTP adapter and the scripted provider disagree:\n scripted %+v\n http     %+v", w, g)
			}
			got.Feedback = want.Feedback
			for _, f := range c.Check(got, wrapper).Failures {
				t.Errorf("[HTTP] %s", f)
			}
			compared++
		})
		// Evaluate figures: one case per gateway (the provider is the case's script).
		// A case carries stateful compile injection: each provider gets its own copy.
		evScripted, evHTTP := evalCaseOf(t, c), evalCaseOf(t, c)
		if evScripted == nil {
			continue
		}
		sp := NewScriptedProvider(scriptStepsOf(c)...)
		scriptedFigures = append(scriptedFigures, evaluateOne(t, NewGateway(sp, Options{}), *evScripted))
		hp, _, closeFn := httpReplay(t, c)
		httpFigures = append(httpFigures, evaluateOne(t, NewGateway(hp, Options{}), *evHTTP))
		closeFn()
	}
	for i := range scriptedFigures {
		if scriptedFigures[i].Usage != httpFigures[i].Usage {
			t.Logf("usage differs for %s: scripted %+v http %+v; attempts %d/%d outcomes %v/%v", scriptedFigures[i].Name, scriptedFigures[i].Usage, httpFigures[i].Usage, scriptedFigures[i].Attempts, httpFigures[i].Attempts, scriptedFigures[i].Outcomes, httpFigures[i].Outcomes)
		}
	}
	a := EvalReport{Version: EvalVersion, Cases: scriptedFigures}
	b := EvalReport{Version: EvalVersion, Cases: httpFigures}
	if a.Signature() != b.Signature() {
		t.Errorf("the evaluation signatures differ: %s vs %s", a.Signature(), b.Signature())
	}
	summarize := func(label string, r []EvalResult) {
		var valid, repaired, attempts int
		var tokens int64
		var latency time.Duration
		for _, x := range r {
			if x.Valid {
				valid++
			}
			if x.Valid && x.Attempts > 1 {
				repaired++
			}
			attempts += x.Attempts
			tokens += x.Usage.InputTokens + x.Usage.OutputTokens
			latency += x.Latency
		}
		t.Logf("%s: %d cases, %d valid (%.1f%%), %d repaired, %d provider calls, %d tokens charged, %v summed latency", label, len(r), valid, 100*float64(valid)/float64(max(len(r), 1)), repaired, attempts, tokens, latency.Round(time.Millisecond))
	}
	summarize("scripted ", scriptedFigures)
	summarize("https-json", httpFigures)
	var why []string
	for k, v := range skippedWhy {
		why = append(why, k+" ("+itoaInt(v)+")")
	}
	sort.Strings(why)
	t.Logf("swap items compared: %d, not representable over the envelope: %d %v, disagreements: %d", compared, skipped, why, mismatched)
	if compared < 100 {
		t.Fatalf("only %d swap items were compared", compared)
	}
}

func itoaInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func evalCaseOf(t *testing.T, c corpus.Case) *EvalCase {
	t.Helper()
	g := NewGateway(NewScriptedProvider(), Options{})
	bundle, err := g.BuildContext(ContextInput{Task: taskOf(c), Request: c.Request, Draft: c.Draft, DraftRevision: 1, DraftHash: c.DraftHash, Tools: toolsOf(c.Tools), RoleNames: append([]string{"owner", "admin", "member", "viewer"}, c.Roles...), Requirements: c.Reqs})
	if err != nil {
		return nil
	}
	want := RunReady
	if len(c.Item.Expect.Status) > 0 {
		want = RunStatus(c.Item.Expect.Status[0])
	}
	return &EvalCase{Name: c.Item.ID, Task: taskOf(c), Context: bundle, Draft: c.Draft, Compile: c.CompileFunc(corpusCompile(t, c.Catalog)), Budget: budgetOf(c), Want: EvalWant{Status: want}}
}

func evaluateOne(t *testing.T, g Gateway, c EvalCase) EvalResult {
	t.Helper()
	rep, err := Evaluate(context.Background(), g, []EvalCase{c})
	if err != nil || len(rep.Cases) != 1 {
		t.Fatalf("Evaluate: %v", err)
	}
	return rep.Cases[0]
}
