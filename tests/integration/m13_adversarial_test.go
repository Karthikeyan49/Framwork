//go:build integration

package integration

// Adversarial end-to-end tests for ADR039 (M13 approval and promotion). The
// attacker controls the API process (direct port calls with identity GUCs it
// sets itself), is a rogue or demoted approver, or races honest clients. Every
// test is named TestM13Adversarial... and runs on the isolated database.
//
// Division of labour proven here:
//   - The database alone (ports of migration 49) stops: approvals/activations by
//     non-members, viewers, disabled users and other tenants; the HIGH/CRITICAL
//     separation floors on DB-stamped parties (requester, acceptance author,
//     sandbox freezer); quorum re-counted under lock; base/pointer staleness;
//     rejected hashes; CAS on (hash, generation); rollback to never-activated
//     targets; non-POINTER_SAFE rollback without an acknowledged plan; every
//     direct write to the authority tables.
//   - Only Go recomputation (LoadCandidate/Restore + Staleness) stops: a
//     self-consistent forged body (lowered risk, edited report, claimed policy or
//     tool hashes) and policy/tool staleness. The tests marked RESIDUAL show the
//     raw ports accepting exactly these.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
	"github.com/jackc/pgx/v5/pgconn"
)

// ------------------------------------------------------------------ helpers

func m13Trim(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// m13Refused fails unless err carries one of the SQLSTATEs.
func m13Refused(t *testing.T, what string, err error, states ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted, want SQLSTATE %v", what, states)
		return
	}
	if got := sqlState(err); !slices.Contains(states, got) {
		t.Errorf("%s: %v (SQLSTATE %q), want %v", what, err, got, states)
	}
}

// m13Detail is the DETAIL of a database error (the separation role).
func m13Detail(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Detail
	}
	return ""
}

// m13Code fails unless the reply has the status and (when given) one of the codes.
func m13Code(t *testing.T, what string, w *httptest.ResponseRecorder, status int, codes ...string) m13Reply {
	t.Helper()
	r := m13Decode(w)
	if w.Code != status {
		t.Errorf("%s: %d %s, want %d %v", what, w.Code, m13Trim(w.Body.String()), status, codes)
		return r
	}
	if len(codes) > 0 && !slices.Contains(codes, r.Code) {
		t.Errorf("%s: code %q, want %v (%s)", what, r.Code, codes, m13Trim(w.Body.String()))
	}
	return r
}

func m13Sha(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }

// m13Forge turns a real candidate body into a COORDINATED forgery, as a
// compromised API process could: the diff is shrunk (approval gate and money
// conditions removed), facts, risk, HighRisk and the verifier report are
// recomputed from the shrunk diff, so the body is self-consistent for every check
// the database (and Restore without the base) can make. Taken from
// TestM12CoordinatedForgeryIsRefusedWhenLoaded.
func m13Forge(t *testing.T, raw []byte) (promotion.BundleV2, []byte) {
	t.Helper()
	var b promotion.BundleV2
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	d, err := compiler.ParseDiff(b.Diff.JSON, b.Diff.Hash)
	if err != nil {
		t.Fatal(err)
	}
	for i := range d.Changes {
		kept := []string{}
		for _, aspect := range d.Changes[i].Aspects {
			if aspect != compiler.AspectApprovalGate {
				kept = append(kept, aspect)
			}
		}
		d.Changes[i].Aspects = kept
	}
	d.MoneyConditions, d.ApprovalGates = []compiler.MoneyConditionChange{}, []compiler.ApprovalGateChange{}
	program, err := compiler.Restore(b.IR.JSON, b.IR.Hash, compiler.SupportedIRVersions())
	if err != nil {
		t.Fatal(err)
	}
	facts, err := risk.Normalize(promotion.ReduceFacts(promotion.FactsInput{Diff: d, Storage: b.Storage, TargetPins: promotion.ToolPins(program.IR()), Definitions: map[string]tools.Definition{}, TenantID: b.TenantID, Volume: b.Risk.Facts.Volume}))
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := risk.Evaluate(facts)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.HighRisk() {
		t.Fatalf("the forgery did not lower the risk (%s), so the test proves nothing", assessment.Level)
	}
	b.Diff.JSON, b.Diff.Hash = d.JSON(), d.Hash()
	b.Risk = promotion.RiskV2{PolicyVersion: assessment.PolicyVersion, PolicyHash: assessment.PolicyHash, Level: assessment.Level.String(), Reasons: assessment.Reasons, FactsHash: assessment.FactsHash,
		Facts: facts, Separation: assessment.Separation, Rollback: assessment.Rollback}
	b.HighRisk = assessment.HighRisk()
	set, _, _, err := intent.ParseRequirementSet(b.Requirements.JSON)
	if err != nil {
		t.Fatal(err)
	}
	spec, _, _, err := intent.ParseAcceptanceSpec(b.Acceptance.JSON)
	if err != nil {
		t.Fatal(err)
	}
	report, err := verification.Verify(verification.Input{Program: program, Requirements: set, Acceptance: spec, StorageClass: b.Storage.Class, Changed: promotion.ChangedFor(d, b.Storage),
		Tools: tools.EmptyCatalog(), TenantID: b.TenantID, SandboxEvidenceHash: b.Sandbox.EvidenceHash})
	if err != nil {
		t.Fatal(err)
	}
	b.Verification.Report, b.Verification.ReportHash, b.Verification.Outcome = report.JSON(), report.Hash(), report.Outcome
	body, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return b, body
}

