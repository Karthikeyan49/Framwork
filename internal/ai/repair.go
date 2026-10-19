package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
)

// The bounded loop shared by RunPatch and RunRequirements (ADR104 section 7 and
// amendment B). It owns the budgets, the provider calls, the usage accounting and
// the trace; what a payload means is decided by the evaluate function of the caller.
// Nothing here opens a transaction, touches a database or mutates a draft.

// maxChargeTokens bounds any single token figure taken from a provider report, so a
// hostile or buggy adapter can neither overflow the totals nor refill a budget with a
// negative number. It equals the database ceiling of the usage columns.
const maxChargeTokens = 100_000_000

// attemptEval is the verdict of one payload.
type attemptEval struct {
	outcome  string                // an Outcome* constant
	diags    []compiler.Diagnostic // machine-readable problems fed to the next attempt
	prevHash string                // hash of the proposal (or of the raw output) the problems refer to
	accepted bool                  // the payload is a valid, compiling proposal: stop with READY
	terminal bool                  // a further model call cannot help: stop
	err      error                 // an internal failure of the pipeline, not of the model
}

type runSpec struct {
	task     Task
	context  Context
	schema   json.RawMessage
	budget   Budget
	now      func() time.Time
	evaluate func(payload []byte) attemptEval
}

type loopResult struct {
	attempts []Attempt
	usage    Usage
	status   RunStatus // READY, FAILED or BUDGET_EXCEEDED; the caller refines FAILED
	last     attemptEval
	diags    []compiler.Diagnostic // the last problems found, kept when a later provider failure ends the run
	err      error
}

func estimateTokens(bytes int) int64 { return int64(bytes+3) / 4 }

func clampTokens(v int64) int64 {
	switch {
	case v < 0:
		return 0
	case v > maxChargeTokens:
		return maxChargeTokens
	}
	return v
}

// chargeUsage is what a call costs the budget: the provider's report, clamped to
// [0, maxChargeTokens], but never less than the byte estimate of what actually
// crossed the boundary (about four bytes per token). A provider that reports nothing,
// or less than the bytes it was sent and returned, is charged the estimate.
func chargeUsage(reported Usage, requestBytes, payloadBytes int) Usage {
	return Usage{
		InputTokens:  max(clampTokens(reported.InputTokens), estimateTokens(requestBytes)),
		OutputTokens: max(clampTokens(reported.OutputTokens), estimateTokens(payloadBytes)),
	}
}

// feedback is what the next attempt is told about the previous one.
type feedback struct {
	prevHash string
	diags    []compiler.Diagnostic
}

// request builds the provider request of one attempt: the bundle, then (after a
// rejected answer) the repair lead-in and the problems as UNTRUSTED data.
func (g *gateway) request(spec runSpec, tpl TemplateSet, attempt, outTokens int, fb *feedback) (ProviderRequest, error) {
	segments := append([]Segment(nil), spec.context.Segments...)
	if fb != nil {
		text, err := wrapUntrusted(struct {
			PreviousProposalHash string                `json:"previous_proposal_hash"`
			Problems             []compiler.Diagnostic `json:"problems"`
		}{fb.prevHash, capDiagnostics(fb.diags)})
		if err != nil {
			return ProviderRequest{}, err
		}
		segments = append(segments,
			Segment{Name: SegmentRepair, Trust: TrustTrusted, Text: tpl.Repair},
			Segment{Name: SegmentDiagnostics, Trust: TrustUntrusted, Text: text})
	}
	return ProviderRequest{Task: spec.task, TemplateVersion: tpl.Version, Segments: segments, OutputSchema: spec.schema, MaxOutputTokens: outTokens, Attempt: attempt}, nil
}

// call runs one provider call. A panic inside an adapter is operator code failing;
// it becomes a refusal, which the loop never retries.
func (g *gateway) call(ctx context.Context, req ProviderRequest) (resp ProviderResponse, err error) {
	defer func() {
		if recover() != nil {
			resp, err = ProviderResponse{}, fmt.Errorf("%w: the adapter panicked", ErrProviderRefused)
		}
	}()
	return g.provider.Complete(ctx, req)
}

