package ai

import (
	"errors"
	"fmt"
)

// ErrNotImplemented marks a published function whose body is not written yet.
var ErrNotImplemented = errors.New("ai: not implemented")

// Proposal error classes. Every strict decoder failure is a *ProposalError whose
// class is one of the sentinels below, so a caller classifies with errors.Is and
// never matches text. Messages and paths are fixed vocabulary: they never echo
// model-supplied bytes.
var (
	ErrProposal      = errors.New("ai: invalid proposal")
	ErrMalformed     = fmt.Errorf("%w: not well-formed JSON", ErrProposal)
	ErrTooLarge      = fmt.Errorf("%w: exceeds a size, depth or count bound", ErrProposal)
	ErrDuplicateKey  = fmt.Errorf("%w: duplicate or case-folded duplicate key", ErrProposal)
	ErrSchema        = fmt.Errorf("%w: violates the closed schema", ErrProposal)
	ErrUnknownField  = fmt.Errorf("%w: unknown field", ErrSchema)
	ErrServerField   = fmt.Errorf("%w: names a server-derived field", ErrSchema)
	ErrForbiddenKind = fmt.Errorf("%w: target kind is not allowed", ErrProposal)
	ErrSemantics     = fmt.Errorf("%w: requirement semantics do not match their category", ErrProposal)
)

// ProposalError is the typed failure of ParsePatchProposal, ParseRequirementProposal
// and the canonical encoders.
type ProposalError struct {
	Class  error  // one of the Err* classes above
	Path   string // JSON-pointer-like location inside the payload; "" for the whole payload
	Detail string // fixed text, never model-supplied
}

func (e *ProposalError) Error() string {
	if e.Path == "" {
		return e.Class.Error() + ": " + e.Detail
	}
	return e.Class.Error() + " at " + e.Path + ": " + e.Detail
}

func (e *ProposalError) Unwrap() error { return e.Class }

func proposalErr(class error, path, detail string) *ProposalError {
	return &ProposalError{Class: class, Path: path, Detail: detail}
}
