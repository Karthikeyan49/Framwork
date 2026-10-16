// Package promotion owns release integrity contracts: hash-bound bundles and
// their restore, server-built candidates, staleness, and the store wrappers of
// the database ports that stage, approve, reject, activate and roll back them
// (ADR038, ADR039). Nothing serves an activated release yet: the production
// data plane is a later milestone (ADR039 section 10).
package promotion

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/sandbox"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var ErrGate = errors.New("release gate rejected")

type Dependencies struct{ PolicyHash, ToolsHash, SchemaHash string }
type Verification struct{ Version, SandboxID, IntentHash, AcceptanceHash, ReportHash, Outcome string }
type Bundle struct {
	Version                            string
	BaseRelease                        string
	Source                             json.RawMessage
	SourceHash                         string
	IR                                 json.RawMessage
	IRHash, CompilerVersion, IRVersion string
	Dependencies                       Dependencies
	Intent                             intent.Contract
	Acceptance                         intent.Acceptance
	Sandbox                            sandbox.Context
	Verification                       Verification
	RequesterID                        string
	HighRisk                           bool
}
type Candidate struct {
	raw  []byte
	hash string
}

func Hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func ValidHash(s string) bool {
	raw, e := hex.DecodeString(s)
	return e == nil && len(raw) == 32 && s == hex.EncodeToString(raw)
}
func (c Candidate) Hash() string   { return c.hash }
func (c Candidate) JSON() []byte   { return append([]byte(nil), c.raw...) }
func (c Candidate) Bundle() Bundle { var b Bundle; _ = json.Unmarshal(c.raw, &b); return b }

// Build is a trusted composition port, never an HTTP/LLM attestation endpoint.
// A future verifier must supply independently executed evidence, not an AI claim.
func Build(source metadata.Source, program compiler.Program, b Bundle) (Candidate, error) {
	if !program.Valid() || !program.MatchesSource(source) || len(source.JSON()) == 0 || b.Version != "1.0.0" || b.RequesterID == "" || b.Acceptance.Validate(b.Intent) != nil || b.Sandbox.Validate() != nil {
		return Candidate{}, ErrGate
	}
	if b.BaseRelease != "" && !ValidHash(b.BaseRelease) {
		return Candidate{}, ErrGate
	}
	for _, h := range []string{b.Dependencies.PolicyHash, b.Dependencies.ToolsHash, b.Dependencies.SchemaHash, b.Verification.ReportHash} {
		if !ValidHash(h) {
			return Candidate{}, ErrGate
		}
	}
	var doc metadata.Document
	if json.Unmarshal(source.JSON(), &doc) != nil {
		return Candidate{}, ErrGate
	}
	// Compilation normalizes the application. Verify source/IR identity without
	// recompiling by comparing the source's schema/app/version and definition IDs.
	ir := program.IR()
	if doc.AppID != ir.Application.AppID || doc.SchemaVersion != ir.Application.SchemaVersion || len(doc.Definitions) != len(ir.Application.Definitions) {
		return Candidate{}, ErrGate
	}
	// The candidate stores the normalized source actually embedded by the compiler.
	// Independent original authoring bytes are included as well for review/audit.
	b.Source = source.JSON()
	b.SourceHash = Hash(b.Source)
	b.IR = program.JSON()
	b.IRHash = program.Hash()
	b.CompilerVersion = ir.CompilerVersion
	b.IRVersion = ir.IRVersion
	if validateBundle(b, compiler.SupportedIRVersions()) != nil {
		return Candidate{}, ErrGate
	}
	raw, e := json.Marshal(b)
	if e != nil || len(raw) > 8<<20 {
		return Candidate{}, ErrGate
	}
	return Candidate{raw, Hash(raw)}, nil
}

func Restore(raw []byte, hash string, supportedIR []string) (Candidate, error) {
	if len(raw) > MaxBundleBytes || Hash(raw) != hash {
		return Candidate{}, ErrGate
	}
	var probe struct{ Version string }
	if json.Unmarshal(raw, &probe) != nil {
		return Candidate{}, ErrGate
	}
	if probe.Version == BundleVersion2 {
		return restoreV2(raw, hash, supportedIR)
	}
	var b Bundle
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || validateBundle(b, supportedIR) != nil {
		return Candidate{}, ErrGate
	}
	canonical, e := json.Marshal(b)
	if e != nil || !bytes.Equal(raw, canonical) {
		return Candidate{}, ErrGate
	}
	if _, e = compiler.Restore(b.IR, b.IRHash, supportedIR); e != nil {
		return Candidate{}, ErrGate
	}
	return Candidate{append([]byte(nil), raw...), hash}, nil
}

func validateBundle(b Bundle, supported []string) error {
	if b.Version != "1.0.0" || !tenant.ValidID(b.RequesterID) || !tenant.ValidID(b.Acceptance.AuthorActorID) || b.Intent.Validate() != nil || b.Acceptance.Validate(b.Intent) != nil || b.Sandbox.Validate() != nil || Hash(b.Source) != b.SourceHash {
		return ErrGate
	}
	for _, h := range []string{b.Dependencies.PolicyHash, b.Dependencies.ToolsHash, b.Dependencies.SchemaHash, b.Verification.ReportHash} {
		if !ValidHash(h) {
			return ErrGate
		}
	}
	if b.BaseRelease != "" && !ValidHash(b.BaseRelease) {
		return ErrGate
	}
	p, e := compiler.Restore(b.IR, b.IRHash, supported)
	if e != nil || b.IRVersion != p.IR().IRVersion || b.CompilerVersion != p.IR().CompilerVersion {
		return ErrGate
	}
	v, e := metadata.NewValidator()
	if e != nil {
		return ErrGate
	}
	s, e := v.Parse(b.Source)
	if e != nil {
		return ErrGate
	}
	var doc metadata.Document
	if json.Unmarshal(s.JSON(), &doc) != nil || !compiler.MatchesStoredSource(p, s) {
		return ErrGate
	}
	for _, r := range b.Intent.Requirements {
		if !r.Mandatory {
			continue
		}
		entity, ok := p.Entity(r.EntityID)
		if !ok {
			return ErrGate
		}
		found := false
		for _, f := range entity.Fields {
			if f.ID == r.FieldID && f.Type == "money" && f.Currency == r.Currency {
				found = true
			}
		}
		if !found {
			return ErrGate
		}
	}
	a, _ := json.Marshal(b.Acceptance)
	if b.Verification.Version != "1.0.0" || b.Verification.Outcome != "PASS" || b.Verification.SandboxID != b.Sandbox.ID || b.Verification.IntentHash != b.Intent.Hash() || b.Verification.AcceptanceHash != Hash(a) {
		return ErrGate
	}
	return nil
}

// Gate is the composition-root authorization of Store.Stage. Approval and
// promotion authority is enforced by the database ports, never by a Gate.
type Gate interface {
	AuthorizeRelease(tenantID, actorID, action, artifactHash string) bool
}

type ExecutionPin struct{ ReleaseHash, WorkflowID, WorkflowVersion string }

func (p ExecutionPin) Validate() error {
	if !ValidHash(p.ReleaseHash) || p.WorkflowID == "" || p.WorkflowVersion == "" {
		return ErrGate
	}
	return nil
}