func (g *gateway) loop(ctx context.Context, spec runSpec) loopResult {
	var res loopResult
	tpl := g.templates()
	maxAttempts := MaxAttempts
	for _, limit := range []int{g.opts.MaxAttempts, spec.budget.MaxAttempts} {
		if limit > 0 && limit < maxAttempts {
			maxAttempts = limit
		}
	}
	outTokens := spec.budget.MaxOutputTokens
	switch {
	case outTokens <= 0:
		outTokens = DefaultMaxOutputTokens
	case outTokens > MaxOutputTokensCeiling:
		outTokens = MaxOutputTokensCeiling
	}
	wall := spec.budget.WallClock
	if wall <= 0 || wall > DefaultWallClock {
		wall = DefaultWallClock
	}
	runCtx, cancel := context.WithTimeout(ctx, wall)
	defer cancel()

	remaining := spec.budget.TokensRemaining
	retried := false
	var fb *feedback
	for n := 1; n <= maxAttempts; n++ {
		if errors.Is(ctx.Err(), context.Canceled) {
			res.status, res.err = RunFailed, ctx.Err()
			return res
		}
		if runCtx.Err() != nil || remaining <= 0 {
			res.status = RunBudgetExceeded
			return res
		}
		req, err := g.request(spec, tpl, n, outTokens, fb)
		if err != nil {
			res.status, res.err = RunFailed, err
			return res
		}
		reqJSON, err := json.Marshal(req)
		if err != nil {
			res.status, res.err = RunFailed, err
			return res
		}
		if remaining < estimateTokens(len(reqJSON))+int64(outTokens) {
			res.status = RunBudgetExceeded
			return res
		}
		at := Attempt{N: n, TemplateVersion: tpl.Version, ContextHash: spec.context.Hash, RequestHash: sha256Hex(reqJSON)}
		started := spec.now()
		resp, perr := g.call(runCtx, req)
		at.Latency = spec.now().Sub(started)
		if resp.Latency > 0 {
			at.Latency = resp.Latency
		}
		if at.Latency < 0 {
			at.Latency = 0
		}

		if perr != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				res.status, res.err = RunFailed, ctx.Err()
				return res
			}
			at.Outcome = OutcomeProviderError
			if errors.Is(perr, ErrProviderTimeout) || errors.Is(perr, context.DeadlineExceeded) {
				// The provider may have processed the request before it ran out of time.
				at.Outcome = OutcomeTimeout
				at.Usage = Usage{InputTokens: estimateTokens(len(reqJSON))}
			}
			remaining -= at.Usage.InputTokens
			res.usage.InputTokens += at.Usage.InputTokens
			res.attempts = append(res.attempts, at)
			res.last = attemptEval{outcome: at.Outcome}
			if at.Outcome == OutcomeProviderError && errors.Is(perr, ErrProviderUnavailable) && !retried {
				retried = true // one retry of a transient failure, inside the attempt budget
				continue
			}
			res.status = RunFailed
			return res
		}

		at.Provider, at.Model = clip(resp.Provider, 64), clip(resp.Model, MaxModelLabelRunes)
		at.Usage = chargeUsage(resp.Usage, len(reqJSON), len(resp.Payload))
		remaining -= at.Usage.InputTokens + at.Usage.OutputTokens
		res.usage.InputTokens += at.Usage.InputTokens
		res.usage.OutputTokens += at.Usage.OutputTokens
		if len(resp.Payload) > 0 {
			at.OutputHash = sha256Hex(resp.Payload)
		}
		var ev attemptEval
		if len(resp.Payload) > MaxOutputBytes {
			ev = attemptEval{outcome: OutcomeParseError, prevHash: at.OutputHash,
				diags: []compiler.Diagnostic{{Code: "too_large", Message: "the answer is larger than the output limit"}}}
		} else {
			ev = spec.evaluate(resp.Payload)
		}
		ev.diags = capDiagnostics(ev.diags)
		at.Outcome = ev.outcome
		if len(ev.diags) > 0 {
			raw, _ := json.Marshal(ev.diags)
			at.DiagnosticsHash = sha256Hex(raw)
		}
		res.attempts = append(res.attempts, at)
		res.last = ev
		if len(ev.diags) > 0 {
			res.diags = ev.diags
		}
		switch {
		case ev.err != nil:
			res.status, res.err = RunFailed, ev.err
			return res
		case ev.accepted:
			res.status = RunReady
			return res
		case ev.terminal:
			res.status = RunFailed
			return res
		}
		fb = &feedback{prevHash: ev.prevHash, diags: ev.diags}
	}
	res.status = RunFailed
	return res
}

// capDiagnostics bounds, sanitizes and redacts compiler or decoder diagnostics
// before they are fed back, stored or returned: at most MaxDiagnostics of them, each
// field clipped, no control or invisible character. It is idempotent.
func capDiagnostics(in []compiler.Diagnostic) []compiler.Diagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]compiler.Diagnostic, 0, min(len(in), MaxDiagnostics))
	for _, d := range in {
		if len(out) >= MaxDiagnostics {
			break
		}
		out = append(out, compiler.Diagnostic{Path: redactString(clip(d.Path, 200)), Code: redactString(clip(d.Code, 64)), Message: redactString(clip(d.Message, 300))})
	}
	return out
}

// parseFailure turns a strict-decoder error into an attempt verdict. The detail text
// of a ProposalError is fixed vocabulary (it never echoes model bytes), so it is
// safe to feed back; an error that is not a ProposalError is an internal failure.
func parseFailure(err error, payload []byte) attemptEval {
	var pe *ProposalError
	if !errors.As(err, &pe) {
		return attemptEval{outcome: OutcomeSchemaError, err: err}
	}
	outcome, code := OutcomeSchemaError, "schema_violation"
	switch {
	case errors.Is(err, ErrDuplicateKey):
		outcome, code = OutcomeParseError, "duplicate_key"
	case errors.Is(err, ErrTooLarge):
		outcome, code = OutcomeParseError, "too_large"
	case errors.Is(err, ErrMalformed):
		outcome, code = OutcomeParseError, "malformed_json"
	case errors.Is(err, ErrForbiddenKind):
		outcome, code = OutcomeForbidden, "forbidden_kind"
	case errors.Is(err, ErrServerField):
		code = "server_field"
	case errors.Is(err, ErrUnknownField):
		code = "unknown_field"
	case errors.Is(err, ErrSemantics):
		code = "invalid_semantics"
	}
	return attemptEval{outcome: outcome, prevHash: sha256Hex(payload), diags: []compiler.Diagnostic{{Path: pe.Path, Code: code, Message: pe.Detail}}}
}

// conflictDiagnostics presents typed apply conflicts in the shape of the compiler's
// own diagnostics.
func conflictDiagnostics(conflicts []Conflict) []compiler.Diagnostic {
	out := make([]compiler.Diagnostic, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, compiler.Diagnostic{Path: "/" + c.Target, Code: strings.ToLower(c.Category), Message: c.Reason})
	}
	return out
}
