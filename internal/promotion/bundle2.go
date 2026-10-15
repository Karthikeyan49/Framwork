package promotion

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

// BundleVersion2 is the server-built candidate (ADR038). It supersedes 1.0.0 for
// staging; 1.0.0 bytes still restore. No field is a caller claim: every
// authority-bearing value is derived from trusted artifacts and re-derived by
// Restore, or is bound to a database row that the stage port checks.
//
// JSON keys are the Go field names (no tags) because migration 11's
// approval_insert policy and migration 48's stage_candidate port read HighRisk,
// RequesterID, Risk.Level and friends by those names.
const BundleVersion2 = "2.0.0"

const MaxBundleBytes = 8 << 20

type SourceV2 struct {
	Hash          string
	Revision      int64
	SchemaVersion string
	JSON          json.RawMessage
}
type IRV2 struct {
	Hash            string
	Version         string
	CompilerVersion string
	JSON            json.RawMessage
}
type RuntimeContractV2 struct {
	Version        string
	ComponentsHash string
	Components     RuntimeComponentsDoc
}
type DependenciesV2 struct {
	PolicyBundleHash string
	PolicyBundle     PolicyBundleInput
	ToolsHash        string
	ToolPins         []ToolPin
	ToolDefinitions  []tools.Definition
	SchemaLayout     string
	BaseSchemaHash   string
	TargetSchemaHash string
}
type EvolutionV2 struct {
	Hash string
	JSON json.RawMessage
}
type DiffV2 struct {
	Version string
	Hash    string
	JSON    json.RawMessage
}
type RiskV2 struct {
	PolicyVersion string
	PolicyHash    string
	Level         string
	Reasons       []string
	FactsHash     string
	Facts         risk.Facts
	Separation    risk.Separation
	Rollback      string
}
type RequirementsV2 struct {
	Version       int64
	Hash          string
	PolicyVersion string
	JSON          json.RawMessage
}
type AcceptanceV2 struct {
	ID               string
	Hash             string
	AuthorID         string
	RequirementsHash string
	JSON             json.RawMessage
}
type SandboxV2 struct {
	DatasetID    string
	EvidenceHash string
	FrozenBy     string
	Evidence     string
}
type VerificationV2 struct {
	VerifierVersion   string
	VocabularyVersion string
	Outcome           string
	ReportHash        string
	Report            json.RawMessage
}

type BundleV2 struct {
	Version         string
	TenantID        string
	AppID           string
	HighRisk        bool
	RequesterID     string
	BaseRelease     string
	BaseGeneration  int64
	Source          SourceV2
	IR              IRV2
	RuntimeContract RuntimeContractV2
	Dependencies    DependenciesV2
	Storage         compiler.StorageDelta
	Evolution       *EvolutionV2 `json:",omitempty"`
	Diff            DiffV2
	Risk            RiskV2
	Requirements    RequirementsV2
	Acceptance      AcceptanceV2
	Sandbox         SandboxV2
	Verification    VerificationV2
}

// Header is the version-neutral identity of a candidate, for approval,
// activation, rollback and audit code that must not care which bundle version it
// holds. Dependencies carries the three hashes M6R activation compares.
type Header struct {
	Version      string
	TenantID     string
	AppID        string
	IRHash       string
	RequesterID  string
	BaseRelease  string
	HighRisk     bool
	Dependencies Dependencies
}

// Header decodes the version-neutral identity; the zero Header means unreadable.
func (c Candidate) Header() Header {
	var probe struct{ Version string }
	if json.Unmarshal(c.raw, &probe) != nil {
		return Header{}
	}
	switch probe.Version {
	case "1.0.0":
		b := c.Bundle()
		return Header{Version: b.Version, AppID: bApplication(b), IRHash: b.IRHash, RequesterID: b.RequesterID, BaseRelease: b.BaseRelease, HighRisk: b.HighRisk, Dependencies: b.Dependencies}
	case BundleVersion2:
		b, ok := c.V2()
		if !ok {
			return Header{}
		}
		return Header{Version: b.Version, TenantID: b.TenantID, AppID: b.AppID, IRHash: b.IR.Hash, RequesterID: b.RequesterID, BaseRelease: b.BaseRelease, HighRisk: b.HighRisk,
			Dependencies: Dependencies{PolicyHash: b.Dependencies.PolicyBundleHash, ToolsHash: b.Dependencies.ToolsHash, SchemaHash: b.Dependencies.TargetSchemaHash}}
	}
	return Header{}
}

