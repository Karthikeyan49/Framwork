//go:build integration

package integration

// Adversarial end-to-end tests for ADR103 (M13 approval and promotion). The
// attacker controls the API process (direct port calls with identity GUCs it
// sets itself), is a rogue or demoted approver, or races honest clients. Every
// test is named TestM13Adversarial... and runs on the isolated database.
//
// Division of labour proven here:
//   - The database alone (ports of migration 84) stops: approvals/activations by
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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
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
// cannot recompute and accept it: that is the documented ADR103 residual.
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
	// ...but RESIDUAL (ADR103 Consequences): the database cannot recompute the
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

// ------------------------------------------------- 2. rogue approver matrix

// m13Rogues adds the ineligible approvers: frank (admin demoted to viewer),
// gina (membership deactivated), hank (user disabled) and ivan (role member).
func m13Rogues(e *m13Env) {
	for _, name := range []string{"frank", "gina", "hank"} {
		e.member(name, "admin")
	}
	e.member("ivan", "member")
	e.setRole(e.person("frank").id, "viewer")
	e.setMemberActive(e.person("gina").id, false)
	e.setUserActive(e.person("hank").id, false)
}

func m13Relabel(label string) func(*metadata.Document) {
	return func(d *metadata.Document) {
		for i := range d.Definitions {
			if d.Definitions[i].ID == "entity.order" {
				d.Definitions[i].Label = label
			}
		}
	}
}

// Requester carol, acceptance author bob and sandbox freezer alice each try to
// approve their own candidate at every level, together with a demoted, a
// removed, a disabled, a member-role and a viewer approver, through the raw port
// (the database alone) and through HTTP (policy + Go separation + port).
func TestM13AdversarialRogueApproverMatrix(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	m13Rogues(e)
	order := []string{"carol", "bob", "alice", "viewer", "frank", "gina", "hank", "ivan", "dave"}
	// Database floor (ADR103 section 3): LOW/MEDIUM none; HIGH requester and
	// acceptance author; CRITICAL also the freezer. Non-members, viewers and
	// disabled users never. A member-role user passes the database floor; only
	// the Go policy (owner/admin) refuses it.
	excluded := map[string]map[string]string{
		"LOW": {}, "MEDIUM": {},
		"HIGH":     {"carol": "requester", "bob": "acceptance_author"},
		"CRITICAL": {"carol": "requester", "bob": "acceptance_author", "alice": "sandbox_freezer"},
	}
	ineligible := map[string]bool{"viewer": true, "frank": true, "gina": true, "hank": true}
	for _, level := range []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"} {
		hash, err := e.stageLevel(level)
		if err != nil {
			t.Fatal(level, err)
		}
		want := []string{}
		for _, name := range order {
			p := e.person(name)
			n, err := e.approvePort(p.subject, hash)
			role, barred := excluded[level][name]
			switch {
			case ineligible[name]:
				if m13Refused(t, level+" port: "+name, err, "42501"); m13Detail(err) != "" {
					t.Errorf("%s port: %s got a separation detail %q", level, name, m13Detail(err))
				}
			case barred:
				if m13Refused(t, level+" port: "+name, err, "42501"); m13Detail(err) != role {
					t.Errorf("%s port: %s detail %q, want %q", level, name, m13Detail(err), role)
				}
			default:
				want = append(want, p.id)
				if err != nil || n != len(want) {
					t.Errorf("%s port: %s approval = %d, %v (want count %d)", level, name, n, err, len(want))
				}
			}
		}
		slices.Sort(want)
		if got := e.approvalsOf(hash); !slices.Equal(got, want) {
			t.Errorf("%s approvals %v, want %v", level, got, want)
		}
		// HTTP refuses the doctored body for everyone before any separation logic.
		if level != "HIGH" {
			m13Code(t, level+" doctored via HTTP", e.httpApprove(e.erin.token, hash), 409, "candidate_failed_verification")
		}
	}

	// HTTP on the real HIGH template.
	before := e.state()
	leak := []string{e.carol.id, e.f.bob, e.f.alice}
	for _, c := range []struct {
		name   string
		status int
		code   string
		role   string
	}{
		{"carol", 403, "separation_of_duties", "requester"},
		{"bob", 403, "separation_of_duties", "acceptance_author"},
		{"viewer", 403, "forbidden", ""},
		{"frank", 403, "forbidden", ""},
		{"gina", 403, "forbidden", ""},
		{"hank", 401, "unauthenticated", ""},
		{"ivan", 403, "forbidden", ""},
	} {
		w := e.httpApprove(e.person(c.name).token, tmpl.hash)
		r := m13Code(t, "HTTP HIGH approve by "+c.name, w, c.status, c.code)
		if c.role != "" && !strings.Contains(r.Message, c.role) {
			t.Errorf("separation reply for %s must name the role %q: %s", c.name, c.role, r.Message)
		}
		for _, id := range leak {
			if strings.Contains(w.Body.String(), id) {
				t.Errorf("refusal for %s leaks an actor id: %s", c.name, w.Body.String())
			}
		}
		// Activation is a separate action: rogues are refused by policy, the
		// admin parties reach the quorum check.
		activation, code := 403, "forbidden"
		switch c.name {
		case "hank":
			activation, code = 401, "unauthenticated"
		case "carol", "bob":
			activation, code = 409, "approval_quorum_not_met"
		}
		m13Code(t, "HTTP HIGH activate by "+c.name, e.httpActivate(e.person(c.name).token, tmpl.hash, "", 0), activation, code)
	}
	e.unchanged("rogue HTTP approvals", before)
	// Note: requester and acceptance author are not activators by policy, but
	// carol (admin) may activate once an independent approval exists.
	m13Code(t, "HTTP HIGH activation without approval", e.httpActivate(e.carol.token, tmpl.hash, "", 0), 409, "approval_quorum_not_met")
	if r := m13Code(t, "freezer approves HIGH", e.httpApprove(e.alice, tmpl.hash), 201); r.ApprovalsRequired != 1 || r.Status != "APPROVED" {
		t.Errorf("approved HIGH view: %+v", r)
	}
	m13Code(t, "independent approval", e.httpApprove(e.dave.token, tmpl.hash), 201)
	m13Code(t, "requester activates an independently approved HIGH candidate", e.httpActivate(e.carol.token, tmpl.hash, "", 0), 200)

	// HTTP on a real LOW candidate (label-only second release): no exclusions.
	low, _ := e.build("500", m13Relabel("Purchase order"), e.carol)
	if r := m13Decode(e.httpCandidate(e.viewer, low)); r.RiskLevel != "LOW" {
		t.Fatalf("precondition: the label-only release rates LOW, got %+v", r)
	}
	for _, c := range []struct {
		name   string
		status int
	}{{"viewer", 403}, {"frank", 403}, {"gina", 403}, {"hank", 401}, {"ivan", 403}, {"carol", 201}, {"bob", 201}} {
		m13Code(t, "HTTP LOW approve by "+c.name, e.httpApprove(e.person(c.name).token, low), c.status)
	}
	if got := e.approvalsOf(low); len(got) != 2 {
		t.Errorf("LOW approvals: %v", got)
	}
	m13Code(t, "requester activates a self-approved LOW candidate", e.httpActivate(e.carol.token, low, tmpl.hash, 1), 200)
	if hash, gen, _ := e.activePointer(); hash != low || gen != 2 {
		t.Fatalf("pointer %s@%d", hash, gen)
	}
	e.checkHistory()
}

