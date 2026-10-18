package ai

import (
	"context"
	"time"
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

func (g *gateway) BuildContext(in ContextInput) (Context, error) {
	return Context{}, ErrNotImplemented
}

func (g *gateway) RunPatch(ctx context.Context, in PatchInput) (PatchRun, error) {
	return PatchRun{}, ErrNotImplemented
}

func (g *gateway) RunRequirements(ctx context.Context, in RequirementInput) (RequirementRun, error) {
	return RequirementRun{}, ErrNotImplemented
}
