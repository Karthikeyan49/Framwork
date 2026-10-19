package promotion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

// Derived dependencies of a candidate (ADR102 section 2). None is declared by a
// caller: each is computed from trusted artifacts so approval can recompute it.

// ToolPin is one governed tool activity pin found in the compiled workflows.
type ToolPin struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// ToolPins returns every distinct tool pin of the IR's workflow plans, sorted by
// (ref, version, hash). A release without tools has an empty, non-nil list.
func ToolPins(ir compiler.IR) []ToolPin {
	seen := map[ToolPin]bool{}
	for _, plan := range ir.Workflows {
		for _, s := range plan.States {
			if s.IsTool() {
				seen[ToolPin{s.Activity.ToolRef, s.Activity.Version, s.ToolHash}] = true
			}
		}
	}
	pins := make([]ToolPin, 0, len(seen))
	for p := range seen {
		pins = append(pins, p)
	}
	sortPins(pins)
	return pins
}

func sortPins(pins []ToolPin) {
	sort.Slice(pins, func(i, j int) bool {
		a, b := pins[i], pins[j]
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Hash < b.Hash
	})
}

// ToolsHash is the SHA-256 of the canonical sorted pin list. Catalog additions
// that no workflow pins never change it; an empty list hashes "[]".
func ToolsHash(pins []ToolPin) string {
	sorted := append([]ToolPin{}, pins...)
	sortPins(sorted)
	raw, _ := json.Marshal(sorted)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// PolicyRole is the active head of one tenant role definition.
type PolicyRole struct {
	Name     string `json:"name"`
	Version  int64  `json:"version"`
	BodyHash string `json:"body_hash"`
	Active   bool   `json:"active"`
}

// PolicyBundleInput is the whole-tenant authorization state a candidate was
// built under. It is deliberately broad: any role change, activation change or
// engine change makes earlier candidates stale (fail closed).
type PolicyBundleInput struct {
	EngineVersion     string       `json:"engine_version"`
	DefinitionVersion string       `json:"definition_version"`
	ActionCatalogHash string       `json:"action_catalog_hash"`
	Roles             []PolicyRole `json:"roles"`
}

var ErrPolicyBundle = errors.New("invalid policy bundle input")

// PolicyBundleHash hashes the input with roles sorted by name. Empty fields,
// duplicate role names or malformed hashes are errors.
func PolicyBundleHash(in PolicyBundleInput) (string, error) {
	if in.EngineVersion == "" || in.DefinitionVersion == "" || !ValidHash(in.ActionCatalogHash) || len(in.Roles) > 1024 {
		return "", ErrPolicyBundle
	}
	roles := append([]PolicyRole{}, in.Roles...)
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	for i, r := range roles {
		if r.Name == "" || r.Version < 1 || !ValidHash(r.BodyHash) || i > 0 && roles[i-1].Name == r.Name {
			return "", ErrPolicyBundle
		}
	}
	in.Roles = roles
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// RuntimeContractVersion names the platform contract a candidate was built for.
const RuntimeContractVersion = "runtime-contract-v1"

// RuntimeComponentsDoc records the component versions that shape a candidate.
type RuntimeComponentsDoc struct {
	StorageContract    string   `json:"storage_contract"`
	Evolution          string   `json:"evolution"`
	SupportedIR        []string `json:"supported_ir"`
	WorkflowDSL        []string `json:"workflow_dsl"`
	RuleDSL            string   `json:"rule_dsl"`
	ToolCatalog        string   `json:"tool_catalog"`
	Diff               string   `json:"diff"`
	RiskPolicies       []string `json:"risk_policies"`
	Verifier           string   `json:"verifier"`
	AcceptanceVocab    string   `json:"acceptance_vocabulary"`
	RequirementPolicy  string   `json:"requirement_policy"`
	RequirementSetSpec string   `json:"requirement_set"`
	AcceptanceSpec     string   `json:"acceptance_spec"`
}

func runtimeComponents(supported []string) RuntimeComponentsDoc {
	return RuntimeComponentsDoc{
		StorageContract: compiler.StorageContractVersion, Evolution: compiler.EvolutionVersion, SupportedIR: append([]string{}, supported...),
		WorkflowDSL: []string{workflow.Version, workflow.LegacyToolDSLVersion, workflow.ToolDSLVersion}, RuleDSL: rules.Version, ToolCatalog: tools.CatalogVersion,
		Diff: compiler.DiffVersion, RiskPolicies: risk.Versions(), Verifier: verification.VerifierVersion, AcceptanceVocab: intent.VocabularyVersion,
		RequirementPolicy: intent.RequirementPolicyVersion, RequirementSetSpec: intent.RequirementSetVersion, AcceptanceSpec: intent.AcceptanceSpecVersion,
	}
}

func hashComponents(doc RuntimeComponentsDoc) string {
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RuntimeComponents returns the current binary's component document and hash.
func RuntimeComponents() (RuntimeComponentsDoc, string) {
	doc := runtimeComponents(compiler.SupportedIRVersions())
	return doc, hashComponents(doc)
}
