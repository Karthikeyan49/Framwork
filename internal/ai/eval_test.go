package ai

import (
	"context"
	"fmt"
	"testing"
)

func TestEvaluateReportsAndIsDeterministic(t *testing.T) {
	build := func() (EvalReport, error) {
		r := newRig(t, Options{})
		r.prov.Push(
			ScriptStep{Payload: r.good(), Usage: usage(100, 50)},                                                            // case 1: first try
			ScriptStep{Payload: r.broken("G"), Usage: usage(100, 50)}, ScriptStep{Payload: r.good(), Usage: usage(100, 50)}, // case 2: repaired
			ScriptStep{Payload: []byte("nope"), Usage: usage(100, 50)}, ScriptStep{Payload: []byte("nope"), Usage: usage(100, 50)}, ScriptStep{Payload: []byte("nope"), Usage: usage(100, 50)}, // case 3
		)
		in := r.patchInput(okBudget())
		_, hash, _ := CanonicalPatch(parseOK(t, r.good()))
		corpus := []EvalCase{
			{Name: "first-try", Task: TaskApplicationPatch, Context: in.Context, Draft: r.doc, Compile: r.comp.Compile, Want: EvalWant{Status: RunReady, ProposalHash: hash, MaxAttempts: 1}},
			{Name: "repaired", Task: TaskApplicationPatch, Context: in.Context, Draft: r.doc, Compile: r.comp.Compile, Want: EvalWant{Status: RunReady, MaxAttempts: 2}},
			{Name: "garbage", Task: TaskApplicationPatch, Context: in.Context, Draft: r.doc, Compile: r.comp.Compile, Want: EvalWant{Status: RunReady}},
		}
		return Evaluate(context.Background(), r.g, corpus)
	}
	a, err := build()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := build()
	if a.Signature() != b.Signature() || len(a.Signature()) != 64 {
		t.Fatal("the signature is not a pure function of the validation outcomes")
	}
	if a.Total != 3 || a.Valid != 2 || a.Accepted != 2 || a.Repaired != 1 || a.Attempts != 6 || fmt.Sprintf("%.3f", a.ValidityRate) != "0.667" {
		t.Fatalf("report %+v", a)
	}
	if got := a.Cases[2]; got.Accepted || got.Mismatch == "" || got.Status != RunFailed {
		t.Fatalf("a failed case must say why: %+v", got)
	}
	if got := a.Cases[1]; got.Repairs != 1 || fmt.Sprint(got.Outcomes) != "[COMPILE_ERROR OK]" {
		t.Fatalf("repair case %+v", got)
	}
	if a.Usage.InputTokens < 600 || a.Usage.OutputTokens < 300 {
		t.Fatalf("usage %+v", a.Usage)
	}
}

func TestEvaluateWantChecks(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Payload: r.good(), Usage: usage(1, 1)}, ScriptStep{Payload: r.broken("G"), Usage: usage(1, 1)}, ScriptStep{Payload: r.good(), Usage: usage(1, 1)})
	in := r.patchInput(okBudget())
	c := func(name string, want EvalWant) EvalCase {
		return EvalCase{Name: name, Task: TaskApplicationPatch, Context: in.Context, Draft: r.doc, Compile: r.comp.Compile, Want: want}
	}
	rep, err := Evaluate(context.Background(), r.g, []EvalCase{
		c("wrong hash", EvalWant{Status: RunReady, ProposalHash: "00"}),
		c("too many calls", EvalWant{Status: RunReady, MaxAttempts: 1}),
	})
	if err != nil || rep.Accepted != 0 || rep.Cases[0].Mismatch == "" || rep.Cases[1].Mismatch == "" {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestEvaluateRecordsRunErrorsAsCases(t *testing.T) {
	r := newRig(t, Options{})
	rep, err := Evaluate(context.Background(), r.g, []EvalCase{
		{Name: "no compile", Task: TaskApplicationPatch, Context: r.patchInput(okBudget()).Context, Draft: r.doc, Want: EvalWant{Status: RunFailed}},
		{Name: "unknown task", Task: "chat", Want: EvalWant{Status: RunFailed}},
		{Name: "forged context", Task: TaskRequirementCapture, Context: Context{Version: "x"}, Want: EvalWant{Status: RunFailed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Cases {
		if c.RunError != "INVALID_INPUT" || c.Status != RunFailed || !c.Accepted {
			t.Errorf("%+v", c)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Evaluate(ctx, r.g, []EvalCase{{Name: "x", Task: TaskApplicationPatch}}); err == nil {
		t.Fatal("a canceled context stops the evaluation")
	}
}

func TestEvaluateRequirementCases(t *testing.T) {
	prov := NewScriptedProvider(ScriptStep{Payload: reqPayload(t, nil), Usage: usage(10, 10)})
	g := NewGateway(prov, Options{})
	c, err := g.BuildContext(ContextInput{Task: TaskRequirementCapture, Request: "Orders above 500 need approval.", Draft: fixtureDoc(t, exWorkflow), DraftHash: testDraftHash})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Evaluate(context.Background(), g, []EvalCase{{Name: "req", Task: TaskRequirementCapture, Context: c, Want: EvalWant{Status: RunReady}}})
	if err != nil || rep.Accepted != 1 || rep.Cases[0].ProposalHash == "" {
		t.Fatalf("%+v %v", rep, err)
	}
}
