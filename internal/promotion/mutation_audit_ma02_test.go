package promotion

import (
	"reflect"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

func TestRuntimeComponentsRecordTheLiveVersions(t *testing.T) {
	want := RuntimeComponentsDoc{
		StorageContract: compiler.StorageContractVersion, Evolution: compiler.EvolutionVersion, SupportedIR: compiler.SupportedIRVersions(),
		WorkflowDSL: []string{workflow.Version, workflow.TenantRoleVersion, workflow.ToolDSLVersion}, RuleDSL: rules.Version, ToolCatalog: tools.CatalogVersion,
		Diff: compiler.DiffVersion, RiskPolicies: risk.Versions(), Verifier: verification.VerifierVersion, AcceptanceVocab: intent.VocabularyVersion,
		RequirementPolicy: intent.RequirementPolicyVersion, RequirementSetSpec: intent.RequirementSetVersion, AcceptanceSpec: intent.AcceptanceSpecVersion,
	}
	got, hash := RuntimeComponents()
	if !reflect.DeepEqual(got, want) || hash != hashComponents(want) {
		t.Fatalf("the recorded components are not the live versions:\n got %+v\nwant %+v", got, want)
	}
}
