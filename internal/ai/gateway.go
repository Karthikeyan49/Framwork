package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
)

// Options configures a Gateway. A zero Clock is time.Now, a zero MaxAttempts the
// MaxAttempts ceiling (a larger value is clamped to it) and a zero Templates the
// DefaultTemplates set.
type Options struct {
	Clock       func() time.Time
	MaxAttempts int
	Templates   TemplateSet
}

type gateway struct {
	provider Provider
	opts     Options
}

// NewGateway returns the pipeline over an injected provider. It is safe for
// concurrent use and never holds a database connection or transaction.
func NewGateway(p Provider, o Options) Gateway { return &gateway{provider: p, opts: o} }

func (g *gateway) clock(override func() time.Time) func() time.Time {
	switch {
	case override != nil:
		return override
	case g.opts.Clock != nil:
		return g.opts.Clock
	}
	return time.Now
}

// RunPatch is the repair loop of ADR040 section 7 for TaskApplicationPatch: provider
// call, strict parse, allowlist and expected-hash CAS on an in-memory copy of the
// draft, real compile, diagnostics fed back, at most Budget.MaxAttempts calls. No
// outcome mutates the draft. It returns an error only for a caller bug (no compile
// function, a context the gateway did not build), a canceled parent context or an
// internal failure; every provider, parse, policy and budget outcome is a status.
func (g *gateway) RunPatch(ctx context.Context, in PatchInput) (PatchRun, error) {
	if g.provider == nil {
		return PatchRun{}, errNoProvider
	}
	if in.Compile == nil {
		return PatchRun{}, fmt.Errorf("%w: no compile function", ErrInvalidInput)
	}
	if err := g.checkContext(in.Context, TaskApplicationPatch); err != nil {
		return PatchRun{}, err
	}
	schema, err := PatchOutputSchema(in.Draft.SchemaVersion)
	if err != nil {
		return PatchRun{}, err
	}
	if err = ctx.Err(); err != nil {
		return PatchRun{}, err
	}

	var (
		parsed      *PatchProposal
		parsedCanon []byte
		parsedHash  string
		accepted    *PatchRun
		infra       bool
	)
	evaluate := func(payload []byte) attemptEval {
		p, perr := ParsePatchProposal(payload)
		if perr == nil {
			var canon []byte
			var hash string
			if canon, hash, perr = CanonicalPatch(p); perr == nil {
				cp, _ := cloneJSON(p)
				parsed, parsedCanon, parsedHash = &cp, canon, hash
			}
		}
		if perr != nil {
			return parseFailure(perr, payload)
		}
		ap, aerr := applyProposal(in.Draft, p)
		if aerr != nil {
			var pe *ProposalError
			if errors.As(aerr, &pe) {
				return parseFailure(aerr, payload)
			}
			return attemptEval{outcome: OutcomeCompileError, prevHash: parsedHash, err: aerr}
		}
		if len(ap.conflicts) > 0 {
			outcome := OutcomeConflict
			for _, c := range ap.conflicts {
				if c.Category == ConflictForbiddenKind {
					outcome = OutcomeForbidden
				}
			}
			return attemptEval{outcome: outcome, diags: conflictDiagnostics(ap.conflicts), prevHash: parsedHash}
		}
		program, diags, cerr := in.Compile(ap.doc)
		if cerr != nil {
			infra = true
			return attemptEval{outcome: OutcomeCompileError, prevHash: parsedHash, terminal: true,
				diags: []compiler.Diagnostic{{Code: "compiler_unavailable", Message: "the compiler could not run"}}}
		}
		if len(diags) > 0 || !program.Valid() {
			if len(diags) == 0 {
				diags = []compiler.Diagnostic{{Code: "invalid_program", Message: "the compiler produced no program"}}
			}
			return attemptEval{outcome: OutcomeCompileError, diags: diags, prevHash: parsedHash}
		}
		// The base of the sensitive-aspect derivation is the program of the current
		// draft; a draft that does not compile counts everything as new.
		var base compiler.Program
		if cur, curDiags, cerr := in.Compile(in.Draft); cerr == nil && len(curDiags) == 0 && cur.Valid() {
			base = cur
		}
		derived, derr := DeriveSensitiveAspects(base, program)
		if derr != nil {
			infra = true
			return attemptEval{outcome: OutcomeCompileError, prevHash: parsedHash, terminal: true,
				diags: []compiler.Diagnostic{{Code: "diff_unsupported", Message: "the change against the current draft cannot be classified"}}}
		}
		final, _ := cloneJSON(ap.doc)
		pp, _ := cloneJSON(p)
		accepted = &PatchRun{Proposal: &pp, Canonical: parsedCanon, Hash: parsedHash, Derived: derived, Final: &final}
		return attemptEval{outcome: OutcomeOK, accepted: true, prevHash: parsedHash}
	}

	res := g.loop(ctx, runSpec{task: TaskApplicationPatch, context: in.Context, schema: schema, budget: in.Budget, now: g.clock(in.Now), evaluate: evaluate})
	run := PatchRun{Status: res.status, Attempts: res.attempts, Usage: res.usage, Diagnostics: capDiagnostics(res.last.diags)}
	if res.err != nil {
		return run, res.err
	}
	switch {
	case res.status == RunReady && accepted != nil:
		run.Proposal, run.Canonical, run.Hash, run.Derived, run.Final = accepted.Proposal, accepted.Canonical, accepted.Hash, accepted.Derived, accepted.Final
		run.Diagnostics = nil
	case res.status == RunFailed && !infra && parsed != nil && (res.last.outcome == OutcomeConflict || res.last.outcome == OutcomeCompileError):
		run.Status = RunNeedsHuman
		run.Proposal, run.Canonical, run.Hash = parsed, parsedCanon, parsedHash
		run.Derived = Derived{Targets: targetsOf(*parsed), Sensitive: []string{}, Aspects: []string{}}
	}
	return run, nil
}

