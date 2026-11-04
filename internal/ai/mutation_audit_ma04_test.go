package ai

import (
	"encoding/json"
	"testing"
)

func TestNoCallWithoutTheOutputReservation(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Payload: r.broken("G0"), Usage: usage(1200, 300)})
	b := okBudget()
	probe := r.patchInput(b)
	req, _ := r.g.(*gateway).request(runSpec{task: TaskApplicationPatch, context: probe.Context, schema: mustSchema(t, r.doc)}, DefaultTemplates(), 1, b.MaxOutputTokens, nil)
	raw, _ := json.Marshal(req)
	b.TokensRemaining = estimateTokens(len(raw)) + int64(b.MaxOutputTokens) - 1 // one token short of what one attempt may cost
	run := r.run(b)
	if run.Status != RunBudgetExceeded || r.prov.Calls() != 0 {
		t.Fatalf("status %s, %d provider calls with a budget one token short of the reservation", run.Status, r.prov.Calls())
	}
}
