package ai

// CanonicalPatch returns the canonical JSON of a proposal and its SHA-256 (hex).
func CanonicalPatch(p PatchProposal) (canonical []byte, hash string, err error) {
	return nil, "", ErrNotImplemented
}

// CanonicalRequirementProposal returns the canonical JSON of a proposal and its SHA-256 (hex).
func CanonicalRequirementProposal(p RequirementProposal) (canonical []byte, hash string, err error) {
	return nil, "", ErrNotImplemented
}