func targetsOf(p PatchProposal) []DerivedTarget {
	out := make([]DerivedTarget, 0, len(p.Operations))
	for _, op := range p.Operations {
		out = append(out, DerivedTarget{ID: op.Target, Kind: targetKind(op.Target), Op: op.Op})
	}
	return out
}

// RunRequirements is the loop for TaskRequirementCapture. There is no compile step:
// the semantics of every draft are validated by the typed decoders of the intent
// package, and a category the verifier does not understand is refused with a
// diagnostic that sends it to the open questions.
func (g *gateway) RunRequirements(ctx context.Context, in RequirementInput) (RequirementRun, error) {
	if g.provider == nil {
		return RequirementRun{}, errNoProvider
	}
	if err := g.checkContext(in.Context, TaskRequirementCapture); err != nil {
		return RequirementRun{}, err
	}
	if err := ctx.Err(); err != nil {
		return RequirementRun{}, err
	}
	var (
		accepted *RequirementProposal
		canon    []byte
		hash     string
		semantic bool
	)
	evaluate := func(payload []byte) attemptEval {
		p, perr := ParseRequirementProposal(payload)
		if perr == nil {
			var c []byte
			var h string
			if c, h, perr = CanonicalRequirementProposal(p); perr == nil {
				pp, _ := cloneJSON(p)
				accepted, canon, hash = &pp, c, h
				return attemptEval{outcome: OutcomeOK, accepted: true, prevHash: h}
			}
		}
		semantic = errors.Is(perr, ErrSemantics)
		return parseFailure(perr, payload)
	}
	res := g.loop(ctx, runSpec{task: TaskRequirementCapture, context: in.Context, schema: RequirementOutputSchema(), budget: in.Budget, now: g.clock(in.Now), evaluate: evaluate})
	run := RequirementRun{Status: res.status, Attempts: res.attempts, Usage: res.usage, Diagnostics: capDiagnostics(res.last.diags)}
	if res.err != nil {
		return run, res.err
	}
	switch {
	case res.status == RunReady && accepted != nil:
		run.Proposal, run.Canonical, run.Hash, run.Diagnostics = accepted, canon, hash, nil
	case res.status == RunFailed && semantic && res.last.outcome == OutcomeSchemaError:
		run.Status = RunNeedsHuman
	}
	return run, nil
}
