package promotion

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

const (
	tenantA   = "11111111-1111-4111-8111-111111111111"
	requester = "22222222-2222-4222-8222-222222222222"
	acceptor  = "33333333-3333-4333-8333-333333333333"
	freezer   = "44444444-4444-4444-8444-444444444444"
	datasetID = "55555555-5555-4555-8555-555555555555"
	specID    = "66666666-6666-4666-8666-666666666666"
)

// -------------------------------------------------------------------- fixtures

type orderOptions struct {
	threshold string
	label     string
}

func orderDocument(o orderOptions) metadata.Document {
	if o.threshold == "" {
		o.threshold = "500"
	}
	if o.label == "" {
		o.label = "Order"
	}
	version, namespace := metadata.WorkflowSchemaVersion, "example.orders"
	def := func(id, label, kind string) metadata.Definition {
		return metadata.Definition{ID: id, SchemaVersion: version, Namespace: namespace, Label: label, Kind: kind}
	}
	doc := metadata.Document{SchemaVersion: version, AppID: "app.orders", Namespace: namespace}
	entity := def("entity.order", o.label, "entity")
	entity.Entity = &metadata.Entity{Fields: []string{"field.order.amount", "field.order.id", "field.order.note"}}
	doc.Definitions = append(doc.Definitions, entity)
	add := func(id, typ string, required bool, currency string) {
		d := def(id, id, "field")
		d.Field = &metadata.Field{EntityRef: "entity.order", Type: typ, Required: required, Currency: currency}
		doc.Definitions = append(doc.Definitions, d)
	}
	add("field.order.id", "string", true, "")
	add("field.order.amount", "money", true, "USD")
	add("field.order.note", "string", false, "")
	condition := &metadata.Expression{Op: "gt", Args: []metadata.Expression{{Op: "field", FieldRef: "field.order.amount"},
		{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + o.threshold + `","currency":"USD"}`)}}}
	wf := def("workflow.order.approve", "Order approval", "workflow")
	wf.Workflow = &metadata.Workflow{DSLVersion: "2.0.0", Version: "1.0.0", TimeoutMS: 60000, EntityRef: "entity.order", Trigger: "manual", InitialState: "state.start", States: []metadata.WorkflowState{
		{ID: "state.approval", Type: "approval", ApprovalRoles: []string{"admin"}, ApprovalTimeoutMS: 30000, Transitions: []metadata.Transition{{To: "state.complete", On: "approved"}, {To: "state.failed", On: "rejected"}}},
		{ID: "state.complete", Type: "complete", Transitions: []metadata.Transition{}},
		{ID: "state.failed", Type: "failed", Transitions: []metadata.Transition{}},
		{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.approval", Condition: condition}, {To: "state.complete"}}},
	}}
	doc.Definitions = append(doc.Definitions, wf)
	return doc
}

func compileDocument(t *testing.T, doc metadata.Document, catalog compiler.Catalog) (compiler.Program, metadata.Source) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	s, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := compiler.Compile(s, catalog)
	if !p.Valid() {
		t.Fatalf("compile: %+v", ds)
	}
	return p, s
}

func thresholdRequirement(amount string) intent.RequirementV2 {
	raw, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: amount,
		ApproverRoles: []string{"admin"}, Context: map[string]json.RawMessage{"field.order.note": json.RawMessage(`"regular"`)}})
	return intent.RequirementV2{ID: "po-approval", Category: intent.CategoryApprovalThreshold, Requiredness: intent.Mandatory, RequirednessBasis: intent.BasisPolicyFloor, Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: raw}
}

// artifacts builds the stored requirement set and acceptance rows for a threshold.
func artifacts(t *testing.T, amount string, version int64) (StoredRequirements, StoredAcceptance) {
	t.Helper()
	set := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.orders", Requirements: []intent.RequirementV2{thresholdRequirement(amount)}}
	setJSON, err := set.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	above := fmt.Sprintf("%s.01", amount)
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{
		{ID: "above-threshold", RequirementID: "po-approval", Given: map[string]json.RawMessage{"field.order.amount": json.RawMessage(`{"amount":"` + above + `","currency":"USD"}`)}, Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}},
	}}
	specJSON, err := spec.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return StoredRequirements{AppID: "app.orders", Version: version, Hash: set.Hash(), DecidedBy: requester, Body: setJSON},
		StoredAcceptance{ID: specID, AppID: "app.orders", RequirementsHash: set.Hash(), Hash: spec.Hash(), AuthorID: acceptor, Body: specJSON}
}

