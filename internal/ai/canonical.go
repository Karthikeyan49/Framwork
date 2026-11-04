package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Karthikeyan49/Framwork/internal/intent"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalPatch returns the canonical JSON of a proposal and its SHA-256 (hex). The
// canonical form is by definition what the strict parser returns for the proposal's
// own encoding, so only a proposal that passes ParsePatchProposal has one, and
// parsing the canonical bytes reproduces them exactly. Top-level lists may be nil;
// slices inside a definition must already be the empty slice the schema asks for.
func CanonicalPatch(p PatchProposal) (canonical []byte, hash string, err error) {
	if p.Operations == nil {
		p.Operations = []PatchOperation{}
	}
	if p.Assumptions == nil {
		p.Assumptions = []string{}
	}
	if p.UnresolvedRequirements == nil {
		p.UnresolvedRequirements = []UnresolvedRequirement{}
	}
	if p.ProposedTests == nil {
		p.ProposedTests = []intent.AcceptanceCase{}
	}
	if p.RiskFlags == nil {
		p.RiskFlags = []RiskFlag{}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, "", proposalErr(ErrMalformed, "", "proposal cannot be encoded")
	}
	parsed, err := ParsePatchProposal(raw)
	if err != nil {
		return nil, "", err
	}
	if canonical, err = json.Marshal(parsed); err != nil {
		return nil, "", proposalErr(ErrMalformed, "", "proposal cannot be encoded")
	}
	return canonical, sha256Hex(canonical), nil
}

// CanonicalRequirementProposal is CanonicalPatch for requirement proposals.
func CanonicalRequirementProposal(p RequirementProposal) (canonical []byte, hash string, err error) {
	if p.Requirements == nil {
		p.Requirements = []RequirementDraft{}
	}
	if p.OpenQuestions == nil {
		p.OpenQuestions = []UnresolvedRequirement{}
	}
	if p.ProposedTests == nil {
		p.ProposedTests = []intent.AcceptanceCase{}
	}
	if p.Assumptions == nil {
		p.Assumptions = []string{}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, "", proposalErr(ErrMalformed, "", "proposal cannot be encoded")
	}
	parsed, err := ParseRequirementProposal(raw)
	if err != nil {
		return nil, "", err
	}
	if canonical, err = json.Marshal(parsed); err != nil {
		return nil, "", proposalErr(ErrMalformed, "", "proposal cannot be encoded")
	}
	return canonical, sha256Hex(canonical), nil
}