// V2 decodes a 2.0.0 candidate. It is only meaningful for a Candidate produced by
// BuildCandidate or Restore.
func (c Candidate) V2() (BundleV2, bool) {
	var b BundleV2
	if json.Unmarshal(c.raw, &b) != nil || b.Version != BundleVersion2 {
		return BundleV2{}, false
	}
	return b, true
}

// Program restores the candidate's executable program from its own bytes.
func (c Candidate) Program(supported []string) (compiler.Program, error) {
	switch c.Header().Version {
	case "1.0.0":
		b := c.Bundle()
		return compiler.Restore(b.IR, b.IRHash, supported)
	case BundleVersion2:
		b, ok := c.V2()
		if !ok {
			return compiler.Program{}, ErrGate
		}
		return compiler.Restore(b.IR.JSON, b.IR.Hash, supported)
	}
	return compiler.Program{}, ErrGate
}

// -------------------------------------------------------------------- building

// BaseRelease is the active release a candidate evolves from.
type BaseRelease struct {
	ArtifactHash string
	Generation   int64
	Program      compiler.Program
}

// StoredRequirements, StoredAcceptance and FrozenSandbox are rows read from the
// database in the builder's transaction; the stage port binds the bundle to them.
type StoredRequirements struct {
	AppID     string          `json:"app_id"`
	Version   int64           `json:"version"`
	Hash      string          `json:"hash"`
	DecidedBy string          `json:"decided_by"`
	DecidedAt string          `json:"decided_at"`
	Body      json.RawMessage `json:"requirements"`
}
type StoredAcceptance struct {
	ID               string          `json:"id"`
	AppID            string          `json:"app_id"`
	RequirementsHash string          `json:"requirements_hash"`
	Hash             string          `json:"hash"`
	AuthorID         string          `json:"author_id"`
	CreatedAt        string          `json:"created_at"`
	Body             json.RawMessage `json:"acceptance"`
}
type FrozenSandbox struct {
	DatasetID    string
	EvidenceHash string
	FrozenBy     string
	Evidence     string
}

// BuildInput is the trusted input of BuildCandidate. The caller (the composition
// root) reads every field from the database, the registry, the trusted compiler
// and the operator catalog inside one repeatable-read transaction. Nothing here
// comes from a request except which stored artifacts to use.
type BuildInput struct {
	TenantID, AppID, RequesterID string
	Source                       metadata.Source
	SourceRevision               int64
	Target                       compiler.Program
	Base                         *BaseRelease
	Intent                       compiler.EvolutionIntent
	Requirements                 StoredRequirements
	Acceptance                   StoredAcceptance
	Sandbox                      FrozenSandbox
	Tools                        tools.Catalog
	Policy                       PolicyBundleInput
	Volume                       string
}

// Rejection explains a candidate that must not be staged. Report and Assessment
// are the server-computed evidence for the caller to return and audit.
type Rejection struct {
	Reason     string
	Report     *verification.Report
	Assessment *risk.Assessment
}

func (r *Rejection) Error() string { return "release candidate rejected: " + r.Reason }

// Rejection reasons.
const (
	RejectBlockedByRisk       = "BLOCKED_BY_RISK"
	RejectVerificationFailed  = "VERIFICATION_FAILED"
	RejectVerificationBlocked = "VERIFICATION_BLOCKED"
	RejectStorageBreaking     = "STORAGE_BREAKING"
	RejectInvalidIntent       = "INVALID_EVOLUTION_INTENT"
)

func gateError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrGate, fmt.Sprintf(format, args...))
}