// Quorum is re-counted at activation from live, eligible approvers: an approval
// of a since demoted, removed or disabled approver no longer counts, and
// excluded parties never count, through the port and through HTTP.
func TestM13AdversarialQuorumCountsOnlyLiveEligibleApprovers(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	// HTTP: the real HIGH template approved only by a since-demoted approver.
	tmpl := e.template()
	m13Code(t, "approve", e.httpApprove(e.dave.token, tmpl.hash), 201)
	e.setRole(e.dave.id, "viewer")
	before := e.state()
	m13Code(t, "activation counting a demoted approver", e.httpActivate(e.erin.token, tmpl.hash, "", 0), 409)
	e.unchanged("activation with a demoted approver", before)
	e.setRole(e.dave.id, "admin")
	m13Code(t, "activation after the approver is restored", e.httpActivate(e.erin.token, tmpl.hash, "", 0), 200)

	// Ports: a CRITICAL candidate on the live pointer.
	x, err := e.stageLevel("CRITICAL")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"carol", "bob", "alice"} {
		_, err := e.approvePort(e.person(name).subject, x)
		m13Refused(t, "CRITICAL excluded party "+name, err, "42501")
	}
	if n, err := e.approvePort(e.dave.subject, x); err != nil || n != 1 {
		t.Fatal("dave approves", n, err)
	}
	_, err = e.activatePort(e.carol.subject, x, tmpl.hash, 1)
	m13Refused(t, "CRITICAL with one approval", err, "55000")
	if n, err := e.approvePort(e.erin.subject, x); err != nil || n != 2 {
		t.Fatal("erin approves", n, err)
	}
	before = e.state()
	for _, c := range []struct {
		name        string
		spoil, mend func()
	}{
		{"approver demoted to viewer", func() { e.setRole(e.erin.id, "viewer") }, func() { e.setRole(e.erin.id, "admin") }},
		{"approver membership removed", func() { e.setMemberActive(e.dave.id, false) }, func() { e.setMemberActive(e.dave.id, true) }},
		{"approver user disabled", func() { e.setUserActive(e.dave.id, false) }, func() { e.setUserActive(e.dave.id, true) }},
	} {
		c.spoil()
		_, err := e.activatePort(e.carol.subject, x, tmpl.hash, 1)
		m13Refused(t, "CRITICAL activation after "+c.name, err, "55000")
		c.mend()
	}
	_, err = e.activatePort(e.person("viewer").subject, x, tmpl.hash, 1)
	m13Refused(t, "viewer activates", err, "42501")
	e.unchanged("refused quorum activations", before)
	if g, err := e.activatePort(e.carol.subject, x, tmpl.hash, 1); err != nil || g != 2 {
		t.Fatal("CRITICAL activation with two live independent approvals", g, err)
	}
	h := e.checkHistory()
	if len(h) != 2 || h[1].kind != "ACTIVATE" || h[1].riskLevel != "CRITICAL" || h[1].actor != e.carol.id || h[1].previous != tmpl.hash || h[0].actor != e.erin.id {
		t.Fatalf("history %+v", h)
	}
}

// --------------------------------------------------- 3. legacy path closed

// execIn runs one statement as subject (direct identity binding) and commits.
func (e *m13Env) execIn(subject, query string, args ...any) error {
	return postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, subject, e.f.tenantA); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, query, args...)
		return err
	})
}

// The M6R write paths are gone: the runtime role has no DML or TRUNCATE on any
// release authority table (even for its own HIGH candidate, the review finding
// that the requester could self-approve through direct SQL), the operator cannot
// change or hand-write authority rows, the worker drift guard covers the tables
// and a 1.0.0 bundle (no candidate row) can no longer be approved or activated.
func TestM13AdversarialLegacyWritePathsAreClosed(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	if _, err := e.approvePort(e.dave.subject, tmpl.hash); err != nil {
		t.Fatal(err)
	}
	if _, err := e.activatePort(e.dave.subject, tmpl.hash, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rejectPort(e.erin.subject, tmpl.hash, "OTHER", "kept for the immutability probes"); err != nil {
		t.Fatal(err)
	}
	pending, err := e.stageLevel("HIGH")
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{"control.release_approvals", "control.active_releases", "control.release_rejections", "control.release_history"}
	for _, table := range tables {
		for _, privilege := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			var has bool
			if err := e.f.operator.QueryRow(`SELECT has_table_privilege('framework_app',$1,$2)`, table, privilege).Scan(&has); err != nil || has {
				t.Errorf("framework_app has %s on %s (%v)", privilege, table, err)
			}
		}
		var column bool
		if err := e.f.operator.QueryRow(`SELECT has_any_column_privilege('framework_app',$1,'INSERT') OR has_any_column_privilege('framework_app',$1,'UPDATE')`, table).Scan(&column); err != nil || column {
			t.Errorf("framework_app keeps a column write privilege on %s (%v)", table, err)
		}
	}
	before := e.state()
	a, app := e.f.tenantA, e.app
	for name, c := range map[string]struct {
		subject, query string
		args           []any
	}{
		"requester self-approval (M6R insert)": {e.carol.subject, `INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash) VALUES($1,$2,$3)`, []any{a, app, pending}},
		"approval with an actor id":            {e.dave.subject, `INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash,actor_id) VALUES($1,$2,$3,$4)`, []any{a, app, pending, e.dave.id}},
		"pointer update (M6R activate)":        {e.dave.subject, `UPDATE control.active_releases SET artifact_hash=$3,generation=generation+1 WHERE tenant_id=$1 AND app_id=$2`, []any{a, app, pending}},
		"pointer insert":                       {e.dave.subject, `INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES($1,$2,$3,9)`, []any{a, app, pending}},
		"pointer delete":                       {e.dave.subject, `DELETE FROM control.active_releases WHERE tenant_id=$1 AND app_id=$2`, []any{a, app}},
		"history insert":                       {e.dave.subject, `INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,risk_level,storage_class) VALUES($1,$2,2,'ACTIVATE',$3,'',2,'LOW','EQUAL')`, []any{a, app, pending}},
		"history delete":                       {e.dave.subject, `DELETE FROM control.release_history WHERE tenant_id=$1 AND app_id=$2`, []any{a, app}},
		"history update":                       {e.dave.subject, `UPDATE control.release_history SET actor_id=$3 WHERE tenant_id=$1 AND app_id=$2`, []any{a, app, e.dave.id}},
		"rejection delete":                     {e.dave.subject, `DELETE FROM control.release_rejections WHERE tenant_id=$1 AND app_id=$2`, []any{a, app}},
		"rejection insert":                     {e.dave.subject, `INSERT INTO control.release_rejections(tenant_id,app_id,artifact_hash,reason) VALUES($1,$2,$3,'OTHER')`, []any{a, app, pending}},
		"approval delete":                      {e.dave.subject, `DELETE FROM control.release_approvals WHERE tenant_id=$1 AND app_id=$2`, []any{a, app}},
		"truncate approvals":                   {e.dave.subject, `TRUNCATE control.release_approvals`, nil},
		"truncate pointer":                     {e.dave.subject, `TRUNCATE control.active_releases`, nil},
		"truncate history":                     {e.dave.subject, `TRUNCATE control.release_history`, nil},
		"truncate rejections":                  {e.dave.subject, `TRUNCATE control.release_rejections`, nil},
		"internal approval counter":            {e.dave.subject, `SELECT control.release_approval_count($1,$2,$3)`, []any{a, app, pending}},
	} {
		m13Refused(t, "framework_app "+name, e.execIn(c.subject, c.query, c.args...), "42501")
	}
	_, err = e.approvePort(e.carol.subject, pending)
	m13Refused(t, "requester self-approval through the port", err, "42501")
	e.unchanged("direct runtime DML", before)

	// The operator cannot rewrite or hand-write authority either.
	for _, statement := range []string{
		`UPDATE control.release_approvals SET approved_at=approved_at WHERE tenant_id=$1 AND app_id=$2`,
		`DELETE FROM control.release_approvals WHERE tenant_id=$1 AND app_id=$2`,
		`UPDATE control.release_history SET plan_hash=NULL WHERE tenant_id=$1 AND app_id=$2`,
		`DELETE FROM control.release_history WHERE tenant_id=$1 AND app_id=$2`,
		`UPDATE control.release_rejections SET note='' WHERE tenant_id=$1 AND app_id=$2`,
		`DELETE FROM control.release_rejections WHERE tenant_id=$1 AND app_id=$2`,
		`DELETE FROM control.active_releases WHERE tenant_id=$1 AND app_id=$2`,
	} {
		if _, err := e.f.operator.Exec(statement, a, app); err == nil {
			t.Errorf("operator changed release authority: %s", statement)
		}
	}
	if _, err := e.f.operator.Exec(`INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash,actor_id) VALUES($1,$2,$3,$4)`, a, app, pending, e.dave.id); err == nil {
		t.Error("operator hand-wrote an approval")
	}
	// A hand-written pointer move (no history row) fails at commit.
	if _, err := e.f.operator.Exec(`UPDATE control.active_releases SET artifact_hash=$3,generation=generation+1 WHERE tenant_id=$1 AND app_id=$2`, a, app, pending); err == nil {
		t.Error("operator moved the pointer without history")
	}
	for _, table := range tables {
		tx, err := e.f.operator.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`TRUNCATE ` + table + ` CASCADE`); err == nil {
			t.Errorf("operator TRUNCATE %s succeeded", table)
		}
		_ = tx.Rollback()
	}
	e.unchanged("operator writes", before)

	// Workers hold nothing; the drift guard trips on any column grant.
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	m12rDropFixtureGrants(t, e.f.operator)
	var safe bool
	if err = worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe); err != nil || !safe {
		t.Fatal("worker grants unsafe before the probe", err)
	}
	for _, table := range tables {
		if _, err = e.f.operator.Exec(`GRANT SELECT(app_id) ON ` + table + ` TO framework_worker`); err != nil {
			t.Fatal(err)
		}
		scanErr := worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe)
		if _, err = e.f.operator.Exec(`REVOKE SELECT(app_id) ON ` + table + ` FROM framework_worker`); err != nil {
			t.Fatal(err)
		}
		if scanErr != nil || safe {
			t.Errorf("worker drift guard missed a column grant on %s (%v)", table, scanErr)
		}
	}

	// An M6R-shaped 1.0.0 bundle has no candidate row: it is never approvable.
	legacy := []byte(`{"Version":"1.0.0","AppID":"` + app + `","RequesterID":"` + e.carol.id + `","HighRisk":false,"IRHash":"` + strings.Repeat("a", 64) + `"}`)
	legacyHash := promotion.Hash(legacy)
	if _, err = e.f.operator.Exec(`INSERT INTO control.release_bundles(tenant_id,app_id,artifact_hash,body,created_by) VALUES($1,$2,$3,$4,$5)`, a, app, legacyHash, legacy, e.carol.id); err != nil {
		t.Fatal("seed a 1.0.0 bundle row", err)
	}
	_, err = e.approvePort(e.dave.subject, legacyHash)
	m13Refused(t, "approve a 1.0.0 bundle", err, "P0002")
	_, err = e.rejectPort(e.dave.subject, legacyHash, "OTHER", "")
	m13Refused(t, "reject a 1.0.0 bundle", err, "P0002")
	current, generation, _ := e.activePointer()
	_, err = e.activatePort(e.dave.subject, legacyHash, current, generation)
	m13Refused(t, "activate a 1.0.0 bundle", err, "P0002")
	_, err = e.rollbackPort(e.dave.subject, legacyHash, current, generation, strings.Repeat("c", 64), true)
	m13Refused(t, "roll back to a never-activated 1.0.0 bundle", err, "55000")
	// Through HTTP the body does not restore as a candidate (Go refuses before
	// the port): 409 candidate_failed_verification; a 404 would be as safe.
	for what, w := range map[string]*httptest.ResponseRecorder{
		"approve":  e.httpApprove(e.dave.token, legacyHash),
		"reject":   e.httpReject(e.dave.token, legacyHash, "OTHER", ""),
		"activate": e.httpActivate(e.dave.token, legacyHash, current, generation),
	} {
		if r := m13Decode(w); !(w.Code == 404 || w.Code == 409 && r.Code == "candidate_failed_verification") {
			t.Errorf("HTTP %s a 1.0.0 bundle: %d %s", what, w.Code, m13Trim(w.Body.String()))
		}
	}
	e.unchanged("1.0.0 bundle decisions", before)
}