func evidenceFor(t *testing.T, p compiler.Program, revision int64) FrozenSandbox {
	t.Helper()
	_, schema, err := compiler.Storage(p)
	if err != nil {
		t.Fatal(err)
	}
	// Spaced like PostgreSQL's jsonb text, so the exact bytes are what is hashed.
	body := fmt.Sprintf(`{"app_id": "app.orders", "data_mode": "SYNTHETIC_SEPARATE", "dataset_id": "%s", "ir_hash": "%s", "schema_hash": "%s", "metadata_revision": %d, "schema_version": "sandbox-evidence-v1"}`, datasetID, p.Hash(), schema, revision)
	return FrozenSandbox{DatasetID: datasetID, EvidenceHash: sha([]byte(body)), FrozenBy: freezer, Evidence: body}
}

func policyInput() PolicyBundleInput {
	return PolicyBundleInput{EngineVersion: "m12-native-v9", DefinitionVersion: "tenant-capabilities-v1", ActionCatalogHash: strings.Repeat("a", 64), Roles: []PolicyRole{{"admin", 1, strings.Repeat("b", 64), true}, {"owner", 1, strings.Repeat("c", 64), true}}}
}

func firstRelease(t *testing.T, o orderOptions) (BuildInput, compiler.Program) {
	t.Helper()
	p, s := compileDocument(t, orderDocument(o), compiler.PlatformCatalog())
	requirements, acceptance := artifacts(t, "500", 1)
	return BuildInput{TenantID: tenantA, AppID: "app.orders", RequesterID: requester, Source: s, SourceRevision: 1, Target: p, Requirements: requirements, Acceptance: acceptance,
		Sandbox: evidenceFor(t, p, 1), Tools: tools.EmptyCatalog(), Policy: policyInput()}, p
}

func buildOK(t *testing.T, in BuildInput) Candidate {
	t.Helper()
	c, err := BuildCandidate(in)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return c
}

// retamper decodes a candidate, applies a change and re-encodes it canonically
// with a fresh hash, so only the content (never the encoding) is wrong.
func retamper(t *testing.T, c Candidate, mutate func(*BundleV2)) (Candidate, error) {
	t.Helper()
	b, ok := c.V2()
	if !ok {
		t.Fatal("not a v2 candidate")
	}
	mutate(&b)
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return Restore(raw, Hash(raw), compiler.SupportedIRVersions())
}

// --------------------------------------------------------------------- tests

func TestBuildCandidateFirstReleaseRoundTrips(t *testing.T) {
	in, program := firstRelease(t, orderOptions{})
	c := buildOK(t, in)
	h := c.Header()
	if h.Version != BundleVersion2 || h.TenantID != tenantA || h.AppID != "app.orders" || h.IRHash != program.Hash() || h.RequesterID != requester || h.BaseRelease != "" {
		t.Fatalf("%+v", h)
	}
	b, _ := c.V2()
	// The unconditional approval gate is new, so the release rates HIGH and is never
	// self-approvable; the verifier ran and passed; the storage is a first release.
	if !h.HighRisk || b.Risk.Level != "HIGH" || b.Risk.Separation.Approvals != 1 || !reflect.DeepEqual(b.Risk.Separation.Excluded, []string{"requester", "acceptance_author"}) {
		t.Fatalf("risk %+v", b.Risk)
	}
	if b.Verification.Outcome != verification.Pass || b.Storage.Class != compiler.StorageAdditive || !b.Storage.FirstRelease || b.Evolution == nil && b.Storage.EvolutionPlanHash != "" {
		t.Fatalf("%+v %+v", b.Verification, b.Storage)
	}
	if len(c.JSON()) > MaxBundleBytes {
		t.Fatal("size")
	}
	restored, err := Restore(c.JSON(), c.Hash(), compiler.SupportedIRVersions())
	if err != nil || !bytes.Equal(restored.JSON(), c.JSON()) {
		t.Fatalf("restore: %v", err)
	}
	program2, err := restored.Program(compiler.SupportedIRVersions())
	if err != nil || program2.Hash() != program.Hash() {
		t.Fatal("program", err)
	}
	if err = restored.VerifyAgainstBase(compiler.Program{}, compiler.SupportedIRVersions()); err != nil {
		t.Fatal("first release against no base:", err)
	}
	if err = restored.VerifyAgainstBase(program, compiler.SupportedIRVersions()); err == nil {
		t.Fatal("a first release accepted a base")
	}
	again := buildOK(t, in)
	if !bytes.Equal(again.JSON(), c.JSON()) || again.Hash() != c.Hash() {
		t.Fatal("building is not deterministic")
	}
}