func sha(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// DefaultVolume is the volume bucket for a release whose production volume
// cannot be measured yet: a first release has no data; any later release is
// UNVERIFIED until M13 provides a production volume source.
func DefaultVolume(first bool) string {
	if first {
		return risk.VolumeNone
	}
	return risk.VolumeUnverified
}

func definitionsOf(catalog tools.Catalog, pins []ToolPin, d compiler.Diff) (map[string]tools.Definition, []tools.Definition) {
	all := map[string]tools.Definition{}
	want := map[string]ToolPin{}
	for _, p := range pins {
		want[DefinitionKey(p.Ref, p.Version, p.Hash)] = p
	}
	for _, a := range d.ToolActivities {
		if a.BaseHash != "" {
			want[DefinitionKey(a.Ref, a.Version, a.BaseHash)] = ToolPin{a.Ref, a.Version, a.BaseHash}
		}
		if a.TargetHash != "" {
			want[DefinitionKey(a.Ref, a.Version, a.TargetHash)] = ToolPin{a.Ref, a.Version, a.TargetHash}
		}
	}
	for key, p := range want {
		if def, ok := catalog.Lookup(p.Ref, p.Version, p.Hash); ok {
			all[key] = def
		}
	}
	list := make([]tools.Definition, 0, len(all))
	for _, def := range all {
		list = append(list, def)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Hash() < b.Hash()
	})
	return all, list
}

func toRisk(a risk.Assessment, facts risk.Facts) RiskV2 {
	return RiskV2{PolicyVersion: a.PolicyVersion, PolicyHash: a.PolicyHash, Level: a.Level.String(), Reasons: append([]string{}, a.Reasons...), FactsHash: a.FactsHash, Facts: facts, Separation: a.Separation, Rollback: a.Rollback}
}