// ----------------------------------------------------------------- 4. races

const m13Iterations = 20

// m13Race starts every fn at the same barrier, each on its own connection
// (each port call opens its own runtime transaction), and returns their errors.
func m13Race(fns ...func() error) []error {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, len(fns))
	for i, fn := range fns {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			errs[i] = fn()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return errs
}

// m13LostRace is how a losing racer may fail (ADR103 section 8; CONTRACT_CHANGES
// m13-db 6): the CAS/state refusal, a serialization or deadlock abort, or the
// runtime lock timeout. All map to 409 without retry.
var m13LostRace = []string{"55000", "40001", "40P01", "55P03"}

func m13CountRows(e *m13Env, table string) int {
	e.t.Helper()
	var n int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// N parallel approvals by the same actor record one row; every caller either
// gets the count 1 or loses the race with a 409-class SQLSTATE.
func TestM13AdversarialRaceSameActorApprovals(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for i := 0; i < m13Iterations; i++ {
		hash, err := e.stageLevel("LOW")
		if err != nil {
			t.Fatal(err)
		}
		fns := make([]func() error, 8)
		counts := make([]int, len(fns))
		for j := range fns {
			fns[j] = func() error {
				var err error
				counts[j], err = e.approvePort(e.dave.subject, hash)
				return err
			}
		}
		wins := 0
		for j, err := range m13Race(fns...) {
			switch {
			case err == nil && counts[j] == 1:
				wins++
			case err == nil:
				t.Errorf("iteration %d: idempotent approval returned count %d", i, counts[j])
			default:
				m13Refused(t, "losing parallel approval", err, m13LostRace...)
			}
		}
		if got := e.approvalsOf(hash); wins == 0 || len(got) != 1 || got[0] != e.dave.id {
			t.Fatalf("iteration %d: %d successes, approvals %v", i, wins, got)
		}
	}
	// The same race through HTTP on the real template: one row, at most one 201.
	tmpl := e.template()
	start := time.Now()
	single := e.httpCandidate(e.dave.token, tmpl.hash)
	t.Logf("one candidate read (restore and recompute) took %v: %d", time.Since(start), single.Code)
	fns := make([]func() error, 8)
	codes := make([]int, len(fns))
	took := make([]time.Duration, len(fns))
	for j := range fns {
		fns[j] = func() error {
			at := time.Now()
			codes[j] = e.httpApprove(e.dave.token, tmpl.hash).Code
			took[j] = time.Since(at)
			return nil
		}
	}
	m13Race(fns...)
	t.Logf("parallel HTTP approvals: %v in %v", codes, took)
	// Each request restores and recomputes the candidate inside its 3-second
	// runtime transaction; under -race and a shared CPU some exceed it and fail
	// closed with 503 (rolled back). That is an envelope note, not a race win.
	created, replayed, timedOut := 0, 0, 0
	for _, c := range codes {
		switch c {
		case 201:
			created++
		case 200:
			replayed++
		case 503:
			timedOut++
		case 409:
		default:
			t.Errorf("parallel HTTP approval: %d", c)
		}
	}
	got := e.approvalsOf(tmpl.hash)
	if created > 1 || (created == 1) != (len(got) == 1) || replayed > 0 && len(got) != 1 || len(got) > 1 {
		t.Fatalf("parallel HTTP approvals: %v, rows %v", codes, got)
	}
	if timedOut > 0 {
		t.Logf("ENVELOPE: %d of %d parallel approvals exceeded the runtime transaction budget (503, nothing written)", timedOut, len(codes))
	}
}

// Two activations of two different approved candidates on the same base:
// exactly one wins per round, the pointer is the winner and history stays a
// dense chain. Round 1 is the first-release insert path, later rounds the CAS
// update path.
func TestM13AdversarialRaceTwoActivationsOnOneBase(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for i := 0; i < m13Iterations; i++ {
		base, generation, _ := e.activePointer()
		hashes := make([]string, 2)
		for j := range hashes {
			h, err := e.stageLevel("LOW")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.approvePort(e.dave.subject, h); err != nil {
				t.Fatal(err)
			}
			hashes[j] = h
		}
		got := make([]int64, 2)
		errs := m13Race(
			func() (err error) { got[0], err = e.activatePort(e.dave.subject, hashes[0], base, generation); return },
			func() (err error) { got[1], err = e.activatePort(e.erin.subject, hashes[1], base, generation); return },
		)
		winner := -1
		for j, err := range errs {
			if err == nil {
				if winner >= 0 || got[j] != generation+1 {
					t.Fatalf("round %d: second winner or wrong generation %v %v", i, got, errs)
				}
				winner = j
			} else {
				m13Refused(t, "losing activation", err, m13LostRace...)
			}
		}
		if winner < 0 {
			t.Fatalf("round %d: no activation won: %v", i, errs)
		}
		if hash, g, _ := e.activePointer(); hash != hashes[winner] || g != generation+1 {
			t.Fatalf("round %d: pointer %s@%d, winner %s", i, hash, g, hashes[winner])
		}
	}
	if rows := e.checkHistory(); len(rows) != m13Iterations || rows[len(rows)-1].generation != m13Iterations {
		t.Fatalf("history rows %d", len(rows))
	}
	if n := m13CountRows(e, "control.release_approvals"); n != 2*m13Iterations {
		t.Fatalf("approval rows %d", n)
	}
}

// An approval racing the activation that needs it: activation either sees the
// committed second approval or refuses the quorum; it never activates on one.
// A racing approval by an excluded party can never complete the quorum.
func TestM13AdversarialRaceApproveVersusActivate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for i := 0; i < m13Iterations; i++ {
		base, generation, _ := e.activePointer()
		x, err := e.stageLevel("CRITICAL")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.approvePort(e.dave.subject, x); err != nil {
			t.Fatal(err)
		}
		// The excluded requester races the activation: always refused.
		errs := m13Race(
			func() error { _, err := e.approvePort(e.carol.subject, x); return err },
			func() error { _, err := e.activatePort(e.erin.subject, x, base, generation); return err },
		)
		m13Refused(t, "excluded racing approval", errs[0], "42501")
		m13Refused(t, "activation racing an excluded approval", errs[1], append([]string{"55000"}, m13LostRace...)...)
		// An eligible approver races the activation.
		var activated int64
		errs = m13Race(
			func() error { _, err := e.approvePort(e.erin.subject, x); return err },
			func() (err error) { activated, err = e.activatePort(e.carol.subject, x, base, generation); return },
		)
		approvals := e.approvalsOf(x)
		if errs[1] == nil {
			if activated != generation+1 || errs[0] != nil || len(approvals) != 2 {
				t.Fatalf("round %d: activated %d with approvals %v (approve err %v)", i, activated, approvals, errs[0])
			}
			continue
		}
		m13Refused(t, "activation racing an eligible approval", errs[1], append([]string{"55000"}, m13LostRace...)...)
		if errs[0] != nil {
			m13Refused(t, "approval racing an activation", errs[0], m13LostRace...)
			if _, err = e.approvePort(e.erin.subject, x); err != nil {
				t.Fatal(err)
			}
		}
		if g, err := e.activatePort(e.carol.subject, x, base, generation); err != nil || g != generation+1 {
			t.Fatalf("round %d: sequential activation after the race: %d %v", i, g, err)
		}
	}
	e.checkHistory()
}