func TestBundleSurvivesHTMLSensitiveText(t *testing.T) {
	in, _ := firstRelease(t, orderOptions{label: `R&D <orders> "special" \ end`})
	c := buildOK(t, in)
	if _, err := Restore(c.JSON(), c.Hash(), compiler.SupportedIRVersions()); err != nil {
		t.Fatal("escaping changed the embedded documents:", err)
	}
}

func TestEveryTamperedFieldIsRejectedOnRestore(t *testing.T) {
	in, _ := firstRelease(t, orderOptions{})
	c := buildOK(t, in)
	other := strings.Repeat("e", 64)
	mutations := map[string]func(*BundleV2){
		"high risk flag cleared": func(b *BundleV2) { b.HighRisk = false },
		"risk level downgraded":  func(b *BundleV2) { b.Risk.Level = "LOW" },
		"risk level raised":      func(b *BundleV2) { b.Risk.Level = "CRITICAL" },
		"risk reasons edited":    func(b *BundleV2) { b.Risk.Reasons = []string{"S1"} },
		"risk facts cleared":     func(b *BundleV2) { b.Risk.Facts.ApprovalGateChanged = false },
		"risk facts hash":        func(b *BundleV2) { b.Risk.FactsHash = other },
		"risk policy hash":       func(b *BundleV2) { b.Risk.PolicyHash = other },
		"risk policy version":    func(b *BundleV2) { b.Risk.PolicyVersion = "release-risk-v9" },
		"separation weakened":    func(b *BundleV2) { b.Risk.Separation.Excluded = []string{} },
		"rollback class":         func(b *BundleV2) { b.Risk.Rollback = risk.RollbackPointerSafe },
		"verification outcome":   func(b *BundleV2) { b.Verification.Outcome = verification.Fail },
		"verification report": func(b *BundleV2) {
			b.Verification.Report = json.RawMessage(strings.Replace(string(b.Verification.Report), `"PASS"`, `"FAIL"`, 1))
		},
		"verification report hash": func(b *BundleV2) { b.Verification.ReportHash = other },
		"verifier version":         func(b *BundleV2) { b.Verification.VerifierVersion = "verifier-v9" },
		"requirements content": func(b *BundleV2) {
			b.Requirements.JSON = json.RawMessage(strings.Replace(string(b.Requirements.JSON), `"amount":"500"`, `"amount":"900"`, 1))
		},
		"requirements hash":   func(b *BundleV2) { b.Requirements.Hash = other },
		"requirements policy": func(b *BundleV2) { b.Requirements.PolicyVersion = "requirement-policy-v9" },
		"acceptance content": func(b *BundleV2) {
			b.Acceptance.JSON = json.RawMessage(strings.Replace(string(b.Acceptance.JSON), `500.01`, `499.99`, 1))
		},
		"acceptance hash":                    func(b *BundleV2) { b.Acceptance.Hash = other },
		"acceptance author is the requester": func(b *BundleV2) { b.Acceptance.AuthorID = b.RequesterID },
		"acceptance bound elsewhere":         func(b *BundleV2) { b.Acceptance.RequirementsHash = other },
		"sandbox evidence text": func(b *BundleV2) {
			b.Sandbox.Evidence = strings.Replace(b.Sandbox.Evidence, "SYNTHETIC_SEPARATE", "PRODUCTION", 1)
		},
		"sandbox evidence hash": func(b *BundleV2) { b.Sandbox.EvidenceHash = other },
		"sandbox dataset":       func(b *BundleV2) { b.Sandbox.DatasetID = specID },
		"diff content": func(b *BundleV2) {
			b.Diff.JSON = json.RawMessage(strings.Replace(string(b.Diff.JSON), `"first_release":true`, `"first_release":false`, 1))
		},
		"diff hash":                func(b *BundleV2) { b.Diff.Hash = other },
		"storage class":            func(b *BundleV2) { b.Storage.Class = compiler.StorageEqual },
		"storage breaking":         func(b *BundleV2) { b.Storage.Class = compiler.StorageBreaking },
		"tool pins":                func(b *BundleV2) { b.Dependencies.ToolPins = []ToolPin{{"tool.a.b", "1.0.0", other}} },
		"tools hash":               func(b *BundleV2) { b.Dependencies.ToolsHash = other },
		"policy bundle hash":       func(b *BundleV2) { b.Dependencies.PolicyBundleHash = other },
		"policy bundle role":       func(b *BundleV2) { b.Dependencies.PolicyBundle.Roles[0].Version = 9 },
		"target schema hash":       func(b *BundleV2) { b.Dependencies.TargetSchemaHash = other },
		"base schema without base": func(b *BundleV2) { b.Dependencies.BaseSchemaHash = other },
		"runtime components":       func(b *BundleV2) { b.RuntimeContract.Components.Verifier = "verifier-v9" },
		"runtime components hash":  func(b *BundleV2) { b.RuntimeContract.ComponentsHash = other },
		"ir content": func(b *BundleV2) {
			b.IR.JSON = json.RawMessage(strings.Replace(string(b.IR.JSON), "Order approval", "Order approvaL", 1))
		},
		"ir hash":    func(b *BundleV2) { b.IR.Hash = other },
		"ir version": func(b *BundleV2) { b.IR.Version = "1.0.0" },
		"source content": func(b *BundleV2) {
			b.Source.JSON = json.RawMessage(strings.Replace(string(b.Source.JSON), "Order approval", "Order approvaL", 1))
		},
		"source hash":                     func(b *BundleV2) { b.Source.Hash = other },
		"source revision":                 func(b *BundleV2) { b.Source.Revision = 0 },
		"base release without generation": func(b *BundleV2) { b.BaseRelease = other },
		"generation without base":         func(b *BundleV2) { b.BaseGeneration = 3 },
		"tenant":                          func(b *BundleV2) { b.TenantID = "not-a-uuid" },
		"application":                     func(b *BundleV2) { b.AppID = "app.other" },
		"requester":                       func(b *BundleV2) { b.RequesterID = "not-a-uuid" },
		"version":                         func(b *BundleV2) { b.Version = "2.0.1" },
	}
	for name, mutate := range mutations {
		if _, err := retamper(t, c, mutate); !errors.Is(err, ErrGate) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
	// Encoding-level tampering.
	raw := c.JSON()
	if _, err := Restore(append(append([]byte(nil), raw...), ' '), Hash(append(append([]byte(nil), raw...), ' ')), compiler.SupportedIRVersions()); err == nil {
		t.Error("non-canonical encoding accepted")
	}
	if _, err := Restore(raw, other, compiler.SupportedIRVersions()); err == nil {
		t.Error("wrong hash accepted")
	}
	extra := bytes.Replace(raw, []byte(`{"Version":"2.0.0"`), []byte(`{"Extra":1,"Version":"2.0.0"`), 1)
	if _, err := Restore(extra, Hash(extra), compiler.SupportedIRVersions()); err == nil {
		t.Error("unknown field accepted")
	}
	if _, err := Restore(raw, c.Hash(), []string{"1.0.0"}); err == nil {
		t.Error("unsupported IR accepted")
	}
}

func TestRejectionsCarryTheEvidence(t *testing.T) {
	reject := func(in BuildInput) *Rejection {
		t.Helper()
		_, err := BuildCandidate(in)
		var r *Rejection
		if !errors.As(err, &r) {
			t.Fatalf("expected a rejection, got %v", err)
		}
		return r
	}
	// The release behaves differently from the independent requirement.
	in, _ := firstRelease(t, orderOptions{threshold: "5000"})
	r := reject(in)
	if r.Reason != RejectVerificationFailed || r.Report == nil || r.Report.Outcome != verification.Fail || r.Assessment == nil {
		t.Fatalf("%+v", r)
	}
	// A financial release with no requirement at all is blocked, not accepted.
	in, _ = firstRelease(t, orderOptions{})
	empty := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.orders", Requirements: []intent.RequirementV2{}}
	emptyJSON, _ := empty.Canonical()
	in.Requirements = StoredRequirements{AppID: "app.orders", Version: 1, Hash: empty.Hash(), DecidedBy: requester, Body: emptyJSON}
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: empty.Hash(), Cases: []intent.AcceptanceCase{}}
	specJSON, _ := spec.Canonical()
	in.Acceptance = StoredAcceptance{ID: specID, AppID: "app.orders", RequirementsHash: empty.Hash(), Hash: spec.Hash(), AuthorID: acceptor, Body: specJSON}
	r = reject(in)
	if r.Reason != RejectVerificationBlocked || r.Report == nil || len(r.Report.Findings) == 0 || r.Report.Findings[0].Code != verification.CodeOmittedRequirement {
		t.Fatalf("%+v %+v", r, r.Report)
	}
}

