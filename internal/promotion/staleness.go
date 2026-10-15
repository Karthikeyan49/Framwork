package promotion

import (
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/risk"
)

// StaleReason names why a candidate no longer matches the world it was built in.
type StaleReason string

const (
	StaleBaseChanged        StaleReason = "BASE_CHANGED"
	StaleBaseGeneration     StaleReason = "BASE_GENERATION_ADVANCED"
	StaleSchemaChanged      StaleReason = "SCHEMA_CHANGED"
	StalePolicyChanged      StaleReason = "POLICY_CHANGED"
	StaleToolChanged        StaleReason = "TOOL_CHANGED"
	StaleToolRemoved        StaleReason = "TOOL_REMOVED"
	StaleToolTenantDenied   StaleReason = "TOOL_TENANT_DENIED"
	StaleRiskEscalated      StaleReason = "RISK_ESCALATED"
	StaleEvidenceInvalid    StaleReason = "EVIDENCE_INVALID"
	StaleUnsupportedRuntime StaleReason = "UNSUPPORTED_RUNTIME"
)

// ToolPinStatus is the live state of one pin in the operator catalog.
type ToolPinStatus string

const (
	ToolOK           ToolPinStatus = "OK"
	ToolChanged      ToolPinStatus = "CHANGED"
	ToolRemoved      ToolPinStatus = "REMOVED"
	ToolTenantDenied ToolPinStatus = "TENANT_DENIED"
)

// BoundState is what a candidate was built against, read from its stored body.
type BoundState struct {
	BaseRelease       string
	BaseGeneration    int64
	BaseSchemaHash    string
	PolicyBundleHash  string
	ToolPins          []ToolPin
	IRVersion         string
	RiskPolicyVersion string
	RiskLevel         risk.Level
	RiskFacts         risk.Facts
	EvidenceHash      string
}

// EvidenceState is the live state of the candidate's sandbox evidence.
type EvidenceState struct {
	Exists, Frozen bool
	Hash           string
}

// CurrentState is read by a trusted caller from the database and catalogs.
// Source revisions moving is not staleness: a newer draft does not invalidate a
// candidate built from an older, exact revision.
type CurrentState struct {
	ActiveRelease    string
	ActiveGeneration int64
	BaseSchemaHash   string
	PolicyBundleHash string
	ToolStatus       func(ToolPin) ToolPinStatus
	SupportedIR      []string
	Volume           string
	Evidence         EvidenceState
}

// Staleness derives, never stores, why a candidate is stale. An empty (non-nil)
// result means FRESH. Callers must re-evaluate it inside the approval and
// activation transactions; a stale candidate is rebuilt to a new hash and never
// revived.
func Staleness(bound BoundState, current CurrentState) []StaleReason {
	set := map[StaleReason]bool{}
	if bound.BaseRelease != current.ActiveRelease {
		set[StaleBaseChanged] = true
	} else if bound.BaseGeneration != current.ActiveGeneration {
		set[StaleBaseGeneration] = true
	}
	if bound.BaseSchemaHash != current.BaseSchemaHash {
		set[StaleSchemaChanged] = true
	}
	if bound.PolicyBundleHash != current.PolicyBundleHash {
		set[StalePolicyChanged] = true
	}
	for _, pin := range bound.ToolPins {
		status := ToolRemoved
		if current.ToolStatus != nil {
			status = current.ToolStatus(pin)
		}
		switch status {
		case ToolOK:
		case ToolChanged:
			set[StaleToolChanged] = true
		case ToolTenantDenied:
			set[StaleToolTenantDenied] = true
		default:
			set[StaleToolRemoved] = true
		}
	}
	facts := bound.RiskFacts
	facts.Volume = current.Volume
	if assessment, err := risk.Latest().Evaluate(facts); err != nil || assessment.Blocked || assessment.Level > bound.RiskLevel {
		set[StaleRiskEscalated] = true
	}
	if !current.Evidence.Exists || !current.Evidence.Frozen || current.Evidence.Hash != bound.EvidenceHash {
		set[StaleEvidenceInvalid] = true
	}
	supported := false
	for _, v := range current.SupportedIR {
		if v == bound.IRVersion {
			supported = true
		}
	}
	if !supported {
		set[StaleUnsupportedRuntime] = true
	}
	out := make([]StaleReason, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