// A rollback racing an activation on the same pointer: exactly one moves it.
func TestM13AdversarialRaceRollbackVersusActivate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	plan := strings.Repeat("c", 64)
	for k := 0; k < 2; k++ {
		base, generation, _ := e.activePointer()
		h, err := e.stageLevel("LOW")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.approvePort(e.dave.subject, h); err != nil {
			t.Fatal(err)
		}
		if _, err = e.activatePort(e.dave.subject, h, base, generation); err != nil {
			t.Fatal(err)
		}
	}
	rollbacks, activations := 0, 0
	for i := 0; i < m13Iterations; i++ {
		rows := e.historyRows()
		current, generation, _ := e.activePointer()
		target := rows[len(rows)-1].previous
		next, err := e.stageLevel("LOW")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.approvePort(e.dave.subject, next); err != nil {
			t.Fatal(err)
		}
		got := make([]int64, 2)
		errs := m13Race(
			func() (err error) {
				got[0], err = e.rollbackPort(e.erin.subject, target, current, generation, plan, true)
				return
			},
			func() (err error) { got[1], err = e.activatePort(e.dave.subject, next, current, generation); return },
		)
		won := ""
		for j, err := range errs {
			if err != nil {
				m13Refused(t, "losing pointer move", err, m13LostRace...)
				continue
			}
			if won != "" || got[j] != generation+1 {
				t.Fatalf("round %d: two pointer moves or a wrong generation: %v %v", i, got, errs)
			}
			won = []string{target, next}[j]
			if j == 0 {
				rollbacks++
			} else {
				activations++
			}
		}
		if hash, g, _ := e.activePointer(); won == "" || hash != won || g != generation+1 {
			t.Fatalf("round %d: pointer %s@%d after %v", i, hash, g, errs)
		}
	}
	rows := e.checkHistory()
	if len(rows) != 2+m13Iterations {
		t.Fatalf("history rows %d", len(rows))
	}
	t.Logf("rollback won %d rounds, activation won %d", rollbacks, activations)
}

// A rejection racing an approval: whatever the order, the hash ends rejected and
// can never be activated, even holding the approval.
func TestM13AdversarialRaceRejectVersusApprove(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	approved := 0
	for i := 0; i < m13Iterations; i++ {
		base, generation, _ := e.activePointer()
		x, err := e.stageLevel("LOW")
		if err != nil {
			t.Fatal(err)
		}
		var recorded bool
		errs := m13Race(
			func() (err error) {
				recorded, err = e.rejectPort(e.f.bobSubject, x, "RISK_UNACCEPTABLE", "race")
				return
			},
			func() error { _, err := e.approvePort(e.dave.subject, x); return err },
		)
		if errs[0] != nil || !recorded {
			t.Fatalf("round %d: the veto failed: %v", i, errs[0])
		}
		if errs[1] == nil {
			approved++
		} else {
			m13Refused(t, "approval racing a rejection", errs[1], m13LostRace...)
		}
		_, err = e.activatePort(e.erin.subject, x, base, generation)
		m13Refused(t, "activation of a rejected hash", err, "55000")
	}
	if n := m13CountRows(e, "control.release_history"); n != 0 {
		t.Fatalf("a rejected hash was activated: %d history rows", n)
	}
	t.Logf("approval committed before the rejection in %d of %d rounds", approved, m13Iterations)
}

// ------------------------------------------------------- 5. staleness gates

// publishRole publishes a custom tenant role version as the owner (HTTP).
func (e *m13Env) publishRole(name string, expected int64, actions ...string) {
	e.t.Helper()
	def := map[string]any{"schema_version": "tenant-capabilities-v1", "name": name, "grants": []any{map[string]any{"effect": "ALLOW", "actions": actions,
		"resource": map[string]string{"app_id": "*", "entity_id": "*", "field_id": "*"}}}}
	w := m5Call(e.h, "/v1/tenants/"+e.f.tenantA+"/roles", e.alice, map[string]any{"operation_id": uuid(e.t), "expected_version": expected, "active": true, "definition": def})
	if w.Code/100 != 2 {
		e.t.Fatal("publish role", w.Code, w.Body.String())
	}
}

func m13Reasons(t *testing.T, what string, r m13Reply, want ...string) {
	t.Helper()
	if !slices.IsSorted(r.Reasons) || !slices.Equal(r.Reasons, want) {
		t.Errorf("%s: stale reasons %v, want sorted %v", what, r.Reasons, want)
	}
}