func TestBuildCandidateRefusesInvalidInputs(t *testing.T) {
	base, _ := firstRelease(t, orderOptions{})
	cases := map[string]func(*BuildInput){
		"bad tenant":                  func(in *BuildInput) { in.TenantID = "x" },
		"bad requester":               func(in *BuildInput) { in.RequesterID = "" },
		"revision zero":               func(in *BuildInput) { in.SourceRevision = 0 },
		"zero program":                func(in *BuildInput) { in.Target = compiler.Program{} },
		"app mismatch":                func(in *BuildInput) { in.AppID = "app.other" },
		"acceptance by the requester": func(in *BuildInput) { in.Acceptance.AuthorID = in.RequesterID },
		"requirements row hash":       func(in *BuildInput) { in.Requirements.Hash = strings.Repeat("0", 64) },
		"requirements not canonical": func(in *BuildInput) {
			in.Requirements.Body = append(append(json.RawMessage(nil), in.Requirements.Body...), ' ')
		},
		"acceptance row hash":        func(in *BuildInput) { in.Acceptance.Hash = strings.Repeat("0", 64) },
		"acceptance for other set":   func(in *BuildInput) { in.Acceptance.RequirementsHash = strings.Repeat("0", 64) },
		"evidence for another build": func(in *BuildInput) { in.Sandbox = evidenceFor(t, in.Target, 7) },
		"evidence hash":              func(in *BuildInput) { in.Sandbox.EvidenceHash = strings.Repeat("0", 64) },
		"evidence production mode": func(in *BuildInput) {
			in.Sandbox.Evidence = strings.Replace(in.Sandbox.Evidence, "SYNTHETIC_SEPARATE", "PRODUCTION", 1)
			in.Sandbox.EvidenceHash = sha([]byte(in.Sandbox.Evidence))
		},
		"policy bundle empty": func(in *BuildInput) { in.Policy = PolicyBundleInput{} },
		"base without generation": func(in *BuildInput) {
			p, _ := compileDocument(t, orderDocument(orderOptions{}), compiler.PlatformCatalog())
			in.Base = &BaseRelease{ArtifactHash: strings.Repeat("a", 64), Generation: 0, Program: p}
		},
	}
	for name, mutate := range cases {
		in := base
		mutate(&in)
		_, err := BuildCandidate(in)
		var r *Rejection
		if err == nil || errors.As(err, &r) || !errors.Is(err, ErrGate) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSecondReleaseBindsAndVerifiesAgainstItsBase(t *testing.T) {
	first, firstProgram := firstRelease(t, orderOptions{})
	baseCandidate := buildOK(t, first)

	// The threshold moves from 500 to 600 under a new requirement version.
	p2, s2 := compileDocument(t, orderDocument(orderOptions{threshold: "600"}), compiler.PlatformCatalog())
	requirements, acceptance := artifacts(t, "600", 2)
	second := BuildInput{TenantID: tenantA, AppID: "app.orders", RequesterID: requester, Source: s2, SourceRevision: 2, Target: p2, Requirements: requirements, Acceptance: acceptance,
		Sandbox: evidenceFor(t, p2, 2), Tools: tools.EmptyCatalog(), Policy: policyInput(), Base: &BaseRelease{ArtifactHash: baseCandidate.Hash(), Generation: 1, Program: firstProgram}}
	c := buildOK(t, second)
	b, _ := c.V2()
	if b.BaseRelease != baseCandidate.Hash() || b.BaseGeneration != 1 || b.Storage.Class != compiler.StorageEqual || b.Storage.FirstRelease {
		t.Fatalf("%+v", b.Storage)
	}
	if b.Risk.Level != "HIGH" || !b.HighRisk || !b.Risk.Facts.ApprovalGateChanged || b.Risk.Facts.FirstRelease {
		t.Fatalf("a changed money route into an approval gate must rate HIGH: %+v", b.Risk)
	}
	if b.Risk.Facts.Volume != risk.VolumeUnverified {
		t.Fatal("a later release must not claim a measured volume")
	}
	if err := c.VerifyAgainstBase(firstProgram, compiler.SupportedIRVersions()); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyAgainstBase(p2, compiler.SupportedIRVersions()); err == nil {
		t.Fatal("verified against the wrong base")
	}
	if err := c.VerifyAgainstBase(compiler.Program{}, compiler.SupportedIRVersions()); err == nil {
		t.Fatal("verified without the base")
	}
	// A diff forged to look smaller than the real change cannot pass the recompute.
	forged, err := compiler.ComputeDiff(compiler.Program{}, p2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = retamper(t, c, func(b *BundleV2) { b.Diff.JSON, b.Diff.Hash = forged.JSON(), forged.Hash() }); err == nil {
		t.Fatal("a diff of a different pair was accepted")
	}

	// Staleness is derived from the stored candidate.
	_, baseSchema, _ := compiler.Storage(firstProgram)
	_, currentComponents := RuntimeComponents()
	bound := BoundState{BaseRelease: b.BaseRelease, BaseGeneration: b.BaseGeneration, BaseSchemaHash: b.Dependencies.BaseSchemaHash, PolicyBundleHash: b.Dependencies.PolicyBundleHash, ToolPins: b.Dependencies.ToolPins,
		IRVersion: b.IR.Version, RiskPolicyVersion: b.Risk.PolicyVersion, RiskLevel: risk.High, RiskFacts: b.Risk.Facts, EvidenceHash: b.Sandbox.EvidenceHash, ComponentsHash: b.RuntimeContract.ComponentsHash}
	current := CurrentState{ActiveRelease: baseCandidate.Hash(), ActiveGeneration: 1, BaseSchemaHash: baseSchema, PolicyBundleHash: b.Dependencies.PolicyBundleHash,
		ToolStatus: func(ToolPin) ToolPinStatus { return ToolOK }, SupportedIR: compiler.SupportedIRVersions(), Volume: b.Risk.Facts.Volume, Evidence: EvidenceState{Exists: true, Frozen: true, Hash: b.Sandbox.EvidenceHash}, ComponentsHash: currentComponents}
	if got := Staleness(bound, current); len(got) != 0 {
		t.Fatalf("a fresh candidate is stale: %v", got)
	}
	current.ActiveGeneration = 2
	if got := Staleness(bound, current); !reflect.DeepEqual(got, []StaleReason{StaleBaseGeneration}) {
		t.Fatalf("%v", got)
	}
	current.ActiveGeneration = 1

	// A candidate recorded by a binary with another component set (here the
	// workflow DSL list before the tool-plan DSL) is self-consistent, so it
	// still restores, but it reads stale: it is rebuilt, never revived.
	older := b.RuntimeContract.Components
	older.WorkflowDSL = append([]string{}, older.WorkflowDSL[:len(older.WorkflowDSL)-1]...)
	if hashComponents(older) == b.RuntimeContract.ComponentsHash {
		t.Fatal("the older component set must hash differently")
	}
	bound.ComponentsHash = hashComponents(older)
	if got := Staleness(bound, current); !reflect.DeepEqual(got, []StaleReason{StaleRuntimeChanged}) {
		t.Fatalf("an older component set: %v", got)
	}
}

// The M6R in-process Approve/ValidateActivation token is gone (ADR103 section
// 9); the header and the floor the approval port enforces are derived from the
// stored body alone.
func TestHeaderAndDatabaseFloorsOfABuiltCandidate(t *testing.T) {
	in, _ := firstRelease(t, orderOptions{})
	c := buildOK(t, in)
	b, ok := c.V2()
	h := c.Header()
	if !ok || h.Version != BundleVersion2 || h.TenantID != tenantA || h.AppID != "app.orders" || h.RequesterID != requester || !h.HighRisk || h.IRHash != b.IR.Hash ||
		h.Dependencies != (Dependencies{PolicyHash: b.Dependencies.PolicyBundleHash, ToolsHash: b.Dependencies.ToolsHash, SchemaHash: b.Dependencies.TargetSchemaHash}) {
		t.Fatalf("header %+v", h)
	}
	if b.Risk.Level != "HIGH" {
		t.Fatalf("fixture is %s, want HIGH", b.Risk.Level)
	}
	parties := risk.Parties{Requester: b.RequesterID, AcceptanceAuthor: b.Acceptance.AuthorID, SandboxFreezer: b.Sandbox.FrozenBy}
	approver := "77777777-7777-4777-8777-777777777777"
	if role, admitted := FloorExclusion(b.Risk.Level, parties, requester); admitted || role != risk.RoleRequester {
		t.Fatal("the floor admits the requester of a HIGH risk candidate", role)
	}
	if role, admitted := FloorExclusion(b.Risk.Level, parties, b.Acceptance.AuthorID); admitted || role != risk.RoleAcceptanceAuthor {
		t.Fatal("the floor admits the acceptance author of a HIGH risk candidate", role)
	}
	if _, admitted := FloorExclusion(b.Risk.Level, parties, approver); !admitted {
		t.Fatal("the floor refuses an independent approver")
	}
	if risk.CheckSeparation(b.Risk.Separation, parties, []string{requester}) == nil || risk.CheckSeparation(b.Risk.Separation, parties, []string{approver}) != nil {
		t.Fatal("the bundle's own separation disagrees with the floor")
	}
	if RequiredApprovals(b.Risk.Level) > b.Risk.Separation.Approvals || CountEligible(b.Risk.Level, parties, []string{requester, b.Acceptance.AuthorID, approver}) != 1 {
		t.Fatal("quorum floor and eligible count disagree with the bundle")
	}
}

func TestToolPinsAndBlockedByUnresolvedTool(t *testing.T) {
	read := tools.Definition{Ref: "tool.vendor_lookup", Version: "1.0.0", SideEffect: "READ", Approval: "NONE",
		Delivery:  tools.DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: tools.Connector{Endpoint: tools.Endpoint{Host: "vendors.example.com", Port: 443, Network: "public"}, Method: "GET", Path: "/vendors/{vendor_id}", TimeoutMS: 1000, MaxResponseBytes: 4096, RatePerSecond: 10},
		Input:     []tools.Field{{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "path"}},
		Output:    []tools.Field{{Name: "risk_score", Type: "integer", Required: true}},
		Sandbox:   tools.SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{"risk_score": json.RawMessage(`7`)}}, Tenants: []string{"*"}}
	catalog, err := tools.NewCatalog([]tools.Definition{read})
	if err != nil {
		t.Fatal(err)
	}
	version, ns := metadata.ToolWorkflowSchemaVersion, "example.vendors"
	def := func(id, kind string) metadata.Definition {
		return metadata.Definition{ID: id, SchemaVersion: version, Namespace: ns, Label: id, Kind: kind}
	}
	entity := def("entity.vendor", "entity")
	entity.Entity = &metadata.Entity{Fields: []string{"field.vendor.id"}}
	id := def("field.vendor.id", "field")
	id.Field = &metadata.Field{EntityRef: "entity.vendor", Type: "string", Required: true}
	wf := def("workflow.vendor.sync", "workflow")
	wf.Workflow = &metadata.Workflow{DSLVersion: "2.0.0", Version: "1.0.0", TimeoutMS: 60000, EntityRef: "entity.vendor", Trigger: "manual", InitialState: "state.start", States: []metadata.WorkflowState{
		{ID: "state.call", Type: "activity", Activity: &metadata.Activity{ToolRef: "tool.vendor_lookup", Version: "1.0.0", Inputs: map[string]string{"vendor_id": "field.vendor.id"}, Retry: metadata.Retry{MaxAttempts: 2, InitialBackoffMS: 100, MaxBackoffMS: 1000}}, Transitions: []metadata.Transition{{To: "state.complete"}}},
		{ID: "state.complete", Type: "complete", Transitions: []metadata.Transition{}},
		{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.call"}}},
	}}
	cc := compiler.PlatformCatalog()
	cc.ToolCatalog = catalog
	p, s := compileDocument(t, metadata.Document{SchemaVersion: version, AppID: "app.vendors", Namespace: ns, Definitions: []metadata.Definition{entity, id, wf}}, cc)
	pins := ToolPins(p.IR())
	if len(pins) != 1 || pins[0].Ref != "tool.vendor_lookup" || pins[0].Hash != read.Hash() || ToolsHash(pins) == ToolsHash(nil) {
		t.Fatalf("%+v", pins)
	}
	// Requirements for the tool, and acceptance by someone else.
	effect, _ := json.Marshal(intent.ToolEffect{ToolRef: "tool.vendor_lookup", MaxSideEffect: "READ", RequiresApproval: false, MaxSandboxMode: "SIMULATE"})
	set := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.vendors", Requirements: []intent.RequirementV2{
		{ID: "tool-bound", Category: intent.CategoryToolEffect, Requiredness: intent.Mandatory, RequirednessBasis: intent.BasisPolicyFloor, Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: effect}}}
	setJSON, _ := set.Canonical()
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{{ID: "tool-holds", RequirementID: "tool-bound", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}}}}
	specJSON, _ := spec.Canonical()
	_, schema, _ := compiler.Storage(p)
	body := fmt.Sprintf(`{"app_id": "app.vendors", "data_mode": "SYNTHETIC_SEPARATE", "dataset_id": "%s", "ir_hash": "%s", "schema_hash": "%s", "metadata_revision": 1, "schema_version": "sandbox-evidence-v1"}`, datasetID, p.Hash(), schema)
	in := BuildInput{TenantID: tenantA, AppID: "app.vendors", RequesterID: requester, Source: s, SourceRevision: 1, Target: p,
		Requirements: StoredRequirements{AppID: "app.vendors", Version: 1, Hash: set.Hash(), DecidedBy: requester, Body: setJSON},
		Acceptance:   StoredAcceptance{ID: specID, AppID: "app.vendors", RequirementsHash: set.Hash(), Hash: spec.Hash(), AuthorID: acceptor, Body: specJSON},
		Sandbox:      FrozenSandbox{DatasetID: datasetID, EvidenceHash: sha([]byte(body)), FrozenBy: freezer, Evidence: body}, Tools: catalog, Policy: policyInput()}
	c := buildOK(t, in)
	b, _ := c.V2()
	if len(b.Dependencies.ToolPins) != 1 || len(b.Dependencies.ToolDefinitions) != 1 || b.Dependencies.ToolsHash != ToolsHash(pins) {
		t.Fatalf("%+v", b.Dependencies)
	}
	// A read tool added in a first release rates MEDIUM (S9 read, S13 first release, S2 entity).
	if b.Risk.Level != "MEDIUM" || b.HighRisk {
		t.Fatalf("%s %v", b.Risk.Level, b.Risk.Reasons)
	}
	// The same release against a catalog that no longer has the pin cannot be built.
	in.Tools = tools.EmptyCatalog()
	_, err = BuildCandidate(in)
	var r *Rejection
	if !errors.As(err, &r) || r.Reason != RejectBlockedByRisk || r.Assessment == nil || !r.Assessment.Blocked {
		t.Fatalf("an unresolved pin must block: %v", err)
	}
	// And a tool definition smuggled into a stored bundle cannot be edited.
	if _, err = retamper(t, c, func(b *BundleV2) { b.Dependencies.ToolDefinitions[0].Connector.RatePerSecond = 99 }); err == nil {
		t.Fatal("an edited tool definition was accepted")
	}
	if _, err = retamper(t, c, func(b *BundleV2) { b.Dependencies.ToolDefinitions = nil }); err == nil {
		t.Fatal("a pin without its definition was accepted")
	}
}