// BuildCandidate assembles, evaluates and verifies a release candidate.
// A non-nil *Rejection means the evidence says the candidate must not be staged;
// any other error is an invalid input. It never writes anything.
func BuildCandidate(in BuildInput) (Candidate, error) {
	supported := compiler.SupportedIRVersions()
	if !tenant.ValidID(in.TenantID) || !tenant.ValidID(in.RequesterID) || in.SourceRevision < 1 || !in.Target.Valid() || !in.Target.MatchesSource(in.Source) || len(in.Source.JSON()) == 0 {
		return Candidate{}, gateError("identities, source or program are invalid")
	}
	ir := in.Target.IR()
	if ir.Application.AppID != in.AppID || in.Source.AppID() != in.AppID {
		return Candidate{}, gateError("application does not match the compiled program")
	}
	base := compiler.Program{}
	baseRelease, baseGeneration := "", int64(0)
	if in.Base != nil {
		if !ValidHash(in.Base.ArtifactHash) || in.Base.Generation < 1 || !in.Base.Program.Valid() {
			return Candidate{}, gateError("base release is invalid")
		}
		base, baseRelease, baseGeneration = in.Base.Program, in.Base.ArtifactHash, in.Base.Generation
	}
	if in.Volume == "" {
		in.Volume = DefaultVolume(in.Base == nil)
	}

	set, setJSON, setHash, err := intent.ParseRequirementSet(in.Requirements.Body)
	if err != nil || !bytes.Equal(setJSON, in.Requirements.Body) || setHash != in.Requirements.Hash || set.AppID != in.AppID || in.Requirements.Version < 1 {
		return Candidate{}, gateError("stored requirement set is not canonical or does not match its row")
	}
	spec, specJSON, specHash, err := intent.ParseAcceptanceSpec(in.Acceptance.Body)
	if err != nil || !bytes.Equal(specJSON, in.Acceptance.Body) || specHash != in.Acceptance.Hash || spec.RequirementsHash != setHash || in.Acceptance.RequirementsHash != setHash || !tenant.ValidID(in.Acceptance.ID) || !tenant.ValidID(in.Acceptance.AuthorID) {
		return Candidate{}, gateError("stored acceptance spec is not canonical or does not match its row")
	}
	if err = intent.ValidateAcceptance(spec, set); err != nil {
		return Candidate{}, gateError("acceptance spec rejected: %v", err)
	}
	if in.Acceptance.AuthorID == in.RequesterID {
		return Candidate{}, gateError("acceptance must be authored by someone other than the requester")
	}
	_, targetSchema, err := compiler.Storage(in.Target)
	if err != nil {
		return Candidate{}, gateError("target has no storage layout")
	}
	if err = checkEvidence(in.Sandbox, in.AppID, in.Target.Hash(), in.SourceRevision, targetSchema); err != nil {
		return Candidate{}, err
	}

	delta := compiler.ClassifyStorage(base, in.Target, in.Intent)
	if delta.Class == compiler.StorageBreaking {
		return Candidate{}, &Rejection{Reason: RejectStorageBreaking}
	}
	if delta.Class == compiler.StorageEqual && (len(in.Intent.Renames) > 0 || len(in.Intent.Backfills) > 0) {
		return Candidate{}, &Rejection{Reason: RejectInvalidIntent}
	}
	var evolution *EvolutionV2
	if delta.EvolutionPlanHash != "" {
		plan, perr := compiler.PlanEvolution(base, in.Target, in.Intent)
		if perr != nil || plan.Hash() != delta.EvolutionPlanHash {
			return Candidate{}, gateError("evolution plan cannot be derived")
		}
		evolution = &EvolutionV2{Hash: plan.Hash(), JSON: plan.JSON()}
	}
	diff, err := compiler.ComputeDiff(base, in.Target)
	if err != nil {
		return Candidate{}, gateError("diff cannot be computed")
	}

	pins := ToolPins(ir)
	defs, defList := definitionsOf(in.Tools, pins, diff)
	facts := ReduceFacts(FactsInput{Diff: diff, Storage: delta, TargetPins: pins, Definitions: defs, TenantID: in.TenantID, Volume: in.Volume, AcceptanceAuthorIsRequester: in.Acceptance.AuthorID == in.RequesterID})
	assessment, err := risk.Evaluate(facts)
	if err != nil {
		return Candidate{}, gateError("risk facts are invalid: %v", err)
	}
	if assessment.Blocked {
		return Candidate{}, &Rejection{Reason: RejectBlockedByRisk, Assessment: &assessment}
	}
	catalog := tools.EmptyCatalog()
	if len(defList) > 0 {
		if catalog, err = tools.NewCatalog(defList); err != nil {
			return Candidate{}, gateError("tool definitions are invalid")
		}
	}
	report, err := verification.Verify(verification.Input{Program: in.Target, Requirements: set, Acceptance: spec, StorageClass: delta.Class, Changed: ChangedFor(diff, delta),
		Tools: catalog, TenantID: in.TenantID, SandboxEvidenceHash: in.Sandbox.EvidenceHash})
	if err != nil {
		return Candidate{}, gateError("verification input is invalid: %v", err)
	}
	if report.Outcome != verification.Pass {
		reason := RejectVerificationFailed
		if report.Outcome == verification.Blocked {
			reason = RejectVerificationBlocked
		}
		return Candidate{}, &Rejection{Reason: reason, Report: &report, Assessment: &assessment}
	}

	policyHash, err := PolicyBundleHash(in.Policy)
	if err != nil {
		return Candidate{}, gateError("policy bundle is invalid")
	}
	components, componentsHash := RuntimeComponents()
	baseSchema := ""
	if in.Base != nil {
		_, baseSchema, err = compiler.Storage(in.Base.Program)
		if err != nil {
			return Candidate{}, gateError("base has no storage layout")
		}
	}
	b := BundleV2{
		Version: BundleVersion2, TenantID: in.TenantID, AppID: in.AppID, HighRisk: assessment.HighRisk(), RequesterID: in.RequesterID, BaseRelease: baseRelease, BaseGeneration: baseGeneration,
		Source:          SourceV2{Hash: in.Source.Hash(), Revision: in.SourceRevision, SchemaVersion: ir.Application.SchemaVersion, JSON: in.Source.JSON()},
		IR:              IRV2{Hash: in.Target.Hash(), Version: ir.IRVersion, CompilerVersion: ir.CompilerVersion, JSON: in.Target.JSON()},
		RuntimeContract: RuntimeContractV2{Version: RuntimeContractVersion, ComponentsHash: componentsHash, Components: components},
		Dependencies: DependenciesV2{PolicyBundleHash: policyHash, PolicyBundle: in.Policy, ToolsHash: ToolsHash(pins), ToolPins: pins, ToolDefinitions: defList,
			SchemaLayout: ir.SchemaSnapshot, BaseSchemaHash: baseSchema, TargetSchemaHash: targetSchema},
		Storage: delta, Evolution: evolution,
		Diff:         DiffV2{Version: compiler.DiffVersion, Hash: diff.Hash(), JSON: diff.JSON()},
		Risk:         toRisk(assessment, facts),
		Requirements: RequirementsV2{Version: in.Requirements.Version, Hash: setHash, PolicyVersion: intent.RequirementPolicyVersion, JSON: setJSON},
		Acceptance:   AcceptanceV2{ID: in.Acceptance.ID, Hash: specHash, AuthorID: in.Acceptance.AuthorID, RequirementsHash: setHash, JSON: specJSON},
		Sandbox:      SandboxV2{DatasetID: in.Sandbox.DatasetID, EvidenceHash: in.Sandbox.EvidenceHash, FrozenBy: in.Sandbox.FrozenBy, Evidence: in.Sandbox.Evidence},
		Verification: VerificationV2{VerifierVersion: verification.VerifierVersion, VocabularyVersion: intent.VocabularyVersion, Outcome: report.Outcome, ReportHash: report.Hash(), Report: report.JSON()},
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > MaxBundleBytes {
		return Candidate{}, gateError("candidate exceeds its size budget")
	}
	c := Candidate{raw, Hash(raw)}
	// Self-check: what was just built must restore and recompute identically.
	if _, err = Restore(raw, c.hash, supported); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func checkEvidence(s FrozenSandbox, app, irHash string, revision int64, schemaHash string) error {
	if !tenant.ValidID(s.DatasetID) || !tenant.ValidID(s.FrozenBy) || !ValidHash(s.EvidenceHash) || sha([]byte(s.Evidence)) != s.EvidenceHash || len(s.Evidence) > 16384 {
		return gateError("sandbox evidence is invalid")
	}
	var e struct {
		SchemaVersion    string `json:"schema_version"`
		AppID            string `json:"app_id"`
		DatasetID        string `json:"dataset_id"`
		DataMode         string `json:"data_mode"`
		IRHash           string `json:"ir_hash"`
		SchemaHash       string `json:"schema_hash"`
		MetadataRevision int64  `json:"metadata_revision"`
	}
	if json.Unmarshal([]byte(s.Evidence), &e) != nil || e.SchemaVersion != "sandbox-evidence-v1" || e.AppID != app || e.DatasetID != s.DatasetID || e.DataMode != "SYNTHETIC_SEPARATE" ||
		e.IRHash != irHash || e.SchemaHash != schemaHash || e.MetadataRevision != revision {
		return gateError("sandbox evidence is not for this exact compiled release")
	}
	return nil
}

// --------------------------------------------------------------------- restore

// restoreV2 re-derives everything a 2.0.0 candidate claims that does not need
// the base release's bytes, and requires byte equality. The diff is the one
// artifact it can only check structurally; VerifyAgainstBase recomputes it.
func restoreV2(raw []byte, hash string, supported []string) (Candidate, error) {
	var b BundleV2
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || d.More() || b.Version != BundleVersion2 {
		return Candidate{}, gateError("bundle 2.0.0 does not match its schema")
	}
	if canonical, err := json.Marshal(b); err != nil || !bytes.Equal(canonical, raw) {
		return Candidate{}, gateError("bundle 2.0.0 is not canonical")
	}
	if !tenant.ValidID(b.TenantID) || !tenant.ValidID(b.RequesterID) || !tenant.ValidID(b.Acceptance.AuthorID) || !tenant.ValidID(b.Acceptance.ID) || !tenant.ValidID(b.Sandbox.DatasetID) || !tenant.ValidID(b.Sandbox.FrozenBy) ||
		b.Acceptance.AuthorID == b.RequesterID {
		return Candidate{}, gateError("identities are invalid or not separated")
	}
	if (b.BaseRelease == "") != (b.BaseGeneration == 0) || b.BaseRelease != "" && (!ValidHash(b.BaseRelease) || b.BaseGeneration < 1) {
		return Candidate{}, gateError("base binding is invalid")
	}
	program, err := compiler.Restore(b.IR.JSON, b.IR.Hash, supported)
	ir := program.IR()
	if err != nil || b.IR.Version != ir.IRVersion || b.IR.CompilerVersion != ir.CompilerVersion || b.AppID != ir.Application.AppID {
		return Candidate{}, gateError("stored IR is invalid or does not match its identity")
	}
	v, err := metadata.NewValidator()
	if err != nil {
		return Candidate{}, gateError("metadata validator unavailable")
	}
	source, err := v.Parse(b.Source.JSON)
	if err != nil || source.Hash() != b.Source.Hash || source.AppID() != b.AppID || b.Source.Revision < 1 || b.Source.SchemaVersion != ir.Application.SchemaVersion || !compiler.MatchesStoredSource(program, source) {
		return Candidate{}, gateError("stored source does not match the IR")
	}
	if b.RuntimeContract.Version != RuntimeContractVersion || b.RuntimeContract.ComponentsHash != hashComponents(b.RuntimeContract.Components) {
		return Candidate{}, gateError("runtime contract is inconsistent")
	}

	// Dependencies.
	pins := ToolPins(ir)
	if !reflect.DeepEqual(pins, b.Dependencies.ToolPins) || b.Dependencies.ToolsHash != ToolsHash(pins) {
		return Candidate{}, gateError("tool pins are not derived from the IR")
	}
	policyHash, err := PolicyBundleHash(b.Dependencies.PolicyBundle)
	if err != nil || policyHash != b.Dependencies.PolicyBundleHash {
		return Candidate{}, gateError("policy bundle hash is inconsistent")
	}
	targetSchemaStruct, targetSchema, err := compiler.Storage(program)
	if err != nil || b.Dependencies.TargetSchemaHash != targetSchema || b.Dependencies.SchemaLayout != targetSchemaStruct.Layout || ir.SchemaSnapshot != targetSchemaStruct.Layout {
		return Candidate{}, gateError("target schema hash is inconsistent")
	}
	if (b.BaseRelease == "") != (b.Dependencies.BaseSchemaHash == "") || b.Dependencies.BaseSchemaHash != "" && !ValidHash(b.Dependencies.BaseSchemaHash) {
		return Candidate{}, gateError("base schema binding is inconsistent")
	}

	// Diff and storage.
	diff, err := compiler.ParseDiff(b.Diff.JSON, b.Diff.Hash)
	if err != nil || b.Diff.Version != compiler.DiffVersion || diff.TargetIRHash != b.IR.Hash || diff.FirstRelease != (b.BaseRelease == "") || (diff.BaseIRHash == "") != (b.BaseRelease == "") {
		return Candidate{}, gateError("diff is not bound to this candidate")
	}
	st := b.Storage
	if st.Class == compiler.StorageBreaking || st.TargetSchemaHash != targetSchema || st.BaseSchemaHash != b.Dependencies.BaseSchemaHash || st.FirstRelease != (b.BaseRelease == "") {
		return Candidate{}, gateError("storage classification is inconsistent")
	}
	if st.FirstRelease && st.Class != compiler.StorageAdditive || !st.FirstRelease && (st.Class == compiler.StorageEqual) != (st.BaseSchemaHash == targetSchema) {
		return Candidate{}, gateError("storage class contradicts the schema hashes")
	}
	switch {
	case st.EvolutionPlanHash == "" && b.Evolution != nil, st.EvolutionPlanHash != "" && b.Evolution == nil:
		return Candidate{}, gateError("evolution plan presence is inconsistent")
	case b.Evolution != nil:
		var plan compiler.EvolutionArtifact
		dec := json.NewDecoder(bytes.NewReader(b.Evolution.JSON))
		dec.DisallowUnknownFields()
		if dec.Decode(&plan) != nil || sha(b.Evolution.JSON) != b.Evolution.Hash || b.Evolution.Hash != st.EvolutionPlanHash || plan.TargetIRHash != b.IR.Hash || plan.SchemaHash != targetSchema || plan.BaseSchemaHash != b.Dependencies.BaseSchemaHash {
			return Candidate{}, gateError("evolution plan is not bound to this candidate")
		}
	}

	// Tool definitions: every stored definition is referenced, every target pin
	// resolves and is allowed for the tenant.
	defs := map[string]tools.Definition{}
	referenced := map[string]bool{}
	for _, p := range pins {
		referenced[DefinitionKey(p.Ref, p.Version, p.Hash)] = true
	}
	for _, a := range diff.ToolActivities {
		if a.BaseHash != "" {
			referenced[DefinitionKey(a.Ref, a.Version, a.BaseHash)] = true
		}
		if a.TargetHash != "" {
			referenced[DefinitionKey(a.Ref, a.Version, a.TargetHash)] = true
		}
	}
	for _, def := range b.Dependencies.ToolDefinitions {
		key := DefinitionKey(def.Ref, def.Version, def.Hash())
		if !referenced[key] {
			return Candidate{}, gateError("tool definition %s is not pinned", def.Ref)
		}
		defs[key] = def
	}
	if len(defs) != len(b.Dependencies.ToolDefinitions) {
		return Candidate{}, gateError("duplicate tool definitions")
	}
	for _, p := range pins {
		def, ok := defs[DefinitionKey(p.Ref, p.Version, p.Hash)]
		if !ok || !def.AllowsTenant(b.TenantID) {
			return Candidate{}, gateError("tool pin %s@%s does not resolve for the tenant", p.Ref, p.Version)
		}
	}
	catalog := tools.EmptyCatalog()
	if len(defs) > 0 {
		if catalog, err = tools.NewCatalog(b.Dependencies.ToolDefinitions); err != nil {
			return Candidate{}, gateError("tool definitions are invalid")
		}
	}

	// Requirements and independent acceptance.
	set, setJSON, setHash, err := intent.ParseRequirementSet(b.Requirements.JSON)
	if err != nil || !bytes.Equal(setJSON, b.Requirements.JSON) || setHash != b.Requirements.Hash || set.AppID != b.AppID || b.Requirements.Version < 1 || b.Requirements.PolicyVersion != intent.RequirementPolicyVersion {
		return Candidate{}, gateError("requirement set is not canonical or does not match its hash")
	}
	if err = intent.CheckFloors(set, st.Class); err != nil {
		return Candidate{}, gateError("requirement floors: %v", err)
	}
	spec, specJSON, specHash, err := intent.ParseAcceptanceSpec(b.Acceptance.JSON)
	if err != nil || !bytes.Equal(specJSON, b.Acceptance.JSON) || specHash != b.Acceptance.Hash || spec.RequirementsHash != setHash || b.Acceptance.RequirementsHash != setHash {
		return Candidate{}, gateError("acceptance spec is not canonical or not bound to the requirement set")
	}
	if err = intent.ValidateAcceptance(spec, set); err != nil {
		return Candidate{}, gateError("acceptance spec: %v", err)
	}
	if err = checkEvidence(FrozenSandbox{b.Sandbox.DatasetID, b.Sandbox.EvidenceHash, b.Sandbox.FrozenBy, b.Sandbox.Evidence}, b.AppID, b.IR.Hash, b.Source.Revision, targetSchema); err != nil {
		return Candidate{}, err
	}

	// Risk is recomputed from the stored facts, and the facts from the stored diff.
	// Volume is the one fact a later binary cannot re-derive (it is measured), so
	// it is carried and its staleness is judged separately.
	want := ReduceFacts(FactsInput{Diff: diff, Storage: st, TargetPins: pins, Definitions: defs, TenantID: b.TenantID, Volume: b.Risk.Facts.Volume, AcceptanceAuthorIsRequester: false})
	gotFacts, err := risk.Normalize(b.Risk.Facts)
	wantFacts, werr := risk.Normalize(want)
	if err != nil || werr != nil || risk.CheckCanonical(b.Risk.Facts) != nil || !reflect.DeepEqual(gotFacts, wantFacts) {
		return Candidate{}, gateError("risk facts do not match the diff")
	}
	level, err := risk.ParseLevel(b.Risk.Level)
	if err != nil {
		return Candidate{}, gateError("risk level is invalid")
	}
	assessment := risk.Assessment{PolicyVersion: b.Risk.PolicyVersion, PolicyHash: b.Risk.PolicyHash, Level: level, Reasons: b.Risk.Reasons, FactsHash: b.Risk.FactsHash, Separation: b.Risk.Separation, Rollback: b.Risk.Rollback}
	if err = risk.Verify(assessment, b.Risk.Facts); err != nil || b.HighRisk != assessment.HighRisk() {
		return Candidate{}, gateError("risk assessment does not recompute")
	}

	// Verification is recomputed from the stored artifacts.
	if b.Verification.Outcome != verification.Pass || b.Verification.VerifierVersion != verification.VerifierVersion || b.Verification.VocabularyVersion != intent.VocabularyVersion || sha(b.Verification.Report) != b.Verification.ReportHash {
		return Candidate{}, gateError("verification claim is invalid")
	}
	input := verification.Input{Program: program, Requirements: set, Acceptance: spec, StorageClass: st.Class, Changed: ChangedFor(diff, st), Tools: catalog, TenantID: b.TenantID, SandboxEvidenceHash: b.Sandbox.EvidenceHash}
	if err = verification.Recompute(input, b.Verification.Report); err != nil {
		return Candidate{}, gateError("verification does not recompute: %v", err)
	}
	if Hash(raw) != hash {
		return Candidate{}, gateError("hash mismatch")
	}
	return Candidate{append([]byte(nil), raw...), hash}, nil
}

// VerifyAgainstBase additionally recomputes the diff and storage classification
// from the base release program, which only the caller holds. Approval and
// activation (M13) must call it with the program restored from BaseRelease.
func (c Candidate) VerifyAgainstBase(base compiler.Program, supported []string) error {
	b, ok := c.V2()
	if !ok {
		return ErrGate
	}
	target, err := compiler.Restore(b.IR.JSON, b.IR.Hash, supported)
	if err != nil {
		return ErrGate
	}
	if b.BaseRelease == "" {
		if base.Valid() {
			return gateError("a first release has no base")
		}
	} else if !base.Valid() {
		return gateError("the base program is required")
	}
	diff, err := compiler.ComputeDiff(base, target)
	if err != nil || !bytes.Equal(diff.JSON(), b.Diff.JSON) {
		return gateError("diff does not recompute from the base")
	}
	var intentOf compiler.EvolutionIntent
	if b.Evolution != nil {
		var plan compiler.EvolutionArtifact
		if json.Unmarshal(b.Evolution.JSON, &plan) != nil {
			return ErrGate
		}
		intentOf = plan.Intent
	}
	if !reflect.DeepEqual(compiler.ClassifyStorage(base, target, intentOf), b.Storage) {
		return gateError("storage classification does not recompute from the base")
	}
	if b.Evolution != nil {
		plan, perr := compiler.RestoreEvolution(base, target, b.Evolution.JSON, b.Evolution.Hash)
		if perr != nil || plan.Hash() != b.Evolution.Hash {
			return gateError("evolution plan does not recompute from the base")
		}
	}
	if base.Valid() {
		if _, schema, serr := compiler.Storage(base); serr != nil || schema != b.Dependencies.BaseSchemaHash {
			return gateError("base schema hash does not recompute")
		}
	}
	return nil
}