// Staleness is re-evaluated inside the approve and the activate transaction: a
// moved base and a changed stored role head each refuse with 409
// candidate_stale and the exact sorted reasons, and change nothing. The database
// re-checks the base itself; it cannot see a policy change (RESIDUAL, shown at
// the end). Sandbox evidence cannot be unfrozen (immutable), and a tool-catalog
// change needs a tool-pinned candidate: both NOT COVERED here.
func TestM13AdversarialStalenessAtApproveAndActivate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.publishRole("auditor", 0, "candidates.read")
	tmpl := e.template()
	m13Code(t, "approve R1", e.httpApprove(e.dave.token, tmpl.hash), 201)
	m13Code(t, "activate R1", e.httpActivate(e.dave.token, tmpl.hash, "", 0), 200)
	x, _ := e.build("500", m13Relabel("Order X"), e.carol)
	y, _ := e.build("500", m13Relabel("Order Y"), e.carol)
	for _, h := range []string{x, y} {
		m13Code(t, "approve on R1", e.httpApprove(e.dave.token, h), 201)
	}
	m13Code(t, "activate Y", e.httpActivate(e.dave.token, y, tmpl.hash, 1), 200)

	// Base moved between approve and activate (and before a second approval).
	before := e.state()
	m13Reasons(t, "approve X after the base moved", m13Code(t, "approve stale X", e.httpApprove(e.erin.token, x), 409, "candidate_stale"), "BASE_CHANGED")
	m13Reasons(t, "activate X on its bound base", m13Code(t, "activate stale X", e.httpActivate(e.dave.token, x, tmpl.hash, 1), 409, "candidate_stale"), "BASE_CHANGED")
	m13Reasons(t, "activate X on the live pointer", m13Code(t, "activate stale X on Y", e.httpActivate(e.dave.token, x, y, 2), 409, "candidate_stale"), "BASE_CHANGED")
	_, err := e.approvePort(e.erin.subject, x)
	m13Refused(t, "port approve of a stale candidate", err, "55000")
	for _, c := range []struct {
		base string
		gen  int64
	}{{tmpl.hash, 1}, {y, 2}} {
		_, err = e.activatePort(e.dave.subject, x, c.base, c.gen)
		m13Refused(t, "port activate of a stale candidate", err, "55000")
	}
	e.unchanged("stale base", before)
	if r := m13Decode(e.httpCandidate(e.viewer, x)); r.Stale == nil || !*r.Stale {
		t.Errorf("X must read as stale: %+v", r)
	}

	// A stored role head changes between approve and activate.
	z, _ := e.build("500", m13Relabel("Order Z"), e.carol)
	w, _ := e.build("500", m13Relabel("Order W"), e.carol)
	m13Code(t, "approve Z", e.httpApprove(e.dave.token, z), 201)
	e.publishRole("auditor", 1, "candidates.read", "releases.read")
	before = e.state()
	m13Reasons(t, "activate Z after a role change", m13Code(t, "activate Z", e.httpActivate(e.dave.token, z, y, 2), 409, "candidate_stale"), "POLICY_CHANGED")
	m13Reasons(t, "approve Z after a role change", m13Code(t, "approve Z", e.httpApprove(e.erin.token, z), 409, "candidate_stale"), "POLICY_CHANGED")
	m13Reasons(t, "approve W after a role change", m13Code(t, "approve W", e.httpApprove(e.dave.token, w), 409, "candidate_stale"), "POLICY_CHANGED")
	e.unchanged("stale policy", before)

	// RESIDUAL: the database cannot recompute the policy bundle hash, so a
	// process that skips Go staleness activates Z through the port.
	if g, err := e.activatePort(e.dave.subject, z, y, 2); err != nil || g != 3 {
		t.Fatalf("RESIDUAL changed: the port refused a policy-stale activation (%d, %v)", g, err)
	}
	t.Log("RESIDUAL: activate_release accepted a POLICY_CHANGED candidate; only Go staleness refuses it")
	// Both reasons at once, sorted.
	m13Reasons(t, "W after base and policy moved", m13Code(t, "activate W", e.httpActivate(e.dave.token, w, y, 2), 409, "candidate_stale"), "BASE_CHANGED", "POLICY_CHANGED")
	e.checkHistory()
}

// ------------------------------------------------- 6. rejection permanence

// A rejection is permanent and bound to the exact hash: approve and activate
// refuse it, an identical rebuild yields the same (still rejected) hash, and
// only a changed input (here a requirement) yields a new hash that proceeds.
func TestM13AdversarialRejectionIsPermanent(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	before := e.state()
	// Malformed rejections are refused at the edge and by the port.
	for name, body := range map[string]map[string]any{
		"unknown reason":    {"artifact_hash": tmpl.hash, "reason": "BECAUSE", "note": ""},
		"note too long":     {"artifact_hash": tmpl.hash, "reason": "OTHER", "note": strings.Repeat("n", 281)},
		"control character": {"artifact_hash": tmpl.hash, "reason": "OTHER", "note": "bell\a"},
		"bidi override":     {"artifact_hash": tmpl.hash, "reason": "OTHER", "note": "abc\u202edef"},
		"actor claimed":     {"artifact_hash": tmpl.hash, "reason": "OTHER", "note": "", "actor_id": e.dave.id},
		"another hash":      {"artifact_hash": strings.Repeat("f", 64), "reason": "OTHER", "note": ""},
	} {
		m13Code(t, "reject with "+name, e.post("/candidates/"+tmpl.hash+"/reject", e.bob, body), 400)
	}
	for name, c := range map[string][2]string{"unknown reason": {"BECAUSE", ""}, "note too long": {"OTHER", strings.Repeat("n", 281)}, "control character": {"OTHER", "a\x01b"}, "line separator": {"OTHER", "a\u2028b"}} {
		_, err := e.rejectPort(e.f.bobSubject, tmpl.hash, c[0], c[1])
		m13Refused(t, "port reject with "+name, err, "22023")
	}
	_, err := e.rejectPort(e.f.viewerSubject, tmpl.hash, "OTHER", "")
	m13Refused(t, "viewer rejects", err, "42501")
	e.unchanged("malformed rejections", before)

	// The acceptance author vetoes (no separation applies to a veto).
	if r := m13Code(t, "acceptance author rejects", e.httpReject(e.bob, tmpl.hash, "REQUIREMENTS_NOT_MET", "threshold disputed"), 201); r.Status != "REJECTED" || len(r.Rejections) != 1 {
		t.Errorf("rejected view: %+v", r)
	}
	m13Code(t, "approve a rejected hash", e.httpApprove(e.dave.token, tmpl.hash), 409, "candidate_rejected")
	m13Code(t, "activate a rejected hash", e.httpActivate(e.dave.token, tmpl.hash, "", 0), 409, "candidate_rejected")
	_, err = e.approvePort(e.dave.subject, tmpl.hash)
	m13Refused(t, "port approve of a rejected hash", err, "55000")
	_, err = e.activatePort(e.dave.subject, tmpl.hash, "", 0)
	m13Refused(t, "port activate of a rejected hash", err, "55000")
	if a := e.approvalsOf(tmpl.hash); len(a) != 0 {
		t.Fatalf("a rejected hash got approvals %v", a)
	}
	// An identical rebuild is the same artifact and stays rejected.
	again := e.candidate(e.carol.token, tmpl.art)
	if r := m13Decode(again); again.Code != 200 || r.ArtifactHash != tmpl.hash || r.Status != "REJECTED" {
		t.Fatalf("identical rebuild: %d %s", again.Code, m13Trim(again.Body.String()))
	}
	m13Code(t, "approve the identical rebuild", e.httpApprove(e.dave.token, tmpl.hash), 409, "candidate_rejected")

	// A changed requirement yields a new hash that can proceed.
	set := thresholdSet(e.app, "500")
	semantics, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: "500",
		ApproverRoles: []string{"admin"}, Context: map[string]json.RawMessage{"field.order.note": json.RawMessage(`"urgent"`)}})
	set.Requirements[0].Semantics = semantics
	_, head := e.heads()
	wr := e.requirements(e.alice, set, head)
	var stored promotion.StoredRequirements
	if wr.Code != 201 || json.Unmarshal(wr.Body.Bytes(), &stored) != nil {
		t.Fatal("changed requirement", wr.Code, wr.Body.String())
	}
	wa := e.acceptance(e.bob, stored.Version, acceptanceFor(set, "500.01"))
	var spec promotion.StoredAcceptance
	if wa.Code != 201 || json.Unmarshal(wa.Body.Bytes(), &spec) != nil {
		t.Fatal("acceptance for the changed requirement", wa.Code, wa.Body.String())
	}
	art := tmpl.art
	art.requirementsVersion, art.acceptanceID = stored.Version, spec.ID
	wc := e.candidate(e.carol.token, art)
	fresh := m13Decode(wc)
	if wc.Code != 201 || fresh.ArtifactHash == tmpl.hash || fresh.Status != "STAGED" {
		t.Fatalf("rebuild with a changed requirement: %d %s", wc.Code, m13Trim(wc.Body.String()))
	}
	m13Code(t, "approve the new hash", e.httpApprove(e.dave.token, fresh.ArtifactHash), 201)
	m13Code(t, "activate the new hash", e.httpActivate(e.dave.token, fresh.ArtifactHash, "", 0), 200)
	if a := e.approvalsOf(tmpl.hash); len(a) != 0 {
		t.Fatalf("the rejected hash gained approvals %v", a)
	}
	e.checkHistory()
}