// ---------------------------------------------- 1. compromised API process

// A compromised API process stages a coordinated forgery of a real HIGH
// candidate (shrunk diff, recomputed LOW/MEDIUM risk and verifier report)
// directly through stage_candidate. The HTTP approve/reject/activate routes
// recompute it from the stored bytes and the candidate's own base and refuse
// it (candidate_failed_verification) without writing any row. The raw ports
// cannot recompute and accept it: that is the documented ADR039 residual.
func TestM13AdversarialCompromisedAPIForgedCandidate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	forged, body := m13Forge(t, tmpl.raw)
	hash, err := e.stageRaw(e.carol, body)
	if err != nil {
		t.Fatalf("the stage port is expected to accept a self-consistent forged body: %v", err)
	}
	// The index row binds the parties from database rows, not from the body.
	var level, requester, author, freezer string
	var high bool
	if err = e.f.operator.QueryRow(`SELECT risk_level,high_risk,requester_id::text,acceptance_author::text,sandbox_frozen_by::text FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`,
		e.f.tenantA, e.app, hash).Scan(&level, &high, &requester, &author, &freezer); err != nil {
		t.Fatal(err)
	}
	if level != forged.Risk.Level || high || requester != e.carol.id || author != e.f.bob || freezer != e.f.alice {
		t.Fatalf("forged index row: %s %v %s %s %s", level, high, requester, author, freezer)
	}

	// Go recomputation refuses it on every decision route, writing nothing.
	before := e.state()
	m13Code(t, "approve a forged candidate (independent approver)", e.httpApprove(e.dave.token, hash), 409, "candidate_failed_verification")
	m13Code(t, "approve a forged candidate (requester, LOW per the forgery)", e.httpApprove(e.carol.token, hash), 409, "candidate_failed_verification")
	m13Code(t, "reject a forged candidate", e.httpReject(e.dave.token, hash, "OTHER", "forged"), 409, "candidate_failed_verification")
	m13Code(t, "activate a forged candidate", e.httpActivate(e.dave.token, hash, "", 0), 409, "candidate_failed_verification")
	m13Code(t, "read a forged candidate", e.httpCandidate(e.viewer, hash), 409, "candidate_failed_verification")
	e.unchanged("forged candidate through HTTP", before)
	if a, r := e.approvalsOf(hash), e.rejectionsOf(hash); len(a) != 0 || len(r) != 0 {
		t.Fatalf("a forged candidate got decision rows: approvals %v rejections %v", a, r)
	}
	if n := e.auditCount("releases.approve.rejected"); n < 3 {
		t.Errorf("refused approve/reject requests must be audited: %d", n)
	}
	if n := e.auditCount("releases.activate.rejected"); n < 1 {
		t.Errorf("refused activation must be audited: %d", n)
	}
	// The honest candidate is unaffected by the forgery.
	if w := e.httpCandidate(e.viewer, tmpl.hash); w.Code != 200 || m13Decode(w).RiskLevel != "HIGH" {
		t.Fatalf("honest candidate: %d %s", w.Code, m13Trim(w.Body.String()))
	}

	// Simple, uncoordinated forgeries are already refused by Restore.
	other := strings.Repeat("e", 64)
	for name, mutate := range map[string]func(*promotion.BundleV2){
		"risk lowered without recomputation": func(b *promotion.BundleV2) {
			b.Risk.Level, b.HighRisk, b.Risk.Separation = "LOW", false, risk.Separation{Approvals: 1, Excluded: []string{}}
		},
		"verifier report edited": func(b *promotion.BundleV2) {
			var r map[string]json.RawMessage
			if json.Unmarshal(b.Verification.Report, &r) != nil {
				t.Fatal("report shape")
			}
			r["requirement_policy_version"] = json.RawMessage(`"requirement-policy-forged"`)
			b.Verification.Report, _ = json.Marshal(r)
			b.Verification.ReportHash = m13Sha(b.Verification.Report)
		},
		"policy bundle hash claimed": func(b *promotion.BundleV2) { b.Dependencies.PolicyBundleHash = other },
		"tool pin hash claimed":      func(b *promotion.BundleV2) { b.Dependencies.ToolsHash = other },
	} {
		h, err := e.stageDoctored(mutate)
		if err != nil {
			t.Fatalf("%s: stage port refused (%v); it cannot recompute these", name, err)
		}
		m13Code(t, name+": approve", e.httpApprove(e.dave.token, h), 409, "candidate_failed_verification")
		m13Code(t, name+": activate", e.httpActivate(e.dave.token, h, "", 0), 409, "candidate_failed_verification")
	}
	// Identity forgeries never reach the index: the database binds them.
	for name, c := range map[string]struct {
		mutate func(*promotion.BundleV2)
		state  string
	}{
		"requester claimed":         {func(b *promotion.BundleV2) { b.RequesterID = e.dave.id }, "22023"},
		"acceptance author claimed": {func(b *promotion.BundleV2) { b.Acceptance.AuthorID = e.dave.id }, "55000"},
		"sandbox freezer claimed":   {func(b *promotion.BundleV2) { b.Sandbox.FrozenBy = e.dave.id }, "55000"},
		"HIGH level, flag cleared":  {func(b *promotion.BundleV2) { b.HighRisk = false }, "22023"},
	} {
		_, err := e.stageDoctored(c.mutate)
		m13Refused(t, name, err, c.state)
	}
	e.unchanged("forgeries through HTTP", before)

	// What the raw ports alone do. The database floors still bind the honest
	// HIGH candidate's DB-stamped parties...
	_, err = e.approvePort(e.carol.subject, tmpl.hash)
	if m13Refused(t, "requester approves the honest HIGH candidate through the port", err, "42501"); m13Detail(err) != "requester" {
		t.Errorf("separation detail: %q", m13Detail(err))
	}
	// ...but RESIDUAL (ADR039 Consequences): the database cannot recompute the
	// verifier, so the forged LOW candidate is self-approved and activated by its
	// requester alone when the API process skips Go recomputation.
	if n, err := e.approvePort(e.carol.subject, hash); err != nil || n != 1 {
		t.Fatalf("RESIDUAL changed: the port refused self-approval of a forged LOW candidate (%d, %v); update the residual list", n, err)
	}
	if g, err := e.activatePort(e.carol.subject, hash, "", 0); err != nil || g != 1 {
		t.Fatalf("RESIDUAL changed: the port refused the forged activation (%d, %v)", g, err)
	}
	t.Logf("RESIDUAL: raw ports self-approved and activated forged candidate %s (risk %s); only Go recomputation refuses it", hash[:12], forged.Risk.Level)
	// The routes still refuse to present or build on the unverifiable release.
	m13Code(t, "active forged release", e.httpActive(e.viewer), 409, "candidate_failed_verification")
	e.checkHistory()
}
