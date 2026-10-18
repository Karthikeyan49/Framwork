package ai

import (
	"context"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// EvalWant is the golden expectation of one case.
type EvalWant struct {
	Status       RunStatus // required
	ProposalHash string    // optional: exact canonical proposal hash
	MaxAttempts  int       // optional: provider calls allowed; 0 = any
}

// EvalCase is one evaluation case. Draft and Compile are used by
// TaskApplicationPatch only. A zero Budget selects DefaultEvalBudget.
type EvalCase struct {
	Name    string
	Task    Task
	Context Context
	Draft   metadata.Document
	Compile CompileFunc
	Budget  Budget
	Want    EvalWant
}

// EvalResult is the outcome of one case.
type EvalResult struct {
	Name         string
	Task         Task
	Status       RunStatus
	Valid        bool // the run produced a validated proposal (READY)
	Accepted     bool // the run matches Want
	Attempts     int
	Repairs      int
	Outcomes     []string // per-attempt outcomes, in order
	ProposalHash string
	Usage        Usage
	Latency      time.Duration
	Mismatch     string // why Accepted is false
}

// EvalReport aggregates a corpus run. Everything except Latency and Usage is a pure
// function of the validation outcomes; Signature covers only that part.
type EvalReport struct {
	Version        string
	Cases          []EvalResult
	Total, Valid   int
	Accepted       int
	Repaired       int // cases that needed more than one provider call and were READY
	ValidityRate   float64
	AcceptanceRate float64
	Attempts       int
	Usage          Usage
	Latency        time.Duration
}

// Signature is a digest of the per-case validation outcomes (name, status, outcome
// sequence, proposal hash), identical for any provider that yields the same bytes.
func (r EvalReport) Signature() string { return "" }

// Evaluate runs a corpus through a gateway and reports validity, repair, latency and
// token figures. It is deterministic given the gateway's provider bytes.
func Evaluate(ctx context.Context, g Gateway, corpus []EvalCase) (EvalReport, error) {
	return EvalReport{}, ErrNotImplemented
}