// ------------------------------------------------------- 7. rollback abuse

func (e *m13Env) rollbackBody(token string, body map[string]any) *httptest.ResponseRecorder {
	return e.post("/releases/rollback", token, body)
}

// Rollback targets only a previously activated release, compares-and-sets the
// pointer, and gates on the rollback class stored in the CURRENT release's body
// (never on a request field): POINTER_SAFE moves freely, anything else needs an
// acknowledged plan, and a plan that requires recovery is refused.
func TestM13AdversarialRollbackAbuse(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	ivan := e.member("ivan", "member")
	r1 := e.template().hash // first release: ADDITIVE, DATA_NOT_REVERSIBLE
	m13Code(t, "approve R1", e.httpApprove(e.dave.token, r1), 201)
	m13Code(t, "activate R1", e.httpActivate(e.dave.token, r1, "", 0), 200)
	m13Code(t, "roll back to the only release", e.httpRollback(e.erin.token, r1, r1, 1, nil), 409, "rollback_target_is_current")
	r2, _ := e.build("500", m13Relabel("Order 2"), e.carol) // EQUAL, POINTER_SAFE
	m13Code(t, "approve R2", e.httpApprove(e.dave.token, r2), 201)
	if r := m13Code(t, "activate R2", e.httpActivate(e.dave.token, r2, r1, 1), 200); r.Generation != 2 {
		t.Fatalf("R2 generation %d", r.Generation)
	}
	never, _ := e.build("500", m13Relabel("Order never"), e.carol)
	m13Code(t, "approve the never-activated candidate", e.httpApprove(e.dave.token, never), 201)
	plan := strings.Repeat("c", 64)
	unknown := strings.Repeat("d", 64)

	before := e.state()
	for _, c := range []struct {
		name, target, current string
		generation            int64
		status                int
		code, state           string
	}{
		{"approved but never activated target", never, r2, 2, 409, "rollback_target_not_released", "55000"},
		{"target is the current release", r2, r2, 2, 409, "rollback_target_is_current", "55000"},
		{"wrong expected current", r1, r1, 2, 409, "release_changed", "55000"},
		{"wrong expected generation", r1, r2, 1, 409, "release_changed", "55000"},
		{"unknown target", unknown, r2, 2, 404, "not_found", "55000"},
	} {
		m13Code(t, "HTTP rollback: "+c.name, e.httpRollback(e.erin.token, c.target, c.current, c.generation, m13SafePlan()), c.status, c.code)
		_, err := e.rollbackPort(e.erin.subject, c.target, c.current, c.generation, plan, true)
		m13Refused(t, "port rollback: "+c.name, err, c.state)
	}
	for name, body := range map[string]map[string]any{
		"malformed target":       {"target": "xyz", "expected_current": r2, "expected_generation": 2},
		"generation zero":        {"target": r1, "expected_current": r2, "expected_generation": 0},
		"missing generation":     {"target": r1, "expected_current": r2},
		"malformed plan hash":    {"target": r1, "expected_current": r2, "expected_generation": 2, "plan": map[string]any{"storage_compatible": true, "external_effects_reviewed": true, "recovery_required": false, "plan_hash": "xyz"}},
		"class claimed":          {"target": r1, "expected_current": r2, "expected_generation": 2, "rollback": "POINTER_SAFE"},
		"class claimed in plan":  {"target": r1, "expected_current": r2, "expected_generation": 2, "plan": map[string]any{"storage_compatible": true, "external_effects_reviewed": true, "recovery_required": false, "plan_hash": plan, "class": "POINTER_SAFE"}},
		"next generation chosen": {"target": r1, "expected_current": r2, "expected_generation": 2, "generation": 7},
	} {
		m13Code(t, "rollback with "+name, e.rollbackBody(e.erin.token, body), 400)
	}
	_, err := e.rollbackPort(e.erin.subject, r1, r2, 2, "xyz", true)
	m13Refused(t, "port rollback with a malformed plan hash", err, "22023")
	_, err = e.rollbackPort(e.erin.subject, r1, r2, 0, plan, true)
	m13Refused(t, "port rollback with generation zero", err, "22023")
	for _, rogue := range []m13Actor{e.person("viewer"), ivan} {
		m13Code(t, "rollback by "+rogue.name, e.httpRollback(rogue.token, r1, r2, 2, m13SafePlan()), 403, "forbidden")
	}
	_, err = e.rollbackPort(e.f.viewerSubject, r1, r2, 2, plan, true)
	m13Refused(t, "port rollback by a viewer", err, "42501")
	e.unchanged("refused rollbacks", before)

	// R2 is POINTER_SAFE: no plan needed.
	if r := m13Code(t, "pointer-safe rollback", e.httpRollback(e.erin.token, r1, r2, 2, nil), 200); r.ArtifactHash != r1 || r.Generation != 3 {
		t.Fatalf("rollback reply %+v", r)
	}
	rows := e.checkHistory()
	if last := rows[len(rows)-1]; last.kind != "ROLLBACK" || last.hash != r1 || last.previous != r2 || last.riskLevel != "HIGH" || last.storageClass != "ADDITIVE" || last.planHash != "" || last.actor != e.erin.id {
		t.Fatalf("rollback history row %+v", last)
	}
	// R1 is DATA_NOT_REVERSIBLE: the class comes from its stored body.
	before = e.state()
	m13Code(t, "rollback without a plan", e.httpRollback(e.erin.token, r2, r1, 3, nil), 409, "rollback_plan_required")
	m13Code(t, "rollback with a recovery plan", e.httpRollback(e.erin.token, r2, r1, 3, &m13Plan{StorageCompatible: true, ExternalEffectsReviewed: true, RecoveryRequired: true, PlanHash: plan}), 409, "recovery_required")
	m13Code(t, "rollback with an unacknowledged plan", e.httpRollback(e.erin.token, r2, r1, 3, &m13Plan{ExternalEffectsReviewed: true, PlanHash: plan}), 409, "rollback_plan_required")
	m13Code(t, "rollback claiming POINTER_SAFE", e.rollbackBody(e.erin.token, map[string]any{"target": r2, "expected_current": r1, "expected_generation": 3, "rollback": "POINTER_SAFE"}), 400)
	_, err = e.rollbackPort(e.erin.subject, r2, r1, 3, plan, false)
	m13Refused(t, "port rollback without acknowledgement", err, "55000")
	_, err = e.rollbackPort(e.erin.subject, r2, r1, 3, "", true)
	m13Refused(t, "port rollback acknowledged without a plan hash", err, "55000")
	e.unchanged("plan-gated rollbacks", before)
	if r := m13Code(t, "planned rollback", e.httpRollback(e.erin.token, r2, r1, 3, m13SafePlan()), 200); r.Generation != 4 {
		t.Fatalf("planned rollback %+v", r)
	}
	rows = e.checkHistory()
	if last := rows[len(rows)-1]; last.kind != "ROLLBACK" || last.hash != r2 || last.planHash != plan || last.riskLevel != "LOW" || last.storageClass != "EQUAL" {
		t.Fatalf("planned rollback history row %+v", last)
	}
	// Replay of the same rollback: the pointer moved, nothing changes.
	before = e.state()
	m13Code(t, "replayed rollback", e.httpRollback(e.erin.token, r2, r1, 3, m13SafePlan()), 409, "release_changed")
	_, err = e.rollbackPort(e.erin.subject, r2, r1, 3, plan, true)
	m13Refused(t, "replayed port rollback", err, "55000")
	e.unchanged("replayed rollback", before)
	// The previous base is still a candidate built on an older world: stale.
	m13Code(t, "activate a candidate built on a replaced pointer", e.httpActivate(e.dave.token, never, r2, 2), 409, "candidate_stale")

	// A release that was rejected after it ran is no longer a rollback target
	// (a rejection is a permanent veto bound to the hash), through the route and the port.
	m13Code(t, "reject the previous release", e.httpReject(e.dave.token, r1, "RISK_UNACCEPTABLE", "withdrawn after activation"), 201)
	before = e.state()
	m13Code(t, "rollback to a rejected release", e.httpRollback(e.erin.token, r1, r2, 4, nil), 409, "rollback_target_rejected")
	_, err = e.rollbackPort(e.erin.subject, r1, r2, 4, "", false)
	m13Refused(t, "port rollback to a rejected release", err, "55000")
	e.unchanged("rollback to a rejected release", before)
	e.checkHistory()
}

