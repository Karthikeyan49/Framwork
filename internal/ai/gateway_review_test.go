package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Regression tests of the independent M14 review: the labels of a provider answer (M14R-01), a canceled provider cycle (M14R-02),
// the cap on a reported usage (M14R-06) and a provider that ignores its context (M14R-07).

// stuckProvider ignores its context: it answers only when release is closed, as an adapter without a deadline would.
type stuckProvider struct {
	release chan struct{}
	calls   atomic.Int32
	resp    ProviderResponse
}

func newStuckProvider() *stuckProvider { return &stuckProvider{release: make(chan struct{})} }

func (p *stuckProvider) Complete(context.Context, ProviderRequest) (ProviderResponse, error) {
	p.calls.Add(1)
	<-p.release
	return p.resp, nil
}

// answeringProvider returns one fixed response at once.
type answeringProvider struct{ resp ProviderResponse }

func (p answeringProvider) Complete(context.Context, ProviderRequest) (ProviderResponse, error) {
	return p.resp, nil
}

// withProvider is the rig over another provider, with the same document, compiler and clock.
func (r *rig) withProvider(p Provider) *rig {
	r2 := *r
	r2.g = NewGateway(p, Options{Clock: r.clock.Now})
	return &r2
}

func TestChargeUsageIsCappedNearTheSizeOfTheRequest(t *testing.T) {
	const requestBytes, outTokens = 8000, 4096 // 2000 tokens in, 4096 asked out
	ceiling := 2*(estimateTokens(requestBytes)+outTokens) + 1024
	cases := []struct {
		name         string
		reported     Usage
		payloadBytes int
		want         Usage
	}{
		{"an honest report is charged as reported", Usage{InputTokens: 2100, OutputTokens: 900}, 3000, Usage{InputTokens: 2100, OutputTokens: 900}},
		{"a report below the bytes is raised to the estimate", Usage{InputTokens: 1, OutputTokens: 1}, 4000, Usage{InputTokens: 2000, OutputTokens: 1000}},
		{"a negative report is the estimate", Usage{InputTokens: -5, OutputTokens: -5}, 400, Usage{InputTokens: 2000, OutputTokens: 100}},
		{"a figure above twice the request is capped, not clamped to 100 million", Usage{InputTokens: 1 << 62, OutputTokens: 100_000_000}, 400, Usage{InputTokens: ceiling, OutputTokens: ceiling}},
		{"a report just inside the cap is kept", Usage{InputTokens: ceiling, OutputTokens: ceiling}, 400, Usage{InputTokens: ceiling, OutputTokens: ceiling}},
		{"the bytes of a huge answer still win over the cap", Usage{InputTokens: 10, OutputTokens: 10}, int(ceiling)*4 + 400, Usage{InputTokens: 2000, OutputTokens: estimateTokens(int(ceiling)*4 + 400)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chargeUsage(c.reported, requestBytes, c.payloadBytes, outTokens); got != c.want {
				t.Fatalf("charged %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestProviderLabelsAreReducedBeforeTheyAreStoredOrAudited(t *testing.T) {
	r := newRig(t, Options{})
	rr := r.withProvider(answeringProvider{resp: ProviderResponse{
		Payload:  r.good(),
		Provider: "vendor\x00 <b>name</b>",
		Model:    "model\n\"quoted\" v1.5-π",
		Usage:    Usage{InputTokens: 10, OutputTokens: 10},
	}})
	run := rr.run(okBudget())
	if run.Status != RunReady || len(run.Attempts) != 1 {
		t.Fatalf("status %s attempts %d", run.Status, len(run.Attempts))
	}
	// Only [A-Za-z0-9._:/@+-] survives; the audit entry and the API refuse anything else, which turned a paid answer into a 503.
	if got := run.Attempts[0].Provider; got != "vendorbname/b" {
		t.Fatalf("provider label %q", got)
	}
	if got := run.Attempts[0].Model; got != "modelquotedv1.5-" {
		t.Fatalf("model label %q", got)
	}
	empty := r.withProvider(answeringProvider{resp: ProviderResponse{Payload: r.good(), Provider: "\x00\x01", Model: "", Usage: Usage{InputTokens: 10, OutputTokens: 10}}})
	run = empty.run(okBudget())
	if got := run.Attempts[0].Provider; got != "unknown" {
		t.Fatalf("a label with nothing left is %q, want unknown", got)
	}
}

func TestACanceledCallIsChargedTheReservedOutput(t *testing.T) {
	r := newRig(t, Options{}, ScriptStep{Payload: nil, Delay: 5 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	start := time.Now()
	run, err := r.g.RunPatch(ctx, r.patchInput(okBudget()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want the cancellation", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the cancellation was not honoured")
	}
	// The provider may have worked (and a vendor billed) until the cancellation: the budget sees it as a crash it cannot measure.
	if len(run.Attempts) != 1 || run.Attempts[0].Outcome != OutcomeTimeout {
		t.Fatalf("attempts %+v, want one TIMEOUT", run.Attempts)
	}
	if run.Attempts[0].Usage.InputTokens == 0 || run.Attempts[0].Usage.OutputTokens != 4096 {
		t.Fatalf("attempt usage %+v, want the input estimate and the 4096 reserved output tokens", run.Attempts[0].Usage)
	}
	if run.Usage != run.Attempts[0].Usage {
		t.Fatalf("run usage %+v differs from the attempt's %+v", run.Usage, run.Attempts[0].Usage)
	}
	if r.prov.Calls() != 1 {
		t.Fatalf("%d provider calls", r.prov.Calls())
	}
}

func TestAnAdapterThatIgnoresItsContextCannotHoldTheRun(t *testing.T) {
	r := newRig(t, Options{})
	p := newStuckProvider()
	rr := r.withProvider(p)
	g := rr.g.(*gateway)
	b := okBudget()
	b.WallClock = 80 * time.Millisecond

	start := time.Now()
	run, err := rr.g.RunPatch(context.Background(), rr.patchInput(b))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the run took %v: the wall clock does not bind an adapter that ignores its context", elapsed)
	}
	if run.Status != RunFailed || len(run.Attempts) != 1 || run.Attempts[0].Outcome != OutcomeTimeout || run.Usage.InputTokens == 0 {
		t.Fatalf("status %s attempts %+v usage %+v", run.Status, run.Attempts, run.Usage)
	}
	if n := g.abandoned.Load(); n != 1 {
		t.Fatalf("%d abandoned calls, want 1 while the adapter has not returned", n)
	}
	close(p.release) // the adapter finally returns: the count goes back down, nothing leaks
	deadline := time.Now().Add(2 * time.Second)
	for g.abandoned.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d calls still counted as abandoned after the adapter returned", g.abandoned.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAbandonedProviderCallsAreBounded(t *testing.T) {
	r := newRig(t, Options{})
	p := newStuckProvider()
	t.Cleanup(func() { close(p.release) })
	rr := r.withProvider(p)
	b := okBudget()
	b.WallClock = 20 * time.Millisecond
	for i := 0; i < maxAbandonedCalls; i++ {
		if run, err := rr.g.RunPatch(context.Background(), rr.patchInput(b)); err != nil || run.Status != RunFailed {
			t.Fatalf("run %d: status %s err %v", i, run.Status, err)
		}
	}
	if got := int(p.calls.Load()); got != maxAbandonedCalls {
		t.Fatalf("%d adapter calls for %d runs", got, maxAbandonedCalls)
	}
	// The next run is refused before it reaches the adapter: stuck calls cannot pile up goroutines without bound.
	run, err := rr.g.RunPatch(context.Background(), rr.patchInput(b))
	if err != nil || run.Status != RunFailed || len(run.Attempts) == 0 || run.Attempts[0].Outcome != OutcomeProviderError {
		t.Fatalf("status %s attempts %+v err %v", run.Status, run.Attempts, err)
	}
	if got := int(p.calls.Load()); got != maxAbandonedCalls {
		t.Fatalf("a refused run reached the adapter (%d calls)", got)
	}
}
