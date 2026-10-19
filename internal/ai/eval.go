package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	RunError     string // class of a run error (a caller bug or an internal failure), "" when none
}

// EvalReport aggregates a corpus run. Everything except Latency and Usage is a pure
// function of the validation outcomes; Signature covers only that part. It measures a
// pipeline over scripted or test-server bytes: it is no evidence about a real model.
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

// EvalVersion identifies the report format.
const EvalVersion = "ai-eval-v1"

// Signature is a digest of the per-case validation outcomes (name, status, outcome
// sequence, proposal hash, run error class), identical for any provider that yields
// the same bytes. Latency and token figures are not part of it.
func (r EvalReport) Signature() string {
	var b strings.Builder
	b.WriteString(EvalVersion)
	for _, c := range r.Cases {
		fmt.Fprintf(&b, "\n%s|%s|%s|%s|%s", c.Name, c.Status, strings.Join(c.Outcomes, ","), c.ProposalHash, c.RunError)
	}
	return sha256Hex([]byte(b.String()))
}

func runErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrContextTooLarge):
		return "CONTEXT_TOO_LARGE"
	case errors.Is(err, ErrInvalidInput):
		return "INVALID_INPUT"
	case errors.Is(err, ErrNotConfigured):
		return "NOT_CONFIGURED"
	}
	return "INTERNAL"
}

// Evaluate runs a corpus through a gateway and reports validity, repair, latency and
// token figures. It is deterministic given the gateway's provider bytes. It returns an
// error only when the context is canceled; a run that fails is a case outcome.
func Evaluate(ctx context.Context, g Gateway, corpus []EvalCase) (EvalReport, error) {
	report := EvalReport{Version: EvalVersion, Cases: make([]EvalResult, 0, len(corpus))}
	for _, c := range corpus {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		budget := c.Budget
		if budget == (Budget{}) {
			budget = DefaultEvalBudget()
		}
		res := EvalResult{Name: c.Name, Task: c.Task}
		var attempts []Attempt
		var runErr error
		switch c.Task {
		case TaskApplicationPatch:
			var run PatchRun
			run, runErr = g.RunPatch(ctx, PatchInput{Context: c.Context, Draft: c.Draft, Compile: c.Compile, Budget: budget})
			res.Status, res.ProposalHash, res.Usage, attempts = run.Status, run.Hash, run.Usage, run.Attempts
		case TaskRequirementCapture:
			var run RequirementRun
			run, runErr = g.RunRequirements(ctx, RequirementInput{Context: c.Context, Budget: budget})
			res.Status, res.ProposalHash, res.Usage, attempts = run.Status, run.Hash, run.Usage, run.Attempts
		default:
			runErr = fmt.Errorf("%w: unknown task", ErrInvalidInput)
		}
		if errors.Is(runErr, context.Canceled) {
			return report, runErr
		}
		if runErr != nil {
			res.RunError = runErrorClass(runErr)
			if res.Status == "" {
				res.Status = RunFailed
			}
		}
		res.Attempts = len(attempts)
		for _, a := range attempts {
			res.Outcomes = append(res.Outcomes, a.Outcome)
			res.Latency += a.Latency
		}
		res.Valid = res.Status == RunReady
		if res.Valid && res.Attempts > 1 {
			res.Repairs = res.Attempts - 1
			report.Repaired++
		}
		switch {
		case res.Status != c.Want.Status:
			res.Mismatch = fmt.Sprintf("status %s, want %s", res.Status, c.Want.Status)
		case c.Want.ProposalHash != "" && res.ProposalHash != c.Want.ProposalHash:
			res.Mismatch = "proposal hash differs from the golden hash"
		case c.Want.MaxAttempts > 0 && res.Attempts > c.Want.MaxAttempts:
			res.Mismatch = fmt.Sprintf("%d provider calls, at most %d allowed", res.Attempts, c.Want.MaxAttempts)
		}
		res.Accepted = res.Mismatch == ""
		report.Total++
		report.Attempts += res.Attempts
		report.Usage.InputTokens += res.Usage.InputTokens
		report.Usage.OutputTokens += res.Usage.OutputTokens
		report.Latency += res.Latency
		if res.Valid {
			report.Valid++
		}
		if res.Accepted {
			report.Accepted++
		}
		report.Cases = append(report.Cases, res)
	}
	if report.Total > 0 {
		report.ValidityRate = float64(report.Valid) / float64(report.Total)
		report.AcceptanceRate = float64(report.Accepted) / float64(report.Total)
	}
	return report, nil
}