// ------------------------------------------------- 8. cross-tenant / -app

// m13Routes calls every ADR103 route under root for hash and returns the replies.
func (e *m13Env) m13Routes(root, token, hash string) map[string]*httptest.ResponseRecorder {
	return map[string]*httptest.ResponseRecorder{
		"approve":  e.postAt(root, "/candidates/"+hash+"/approve", token, map[string]any{"artifact_hash": hash}),
		"reject":   e.postAt(root, "/candidates/"+hash+"/reject", token, map[string]any{"artifact_hash": hash, "reason": "OTHER", "note": ""}),
		"activate": e.postAt(root, "/candidates/"+hash+"/activate", token, map[string]any{"artifact_hash": hash, "expected_base": "", "expected_generation": 0}),
		"rollback": e.postAt(root, "/releases/rollback", token, map[string]any{"target": hash, "expected_current": hash, "expected_generation": 1}),
		"detail":   e.getAt(root, "/candidates/"+hash, token),
		"history":  e.getAt(root, "/releases", token),
		"active":   e.getAt(root, "/releases/active", token),
	}
}

// Another tenant's member, a two-tenant member on the other tenant's URL and
// the right tenant under another application learn nothing: every route answers
// exactly as for a hash that does not exist, and the ports find nothing.
func TestM13AdversarialCrossTenantAndCrossApplication(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	m13Code(t, "approve", e.httpApprove(e.dave.token, tmpl.hash), 201)
	m13Code(t, "activate", e.httpActivate(e.dave.token, tmpl.hash, "", 0), 200)
	olga := e.addMemberIn(e.f.tenantB, "olga", "admin")
	random := strings.Repeat("d", 64)
	rootB := "/v1/tenants/" + e.f.tenantB + "/preview/" + e.app
	rootOther := "/v1/tenants/" + e.f.tenantA + "/preview/app.other_" + strings.ReplaceAll(uuid(t), "-", "")
	before := e.state()
	secrets := []string{e.carol.id, e.f.bob, e.f.alice, e.dave.id, `"risk_level"`, `"requester_id"`}
	for name, c := range map[string]struct{ root, token string }{
		"tenant-B member on tenant B":           {rootB, olga.token},
		"two-tenant member on tenant B":         {rootB, e.bob},
		"tenant-A member on another app":        {rootOther, e.dave.token},
		"tenant-A owner on another application": {rootOther, e.alice},
	} {
		real, fake := e.m13Routes(c.root, c.token, tmpl.hash), e.m13Routes(c.root, c.token, random)
		for route, w := range real {
			r, f := m13Decode(w), m13Decode(fake[route])
			if w.Code != fake[route].Code || r.Code != f.Code || r.Message != f.Message || w.Code == 200 && route != "history" {
				t.Errorf("%s %s: real hash %d %s, unknown hash %d %s", name, route, w.Code, m13Trim(w.Body.String()), fake[route].Code, m13Trim(fake[route].Body.String()))
			}
			for _, s := range secrets {
				if strings.Contains(w.Body.String(), s) {
					t.Errorf("%s %s leaks %s: %s", name, route, s, m13Trim(w.Body.String()))
				}
			}
		}
	}
	// A tenant-B member on tenant A's URL is refused before any lookup.
	for route, w := range e.m13Routes(e.root, olga.token, tmpl.hash) {
		m13Code(t, "tenant-B member on tenant A: "+route, w, 403, "forbidden")
	}
	// Ports: nothing of tenant A exists in tenant B's scope, and a compromised
	// API that binds a tenant-B user to tenant A's scope is not a member there.
	for name, c := range map[string]struct {
		subject, tenant, state string
	}{
		"tenant-B member in tenant B":     {olga.subject, e.f.tenantB, "P0002"},
		"two-tenant member in tenant B":   {e.f.bobSubject, e.f.tenantB, "P0002"},
		"tenant-B member bound to tenant": {olga.subject, e.f.tenantA, "42501"},
	} {
		var n int
		var ok bool
		var g int64
		m13Refused(t, name+": approve", e.portIn(c.subject, c.tenant, &n, `SELECT control.approve_release($1,$2)`, e.app, tmpl.hash), c.state)
		m13Refused(t, name+": reject", e.portIn(c.subject, c.tenant, &ok, `SELECT control.reject_release($1,$2,'OTHER','')`, e.app, tmpl.hash), c.state)
		m13Refused(t, name+": activate", e.portIn(c.subject, c.tenant, &g, `SELECT control.activate_release($1,$2,'',0)`, e.app, tmpl.hash), c.state)
		rollbackState := c.state
		if rollbackState == "P0002" {
			rollbackState = "55000" // no pointer in that scope
		}
		m13Refused(t, name+": rollback", e.portIn(c.subject, c.tenant, &g, `SELECT control.rollback_release($1,$2,$3,1,'',false)`, e.app, tmpl.hash, tmpl.hash), rollbackState)
	}
	var n int
	m13Refused(t, "tenant-A hash under another application", e.portIn(e.dave.subject, e.f.tenantA, &n, `SELECT control.approve_release($1,$2)`, "app.other_x", tmpl.hash), "P0002")
	e.unchanged("cross-tenant and cross-application requests", before)
}

// RESIDUAL (not listed in ADR103 Consequences): the ports take the actor from
// identity GUCs that the API process sets (AGENTS.md: GUCs are not
// credentials). A compromised API process that has seen another member's
// issuer/subject binds them and approves and activates the requester's HIGH
// candidate "as" that member, with no token of theirs.
func TestM13AdversarialCompromisedAPIBindsAnotherMembersIdentity(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template() // requested by carol
	// The process serves only carol's request but binds dave's identity GUCs.
	if n, err := e.approvePort(e.dave.subject, tmpl.hash); err != nil || n != 1 {
		t.Fatalf("RESIDUAL changed: impersonated approval refused (%d, %v)", n, err)
	}
	if g, err := e.activatePort(e.dave.subject, tmpl.hash, "", 0); err != nil || g != 1 {
		t.Fatalf("RESIDUAL changed: impersonated activation refused (%d, %v)", g, err)
	}
	h := e.checkHistory()
	if len(h) != 1 || h[0].actor != e.dave.id {
		t.Fatalf("history %+v", h)
	}
	t.Log("RESIDUAL: a process binding another active member's identity GUCs approved and activated a HIGH candidate; the database floors bind honest identity binding only")
}

// ---------------------------------------------- 9. replay and idempotency

