package promotion

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

// Regression tests for the independent-review finding M12-F1: Restore recomputes
// everything that does not depend on the base release, so a body whose diff was
// shrunk and whose facts, risk and verifier report were then recomputed from the
// forged diff is self-consistent and restores. VerifyWithBase (used by
// Store.LoadCandidate) closes that by recomputing the diff, storage class and
// evolution plan from the candidate's own base release.

// secondReleaseFixture builds the 500 -> 600 threshold release (honest level HIGH)
// on top of a first release and returns both candidates with the base program.
func secondReleaseFixture(t *testing.T) (second, base Candidate, baseProgram compiler.Program, resolve BaseResolver) {
	t.Helper()
	first, program := firstRelease(t, orderOptions{})
	base = buildOK(t, first)
	p2, s2 := compileDocument(t, orderDocument(orderOptions{threshold: "600"}), compiler.PlatformCatalog())
	requirements, acceptance := artifacts(t, "600", 2)
	in := BuildInput{TenantID: tenantA, AppID: "app.orders", RequesterID: requester, Source: s2, SourceRevision: 2, Target: p2, Requirements: requirements, Acceptance: acceptance,
		Sandbox: evidenceFor(t, p2, 2), Tools: tools.EmptyCatalog(), Policy: policyInput(), Base: &BaseRelease{ArtifactHash: base.Hash(), Generation: 1, Program: program}}
	second = buildOK(t, in)
	resolve = func(hash string) (compiler.Program, error) {
		if hash == base.Hash() {
			return program, nil
		}
		return compiler.Program{}, errors.New("unknown release")
	}
	return second, base, program, resolve
}

// refit recomputes everything a forger can recompute after editing the diff or the
// storage delta: facts, assessment, HighRisk and the verifier report.
func refit(t *testing.T, b *BundleV2, d compiler.Diff) {
	t.Helper()
	program, err := compiler.Restore(b.IR.JSON, b.IR.Hash, compiler.SupportedIRVersions())
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]tools.Definition{}
	for _, def := range b.Dependencies.ToolDefinitions {
		defs[DefinitionKey(def.Ref, def.Version, def.Hash())] = def
	}
	facts, err := risk.Normalize(ReduceFacts(FactsInput{Diff: d, Storage: b.Storage, TargetPins: ToolPins(program.IR()), Definitions: defs, TenantID: b.TenantID, Volume: b.Risk.Facts.Volume}))
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := risk.Evaluate(facts)
	if err != nil {
		t.Fatal(err)
	}
	b.Diff.JSON, b.Diff.Hash = d.JSON(), d.Hash()
	b.Risk, b.HighRisk = toRisk(assessment, facts), assessment.HighRisk()
	set, _, _, err := intent.ParseRequirementSet(b.Requirements.JSON)
	if err != nil {
		t.Fatal(err)
	}
	spec, _, _, err := intent.ParseAcceptanceSpec(b.Acceptance.JSON)
	if err != nil {
		t.Fatal(err)
	}
	catalog := tools.EmptyCatalog()
	if len(b.Dependencies.ToolDefinitions) > 0 {
		if catalog, err = tools.NewCatalog(b.Dependencies.ToolDefinitions); err != nil {
			t.Fatal(err)
		}
	}
	report, err := verification.Verify(verification.Input{Program: program, Requirements: set, Acceptance: spec, StorageClass: b.Storage.Class, Changed: ChangedFor(d, b.Storage), Tools: catalog, TenantID: b.TenantID, SandboxEvidenceHash: b.Sandbox.EvidenceHash})
	if err != nil {
		t.Fatal(err)
	}
	b.Verification.Report, b.Verification.ReportHash, b.Verification.Outcome = report.JSON(), report.Hash(), report.Outcome
}

