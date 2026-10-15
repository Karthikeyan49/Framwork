// Package verification is the deterministic independent-acceptance verifier of
// release candidates (ADR038 section 4).
//
// Verify is a pure function. It reads no clock, randomness, network, database or
// file, never compiles anything, and spawns no goroutine: it runs the stored,
// restored IR through the same workflow and rule engines the runtime uses, against
// requirements and acceptance cases that were authored without the IR. The same
// input always yields byte-identical report JSON, so any party can recompute it.
//
// Honest limit: the report is replayable evidence derived from stored bytes, not
// an attestation by an independent credential. Whoever approves or activates a
// candidate must recompute it (Recompute). A separate verifier principal is a
// later hardening.
package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// VerifierVersion names the verification procedure. A change to the checks needs
// a new version, never an edit.
const VerifierVersion = "verifier-v1"

// Outcomes. Precedence is BLOCKED > FAIL > PASS.
const (
	Pass    = "PASS"
	Fail    = "FAIL"
	Blocked = "BLOCKED"
	Skipped = "SKIPPED"
)

// BLOCKED codes: the candidate cannot be verified.
const (
	CodeLegacyIR              = "LEGACY_IR"
	CodeNoExecutablePlan      = "NO_EXECUTABLE_PLAN"
	CodeUnsupportedByRuntime  = "UNSUPPORTED_BY_RUNTIME"
	CodeUnsupportedByVerifier = "UNSUPPORTED_BY_VERIFIER"
	CodeOmittedRequirement    = "OMITTED_REQUIREMENT"
	CodeOpenMandatory         = "OPEN_MANDATORY"
	CodeFloorViolation        = "FLOOR_VIOLATION"
	CodeIncompleteCaseInput   = "INCOMPLETE_CASE_INPUT"
	CodeBudget                = "BUDGET"
)

// FAIL codes: the implementation differs from the requirement.
const (
	CodeApprovalMismatch  = "APPROVAL_MISMATCH"
	CodeGateBypass        = "GATE_BYPASS"
	CodeGateRolesMismatch = "GATE_ROLES_MISMATCH"
	CodeToolUnresolved    = "TOOL_UNRESOLVED"
	CodeToolEffect        = "TOOL_EFFECT_EXCEEDED"
	CodeToolApproval      = "TOOL_APPROVAL_MISSING"
	CodeToolSandbox       = "TOOL_SANDBOX_EXCEEDED"
	CodeMigrationExceeded = "MIGRATION_EXCEEDED"
)

// Finding is one reason the outcome is not PASS (or an advisory note).
type Finding struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	RequirementID string `json:"requirement_id"`
	CaseID        string `json:"case_id"`
	Detail        string `json:"detail"`
}

// CaseResult is the verdict of one probe, human case or static check.
type CaseResult struct {
	ID            string `json:"id"`
	RequirementID string `json:"requirement_id"`
	Source        string `json:"source"`
	Expected      string `json:"expected"`
	Observed      string `json:"observed"`
	Status        string `json:"status"`
}

// Case sources.
const (
	SourceProbe  = "PROBE"
	SourceCase   = "CASE"
	SourceStatic = "STATIC"
)

// Report is the canonical result. It binds the exact inputs it was computed from.
type Report struct {
	VerifierVersion          string       `json:"verifier_version"`
	VocabularyVersion        string       `json:"vocabulary_version"`
	RequirementPolicyVersion string       `json:"requirement_policy_version"`
	RequirementPolicyHash    string       `json:"requirement_policy_hash"`
	Outcome                  string       `json:"outcome"`
	RequirementsHash         string       `json:"requirements_hash"`
	AcceptanceHash           string       `json:"acceptance_hash"`
	IRHash                   string       `json:"ir_hash"`
	SandboxEvidenceHash      string       `json:"sandbox_evidence_hash"`
	StorageClass             string       `json:"storage_class"`
	Findings                 []Finding    `json:"findings"`
	Results                  []CaseResult `json:"results"`
}

// JSON is the canonical encoding Hash covers.
func (r Report) JSON() []byte { raw, _ := json.Marshal(r); return raw }

// Hash is the hex SHA-256 of JSON.
func (r Report) Hash() string {
	sum := sha256.Sum256(r.JSON())
	return hex.EncodeToString(sum[:])
}

func severityRank(s string) int {
	if s == Blocked {
		return 0
	}
	return 1
}

func (r *Report) settle() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.RequirementID != b.RequirementID {
			return a.RequirementID < b.RequirementID
		}
		if a.CaseID != b.CaseID {
			return a.CaseID < b.CaseID
		}
		return a.Detail < b.Detail
	})
	sort.SliceStable(r.Results, func(i, j int) bool {
		a, b := r.Results[i], r.Results[j]
		if a.RequirementID != b.RequirementID {
			return a.RequirementID < b.RequirementID
		}
		return a.ID < b.ID
	})
	r.Outcome = Pass
	for _, f := range r.Findings {
		switch f.Severity {
		case Blocked:
			r.Outcome = Blocked
			return
		case Fail:
			r.Outcome = Fail
		}
	}
}