// Approve and reject are idempotent per actor; a replayed activation or
// rollback after success is a 409 and leaves the pointer alone.
func TestM13AdversarialReplayAndIdempotency(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	first := m13Code(t, "approve", e.httpApprove(e.dave.token, tmpl.hash), 201)
	second := m13Code(t, "approve again", e.httpApprove(e.dave.token, tmpl.hash), 200)
	if len(first.Approvals) != 1 || len(second.Approvals) != 1 {
		t.Errorf("approval views: %d then %d approvals", len(first.Approvals), len(second.Approvals))
	}
	for i := 0; i < 2; i++ {
		if n, err := e.approvePort(e.dave.subject, tmpl.hash); err != nil || n != 1 {
			t.Errorf("port re-approval %d: %d %v", i, n, err)
		}
	}
	if a := e.approvalsOf(tmpl.hash); len(a) != 1 {
		t.Fatalf("approvals %v", a)
	}
	m13Code(t, "activate", e.httpActivate(e.dave.token, tmpl.hash, "", 0), 200)
	before := e.state()
	m13Reasons(t, "replayed activation", m13Code(t, "replay activation", e.httpActivate(e.dave.token, tmpl.hash, "", 0), 409, "candidate_stale"), "BASE_CHANGED", "SCHEMA_CHANGED")
	m13Code(t, "re-apply the active release on itself", e.httpActivate(e.dave.token, tmpl.hash, tmpl.hash, 1), 409, "candidate_stale")
	for _, c := range []struct {
		base string
		gen  int64
	}{{"", 0}, {tmpl.hash, 1}} {
		_, err := e.activatePort(e.dave.subject, tmpl.hash, c.base, c.gen)
		m13Refused(t, "replayed port activation", err, "55000")
	}
	e.unchanged("replayed activation", before)

	y, _ := e.build("500", m13Relabel("Order Y"), e.carol)
	m13Code(t, "reject", e.httpReject(e.erin.token, y, "SUPERSEDED", "replaced"), 201)
	m13Code(t, "reject again", e.httpReject(e.erin.token, y, "WITHDRAWN", "second thoughts"), 200)
	if created, err := e.rejectPort(e.erin.subject, y, "OTHER", ""); err != nil || created {
		t.Errorf("port re-rejection: %v %v", created, err)
	}
	if created, err := e.rejectPort(e.dave.subject, y, "OTHER", ""); err != nil || !created {
		t.Errorf("a second actor's rejection: %v %v", created, err)
	}
	if r := e.rejectionsOf(y); len(r) != 2 {
		t.Fatalf("rejections %v", r)
	}
	var reason string
	if err := e.f.operator.QueryRow(`SELECT reason FROM control.release_rejections WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3 AND actor_id=$4`, e.f.tenantA, e.app, y, e.erin.id).Scan(&reason); err != nil || reason != "SUPERSEDED" {
		t.Errorf("a replayed rejection rewrote the first reason: %q %v", reason, err)
	}
	if e.auditCount("release.approval_replayed") < 1 || e.auditCount("release.rejection_replayed") < 1 {
		t.Error("replays must be audited as replays")
	}
}

// ----------------------------------- 10. replacement end-to-end (HTTP only)

// Replaces the deleted M6R-store flow of TestM12ApprovalActivationStaleness
// AndRollbackOnBuilderCandidates through the ADR103 routes only: build, approve,
// activate, a second release on the active base, rollback, and staleness of a
// candidate built on the replaced base.
func TestM13AdversarialEndToEndReleaseFlowOverHTTP(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	r1, _ := e.build("500", nil, e.carol)
	if r := m13Code(t, "R1 detail", e.httpCandidate(e.viewer, r1), 200); r.Status != "STAGED" || r.ApprovalsRequired != 1 || r.Stale == nil || *r.Stale || r.RiskLevel != "HIGH" {
		t.Fatalf("R1 %+v", r)
	}
	m13Code(t, "no active release yet", e.httpActive(e.viewer), 404, "not_found")
	m13Code(t, "requester cannot approve R1", e.httpApprove(e.carol.token, r1), 403, "separation_of_duties")
	if r := m13Code(t, "approve R1", e.httpApprove(e.dave.token, r1), 201); r.Status != "APPROVED" {
		t.Fatalf("approved R1 %+v", r)
	}
	if r := m13Code(t, "activate R1", e.httpActivate(e.dave.token, r1, "", 0), 200); r.ArtifactHash != r1 || r.Generation != 1 {
		t.Fatalf("activated R1 %+v", r)
	}
	if r := m13Code(t, "active R1", e.httpActive(e.viewer), 200); r.ArtifactHash != r1 || r.Generation != 1 {
		t.Fatalf("active %+v", r)
	}
	// Release 2 evolves from the active release (threshold change: HIGH, EQUAL).
	r2, _ := e.build("600", nil, e.carol)
	var d2 struct {
		Base    string `json:"base_release"`
		Gen     int64  `json:"base_generation"`
		Storage string `json:"storage_class"`
		High    bool   `json:"high_risk"`
	}
	if w := e.httpCandidate(e.viewer, r2); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d2) != nil || d2.Base != r1 || d2.Gen != 1 || d2.Storage != "EQUAL" || !d2.High {
		t.Fatalf("R2 %d %s", w.Code, m13Trim(w.Body.String()))
	}
	m13Code(t, "approve R2", e.httpApprove(e.erin.token, r2), 201)
	m13Code(t, "activate R2 on a wrong base", e.httpActivate(e.erin.token, r2, "", 0), 409, "base_changed")
	if r := m13Code(t, "activate R2", e.httpActivate(e.erin.token, r2, r1, 1), 200); r.Generation != 2 {
		t.Fatalf("activated R2 %+v", r)
	}
	if r := m13Decode(e.httpCandidate(e.viewer, r1)); r.Status != "SUPERSEDED" {
		t.Errorf("R1 after R2: %s", r.Status)
	}
	// A third candidate is built on R2; the rollback to R1 makes it stale.
	r3, _ := e.build("500", m13Relabel("Order three"), e.carol)
	m13Code(t, "approve R3", e.httpApprove(e.dave.token, r3), 201)
	if r := m13Code(t, "roll back to R1", e.httpRollback(e.erin.token, r1, r2, 2, m13SafePlan()), 200); r.ArtifactHash != r1 || r.Generation != 3 {
		t.Fatalf("rollback %+v", r)
	}
	if r := m13Code(t, "R3 after the rollback", e.httpCandidate(e.viewer, r3), 200); r.Stale == nil || !*r.Stale || !slices.Contains(r.Reasons, "BASE_CHANGED") {
		t.Fatalf("R3 must be stale after the rollback: %+v", r)
	}
	m13Code(t, "activate R3 after the rollback", e.httpActivate(e.dave.token, r3, r2, 2), 409, "candidate_stale")
	for hash, want := range map[string]string{r1: "ACTIVE", r2: "SUPERSEDED", r3: "APPROVED"} {
		if r := m13Decode(e.httpCandidate(e.viewer, hash)); r.Status != want {
			t.Errorf("status of %s: %s, want %s", hash[:8], r.Status, want)
		}
	}
	var history struct {
		History []struct {
			Seq        int64  `json:"seq"`
			Kind       string `json:"kind"`
			Hash       string `json:"artifact_hash"`
			Generation int64  `json:"generation"`
		} `json:"history"`
	}
	if w := e.httpHistory(e.viewer); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &history) != nil || len(history.History) != 3 {
		t.Fatalf("history %d %s", w.Code, m13Trim(w.Body.String()))
	}
	for i, want := range []struct {
		kind, hash string
		gen        int64
	}{{"ROLLBACK", r1, 3}, {"ACTIVATE", r2, 2}, {"ACTIVATE", r1, 1}} {
		if h := history.History[i]; h.Kind != want.kind || h.Hash != want.hash || h.Generation != want.gen || h.Seq != want.gen {
			t.Errorf("history[%d] = %+v, want %+v (newest first)", i, h, want)
		}
	}
	e.checkHistory()
	for action, want := range map[string]int{"release.approved": 3, "release.activated": 2, "release.rolled_back": 1} {
		if n := e.auditCount(action); n != want {
			t.Errorf("audit %s: %d, want %d", action, n, want)
		}
	}
}