func TestVerifyWithBaseAcceptsHonestCandidates(t *testing.T) {
	supported := compiler.SupportedIRVersions()
	in, _ := firstRelease(t, orderOptions{})
	first := buildOK(t, in)
	if err := first.VerifyWithBase(nil, supported); err != nil {
		t.Fatalf("a first release has no base and must not need a resolver: %v", err)
	}
	second, _, _, resolve := secondReleaseFixture(t)
	if err := second.VerifyWithBase(resolve, supported); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWithBaseRefusesAnUnresolvableOrWrongBase(t *testing.T) {
	supported := compiler.SupportedIRVersions()
	second, _, _, _ := secondReleaseFixture(t)
	if err := second.VerifyWithBase(nil, supported); err == nil {
		t.Fatal("a candidate with a base was verified without a resolver")
	}
	if err := second.VerifyWithBase(func(string) (compiler.Program, error) { return compiler.Program{}, errors.New("gone") }, supported); err == nil {
		t.Fatal("a candidate whose base release is unavailable was verified")
	}
	// Resolving to some other release's program must not verify either.
	unrelated, _ := compileDocument(t, orderDocument(orderOptions{threshold: "700"}), compiler.PlatformCatalog())
	if err := second.VerifyWithBase(func(string) (compiler.Program, error) { return unrelated, nil }, supported); err == nil {
		t.Fatal("a candidate was verified against an unrelated program")
	}
}

func TestRestoreAloneAcceptsACoordinatedDiffForgeryButVerifyWithBaseRefusesIt(t *testing.T) {
	supported := compiler.SupportedIRVersions()
	second, _, _, resolve := secondReleaseFixture(t)
	honest, _ := second.V2()
	if !honest.HighRisk || honest.Risk.Level != "HIGH" {
		t.Fatalf("precondition: the honest change must rate HIGH: %s", honest.Risk.Level)
	}
	target, err := compiler.Restore(honest.IR.JSON, honest.IR.Hash, supported)
	if err != nil {
		t.Fatal(err)
	}
	forgeries := map[string]func(d *compiler.Diff){
		"drop the approval-gate aspect": func(d *compiler.Diff) {
			for i := range d.Changes {
				kept := []string{}
				for _, aspect := range d.Changes[i].Aspects {
					if aspect != compiler.AspectApprovalGate {
						kept = append(kept, aspect)
					}
				}
				d.Changes[i].Aspects = kept
			}
			d.MoneyConditions = []compiler.MoneyConditionChange{}
			d.ApprovalGates = []compiler.ApprovalGateChange{}
		},
		"diff of the target against itself": func(d *compiler.Diff) {
			self, derr := compiler.ComputeDiff(target, target)
			if derr != nil {
				t.Fatal(derr)
			}
			*d = self
		},
	}
	for name, edit := range forgeries {
		b := honest
		d, derr := compiler.ParseDiff(b.Diff.JSON, b.Diff.Hash)
		if derr != nil {
			t.Fatal(derr)
		}
		edit(&d)
		refit(t, &b, d)
		raw, merr := json.Marshal(b)
		if merr != nil {
			t.Fatal(merr)
		}
		forged, rerr := Restore(raw, Hash(raw), supported)
		if rerr != nil {
			// Restore is allowed to get stricter later; the property that matters is below.
			t.Logf("%s: Restore already refuses: %v", name, rerr)
			continue
		}
		fb, _ := forged.V2()
		if fb.Risk.Level == honest.Risk.Level && fb.HighRisk == honest.HighRisk {
			t.Fatalf("%s: the forgery did not lower the risk, so the test proves nothing", name)
		}
		if err := forged.VerifyWithBase(resolve, supported); err == nil {
			t.Fatalf("%s: a forged diff (level %s -> %s, HighRisk %t -> %t) passed the base recomputation", name, honest.Risk.Level, fb.Risk.Level, honest.HighRisk, fb.HighRisk)
		}
	}
}

// A TRANSFORM release (required field with a backfill) relabelled ADDITIVE lowers
// the level and the separation requirement; only the base recomputation sees it.
func TestStorageClassRelabelIsRefusedByVerifyWithBase(t *testing.T) {
	supported := compiler.SupportedIRVersions()
	first, firstProgram := firstRelease(t, orderOptions{})
	base := buildOK(t, first)
	doc := orderDocument(orderOptions{})
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == "field.order.note" {
			doc.Definitions[i].Field.Required = true
		}
	}
	p2, s2 := compileDocument(t, doc, compiler.PlatformCatalog())
	migration := intent.RequirementV2{ID: "migration", Category: intent.CategoryDataMigration, Requiredness: intent.Mandatory, RequirednessBasis: intent.BasisPolicyFloor, Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: json.RawMessage(`{"max_class":"TRANSFORM"}`)}
	set := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.orders", Requirements: []intent.RequirementV2{migration, thresholdRequirement("500")}}
	setJSON, err := set.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{
		{ID: "above", RequirementID: "po-approval", Given: map[string]json.RawMessage{"field.order.amount": json.RawMessage(`{"amount":"500.01","currency":"USD"}`)}, Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}},
		{ID: "migration-holds", RequirementID: "migration", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}},
	}}
	specJSON, err := spec.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	in := BuildInput{TenantID: tenantA, AppID: "app.orders", RequesterID: requester, Source: s2, SourceRevision: 2, Target: p2,
		Requirements: StoredRequirements{AppID: "app.orders", Version: 2, Hash: set.Hash(), DecidedBy: requester, Body: setJSON},
		Acceptance:   StoredAcceptance{ID: specID, AppID: "app.orders", RequirementsHash: set.Hash(), Hash: spec.Hash(), AuthorID: acceptor, Body: specJSON},
		Sandbox:      evidenceFor(t, p2, 2), Tools: tools.EmptyCatalog(), Policy: policyInput(),
		Base:   &BaseRelease{ArtifactHash: base.Hash(), Generation: 1, Program: firstProgram},
		Intent: compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.order", FieldID: "field.order.note", When: "MISSING_OR_NULL", Value: json.RawMessage(`"regular"`)}}}}
	c := buildOK(t, in)
	honest, _ := c.V2()
	if honest.Storage.Class != compiler.StorageTransform {
		t.Fatalf("precondition: %s", honest.Storage.Class)
	}
	resolve := func(hash string) (compiler.Program, error) {
		if hash == base.Hash() {
			return firstProgram, nil
		}
		return compiler.Program{}, errors.New("unknown release")
	}
	if err = c.VerifyWithBase(resolve, supported); err != nil {
		t.Fatalf("the honest TRANSFORM candidate must verify: %v", err)
	}
	b := honest
	b.Storage.Class, b.Storage.Reasons = compiler.StorageAdditive, []string{"ADD_OPTIONAL"}
	d, err := compiler.ParseDiff(b.Diff.JSON, b.Diff.Hash)
	if err != nil {
		t.Fatal(err)
	}
	refit(t, &b, d)
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	forged, err := Restore(raw, Hash(raw), supported)
	if err != nil {
		t.Logf("Restore already refuses the relabel: %v", err)
		return
	}
	if err = forged.VerifyWithBase(resolve, supported); err == nil {
		fb, _ := forged.V2()
		t.Fatalf("a TRANSFORM release relabelled %s (level %s -> %s) passed the base recomputation", fb.Storage.Class, honest.Risk.Level, fb.Risk.Level)
	}
}
