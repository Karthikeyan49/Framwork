package promotion

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/risk"
)

func irOf(pins []ToolPin) compiler.IR { return compiler.IR{} }

var (
	h1 = strings.Repeat("1", 64)
	h2 = strings.Repeat("2", 64)
	h3 = strings.Repeat("3", 64)
)

func freshPair() (BoundState, CurrentState) {
	pin := ToolPin{"tool.a.b", "1.0.0", h3}
	facts := risk.Facts{Storage: risk.StorageFact{Class: risk.ClassEqual, Reasons: []string{}}, Volume: risk.VolumeNone, Tools: []risk.ToolFact{}, ToolDeclarations: []risk.ToolDeclFact{}}
	bound := BoundState{BaseRelease: h1, BaseGeneration: 4, BaseSchemaHash: h2, PolicyBundleHash: h3, ToolPins: []ToolPin{pin}, IRVersion: "1.2.0", RiskPolicyVersion: risk.PolicyV1, RiskLevel: risk.Low, RiskFacts: facts, EvidenceHash: h1}
	current := CurrentState{ActiveRelease: h1, ActiveGeneration: 4, BaseSchemaHash: h2, PolicyBundleHash: h3, ToolStatus: func(ToolPin) ToolPinStatus { return ToolOK },
		SupportedIR: []string{"1.0.0", "1.2.0"}, Volume: risk.VolumeNone, Evidence: EvidenceState{Exists: true, Frozen: true, Hash: h1}}
	return bound, current
}

func TestStalenessReasons(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*BoundState, *CurrentState)
		want   []StaleReason
	}{
		{"fresh", func(*BoundState, *CurrentState) {}, []StaleReason{}},
		{"base changed", func(b *BoundState, c *CurrentState) { c.ActiveRelease = h2 }, []StaleReason{StaleBaseChanged}},
		{"base appeared", func(b *BoundState, c *CurrentState) {
			b.BaseRelease, b.BaseGeneration, b.BaseSchemaHash = "", 0, ""
			c.BaseSchemaHash = h2
		}, []StaleReason{StaleBaseChanged, StaleSchemaChanged}},
		{"base vanished", func(b *BoundState, c *CurrentState) { c.ActiveRelease, c.ActiveGeneration = "", 0 }, []StaleReason{StaleBaseChanged}},
		{"aba generation", func(b *BoundState, c *CurrentState) { c.ActiveGeneration = 6 }, []StaleReason{StaleBaseGeneration}},
		{"schema", func(b *BoundState, c *CurrentState) { c.BaseSchemaHash = h3 }, []StaleReason{StaleSchemaChanged}},
		{"policy", func(b *BoundState, c *CurrentState) { c.PolicyBundleHash = h1 }, []StaleReason{StalePolicyChanged}},
		{"tool changed", func(b *BoundState, c *CurrentState) {
			c.ToolStatus = func(ToolPin) ToolPinStatus { return ToolChanged }
		}, []StaleReason{StaleToolChanged}},
		{"tool removed", func(b *BoundState, c *CurrentState) {
			c.ToolStatus = func(ToolPin) ToolPinStatus { return ToolRemoved }
		}, []StaleReason{StaleToolRemoved}},
		{"tool tenant", func(b *BoundState, c *CurrentState) {
			c.ToolStatus = func(ToolPin) ToolPinStatus { return ToolTenantDenied }
		}, []StaleReason{StaleToolTenantDenied}},
		{"tool status unknown fails closed", func(b *BoundState, c *CurrentState) { c.ToolStatus = func(ToolPin) ToolPinStatus { return "WEIRD" } }, []StaleReason{StaleToolRemoved}},
		{"nil tool resolver fails closed", func(b *BoundState, c *CurrentState) { c.ToolStatus = nil }, []StaleReason{StaleToolRemoved}},
		{"no pins need no resolver", func(b *BoundState, c *CurrentState) { b.ToolPins, c.ToolStatus = nil, nil }, []StaleReason{}},
		{"volume escalates risk", func(b *BoundState, c *CurrentState) {
			b.RiskFacts.Storage = risk.StorageFact{Class: risk.ClassAdditive, Reasons: []string{"ADD_OPTIONAL"}}
			c.Volume = risk.VolumeUnverified
		}, []StaleReason{StaleRiskEscalated}},
		{"volume that does not change the level", func(b *BoundState, c *CurrentState) { c.Volume = risk.VolumeUnverified }, []StaleReason{}},
		{"invalid volume fails closed", func(b *BoundState, c *CurrentState) { c.Volume = "" }, []StaleReason{StaleRiskEscalated}},
		{"evidence missing", func(b *BoundState, c *CurrentState) { c.Evidence = EvidenceState{} }, []StaleReason{StaleEvidenceInvalid}},
		{"evidence not frozen", func(b *BoundState, c *CurrentState) { c.Evidence.Frozen = false }, []StaleReason{StaleEvidenceInvalid}},
		{"evidence hash differs", func(b *BoundState, c *CurrentState) { c.Evidence.Hash = h2 }, []StaleReason{StaleEvidenceInvalid}},
		{"unsupported ir", func(b *BoundState, c *CurrentState) { c.SupportedIR = []string{"1.0.0"} }, []StaleReason{StaleUnsupportedRuntime}},
		{"several, sorted", func(b *BoundState, c *CurrentState) {
			c.PolicyBundleHash, c.Evidence.Frozen, c.SupportedIR = h1, false, nil
		}, []StaleReason{StaleEvidenceInvalid, StalePolicyChanged, StaleUnsupportedRuntime}},
	}
	for _, c := range cases {
		bound, current := freshPair()
		c.mutate(&bound, &current)
		got := Staleness(bound, current)
		if got == nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
	// A first release with no base on either side is fresh.
	bound, current := freshPair()
	bound.BaseRelease, bound.BaseGeneration, bound.BaseSchemaHash = "", 0, ""
	current.ActiveRelease, current.ActiveGeneration, current.BaseSchemaHash = "", 0, ""
	if got := Staleness(bound, current); len(got) != 0 {
		t.Fatalf("first release: %v", got)
	}
}

