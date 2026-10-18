package ai

import (
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

var (
	_ ApplyPatch      = ApplyProposal
	_ DeriveSensitive = DeriveSensitiveAspects
)

// ApplyProposal applies a proposal to the CURRENT draft with per-target expected-hash
// CAS. It never mutates its input.
func ApplyProposal(current metadata.Document, p PatchProposal) (metadata.Document, []Conflict, error) {
	return metadata.Document{}, nil, ErrNotImplemented
}

// DeriveSensitiveAspects derives the sensitive "<target>:<aspect>" list from the
// compiler diff of the patched program against the program it was applied to.
func DeriveSensitiveAspects(current, patched compiler.Program) (Derived, error) {
	return Derived{}, ErrNotImplemented
}