func TestToolPinsAreSortedDistinctAndIgnoreUnrelatedCatalogChanges(t *testing.T) {
	if got := ToolPins(irOf(nil)); got == nil || len(got) != 0 {
		t.Fatal("no pins must be an empty non-nil list")
	}
	pins := []ToolPin{{"tool.b.b", "1.0.0", h2}, {"tool.a.a", "1.0.0", h1}, {"tool.a.a", "2.0.0", h1}}
	a := ToolsHash(pins)
	b := ToolsHash([]ToolPin{pins[2], pins[0], pins[1]})
	if a != b || len(a) != 64 || ToolsHash(pins[:2]) == a || ToolsHash(nil) != ToolsHash([]ToolPin{}) {
		t.Fatal("tools hash is order dependent or insensitive")
	}
	other := append([]ToolPin{}, pins...)
	other[0].Hash = h3
	if ToolsHash(other) == a {
		t.Fatal("a re-pinned hash did not change the tools hash")
	}
}

func TestPolicyBundleHash(t *testing.T) {
	roles := []PolicyRole{{"member", 1, h1, true}, {"owner", 3, h2, true}, {"auditor", 2, h3, false}}
	in := PolicyBundleInput{EngineVersion: "m12-native-v9", DefinitionVersion: "tenant-capabilities-v1", ActionCatalogHash: h1, Roles: roles}
	a, err := PolicyBundleHash(in)
	if err != nil || len(a) != 64 {
		t.Fatal(err)
	}
	in.Roles = []PolicyRole{roles[2], roles[0], roles[1]}
	if b, _ := PolicyBundleHash(in); a != b {
		t.Fatal("role order changed the hash")
	}
	for name, mutate := range map[string]func(*PolicyBundleInput){
		"role version":    func(p *PolicyBundleInput) { p.Roles[0].Version = 9 },
		"role body":       func(p *PolicyBundleInput) { p.Roles[0].BodyHash = h1 },
		"role activation": func(p *PolicyBundleInput) { p.Roles[0].Active = true },
		"role added":      func(p *PolicyBundleInput) { p.Roles = append(p.Roles, PolicyRole{"zeta", 1, h1, true}) },
		"engine":          func(p *PolicyBundleInput) { p.EngineVersion = "m13" },
		"definition":      func(p *PolicyBundleInput) { p.DefinitionVersion = "v2" },
		"actions":         func(p *PolicyBundleInput) { p.ActionCatalogHash = h2 },
	} {
		x := in
		x.Roles = append([]PolicyRole{}, in.Roles...)
		mutate(&x)
		if got, err := PolicyBundleHash(x); err != nil || got == a {
			t.Errorf("%s did not change the policy hash (%v)", name, err)
		}
	}
	for name, mutate := range map[string]func(*PolicyBundleInput){
		"duplicate role": func(p *PolicyBundleInput) { p.Roles = append(p.Roles, p.Roles[0]) },
		"empty engine":   func(p *PolicyBundleInput) { p.EngineVersion = "" },
		"bad actions":    func(p *PolicyBundleInput) { p.ActionCatalogHash = "x" },
		"bad role hash":  func(p *PolicyBundleInput) { p.Roles[0].BodyHash = "x" },
		"zero version":   func(p *PolicyBundleInput) { p.Roles[0].Version = 0 },
		"empty name":     func(p *PolicyBundleInput) { p.Roles[0].Name = "" },
	} {
		x := in
		x.Roles = append([]PolicyRole{}, in.Roles...)
		mutate(&x)
		if _, err := PolicyBundleHash(x); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRuntimeComponentsHashCoversEveryComponent(t *testing.T) {
	doc, hash := RuntimeComponents()
	if len(hash) != 64 || hash != hashComponents(doc) {
		t.Fatal("hash")
	}
	mutations := []func(*RuntimeComponentsDoc){
		func(d *RuntimeComponentsDoc) { d.StorageContract += "x" }, func(d *RuntimeComponentsDoc) { d.Evolution += "x" },
		func(d *RuntimeComponentsDoc) { d.SupportedIR = d.SupportedIR[1:] }, func(d *RuntimeComponentsDoc) { d.WorkflowDSL = d.WorkflowDSL[:1] },
		func(d *RuntimeComponentsDoc) { d.RuleDSL += "x" }, func(d *RuntimeComponentsDoc) { d.ToolCatalog += "x" }, func(d *RuntimeComponentsDoc) { d.Diff += "x" },
		func(d *RuntimeComponentsDoc) { d.RiskPolicies = nil }, func(d *RuntimeComponentsDoc) { d.Verifier += "x" }, func(d *RuntimeComponentsDoc) { d.AcceptanceVocab += "x" },
		func(d *RuntimeComponentsDoc) { d.RequirementPolicy += "x" }, func(d *RuntimeComponentsDoc) { d.RequirementSetSpec += "x" }, func(d *RuntimeComponentsDoc) { d.AcceptanceSpec += "x" },
	}
	for i, m := range mutations {
		x := runtimeComponents([]string{"1.0.0", "1.1.0"})
		m(&x)
		if hashComponents(x) == hashComponents(runtimeComponents([]string{"1.0.0", "1.1.0"})) {
			t.Errorf("mutation %d did not change the hash", i)
		}
	}
}
