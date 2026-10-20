//go:build integration

package integration

// Regression tests of the independent M13 review (docs/verification/M13_INDEPENDENT_REVIEW.md, Appendix A).
// Each test failed on e6b7877 while its defect existed.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// m13rHold runs query as subject in tenant A: it takes the REPEATABLE READ snapshot,
// reports "ready", waits for "proceed", runs the query and commits.
func (e *m13Env) m13rHold(subject string, ready chan<- struct{}, proceed <-chan struct{}, query string, args ...any) error {
	return postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, subject, e.f.tenantA); err != nil {
			return err
		}
		ready <- struct{}{}
		<-proceed
		var dst any
		return q.QueryRowContext(ctx, query, args...).Scan(&dst)
	})
}

// M13R-01: a rejection that commits after an activation transaction took its snapshot, and
// before that transaction calls the port, does not stop the activation.
func TestM13R01RejectedHashActivatedByStaleSnapshot(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	y, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.approvePort(e.dave.subject, y); err != nil {
		t.Fatal(err)
	}
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.m13rHold(e.erin.subject, ready, proceed, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, y, "", int64(0))
	}()
	<-ready // the activation transaction exists (its snapshot predates the rejection)
	if _, err = e.rejectPort(e.f.bobSubject, y, "RISK_UNACCEPTABLE", "veto"); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	aerr := <-done
	hash, _, _ := e.activePointer()
	if aerr == nil && hash == y {
		t.Errorf("M13R-01: the release pointer moved onto a hash whose rejection had committed before the activation committed (rejections=%d)", len(e.rejectionsOf(y)))
	}
	m13Refused(t, "activation overtaken by a committed rejection", aerr, "40001")
	if hash == y {
		t.Errorf("the rejected hash is active")
	}
}

// M13R-01 control: a rejection of ANOTHER hash does not abort an unrelated activation (the fence is per hash).
func TestM13R01RejectionOfAnotherHashDoesNotBlockTheActivation(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	y, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.approvePort(e.dave.subject, y); err != nil {
		t.Fatal(err)
	}
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.m13rHold(e.erin.subject, ready, proceed, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, y, "", int64(0))
	}()
	<-ready
	if _, err = e.rejectPort(e.f.bobSubject, other, "RISK_UNACCEPTABLE", "veto of the other candidate"); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if err = <-done; err != nil {
		t.Fatalf("an unrelated rejection aborted the activation: %v", err)
	}
	if hash, _, _ := e.activePointer(); hash != y {
		t.Errorf("the pointer is %s, want the activated candidate", hash)
	}
}

// M13R-01: two transactions that both start before either writes the fence still serialise: the second
// writer waits and then fails with 40001 (activation first, rejection second).
func TestM13R01RejectionAfterTheActivationCommittedStillSucceedsOnRetry(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	y, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.approvePort(e.dave.subject, y); err != nil {
		t.Fatal(err)
	}
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.m13rHold(e.f.bobSubject, ready, proceed, `SELECT control.reject_release($1,$2,$3,$4)`, e.app, y, "RISK_UNACCEPTABLE", "late veto")
	}()
	<-ready // the rejection transaction has its snapshot; the activation commits first
	if _, err = e.activatePort(e.erin.subject, y, "", 0); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	m13Refused(t, "rejection overtaken by a committed activation", <-done, "40001")
	// A retry sees the activation and the veto is recorded: a rejected release can still be vetoed after it ran.
	if _, err = e.rejectPort(e.f.bobSubject, y, "RISK_UNACCEPTABLE", "late veto"); err != nil {
		t.Fatalf("retried rejection: %v", err)
	}
}

// M13R-01 over plain HTTP: reject and activate requests that overlap by a few hundred
// milliseconds. The rejection is acknowledged (201) first and the activation still succeeds.
func TestM13R01bHTTPOverlap(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	overtaken := 0
	for k, delay := range []time.Duration{40, 80, 120, 160} {
		base, gen, _ := e.activePointer()
		c, _ := e.build("500", m13Relabel(fmt.Sprintf("Race %d", k)), e.carol)
		m13Code(t, "approve", e.httpApprove(e.dave.token, c), 201)
		var wg sync.WaitGroup
		var rCode, aCode int
		wg.Add(2)
		go func() { defer wg.Done(); rCode = e.httpReject(e.bob, c, "RISK_UNACCEPTABLE", "veto").Code }()
		time.Sleep(delay * time.Millisecond)
		go func() { defer wg.Done(); aCode = e.httpActivate(e.erin.token, c, base, gen).Code }()
		wg.Wait()
		if hash, _, _ := e.activePointer(); rCode == 201 && aCode == 200 && hash == c {
			overtaken++
		}
	}
	if overtaken > 0 {
		t.Errorf("M13R-01b: %d of 4 overlapping reject/activate pairs ended with the rejected hash active", overtaken)
	}
}

// M13R-02: the rollback class gate reads only the CURRENT release, so a rollback that crosses an
// earlier DATA_NOT_REVERSIBLE release needs no acknowledged plan.
func TestM13R02RollbackAcrossIrreversibleRelease(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	required := func(on bool) func(*metadata.Document) {
		return func(d *metadata.Document) {
			for i := range d.Definitions {
				if d.Definitions[i].ID == "field.order.note" && d.Definitions[i].Field != nil {
					d.Definitions[i].Field.Required = on
				}
			}
		}
	}
	c1, _ := e.build("500", required(true), e.carol)
	m13Code(t, "approve c1", e.httpApprove(e.dave.token, c1), 201)
	m13Code(t, "activate c1", e.httpActivate(e.erin.token, c1, "", 0), 200)
	c2 := m13apiBuildMigration(t, e, required(false), e.carol) // CRITICAL, TRANSFORM, DATA_NOT_REVERSIBLE
	m13Code(t, "approve c2 dave", e.httpApprove(e.dave.token, c2), 201)
	m13Code(t, "approve c2 erin", e.httpApprove(e.erin.token, c2), 201)
	m13Code(t, "activate c2", e.httpActivate(e.dave.token, c2, c1, 1), 200)
	c3, _ := e.build("500", func(d *metadata.Document) { required(false)(d); m13Relabel("Hop three")(d) }, e.carol) // label only: POINTER_SAFE
	m13Code(t, "approve c3", e.httpApprove(e.dave.token, c3), 201)
	m13Code(t, "activate c3", e.httpActivate(e.erin.token, c3, c2, 2), 200)
	if w := e.httpRollback(e.erin.token, c1, c3, 3, nil); w.Code == 200 {
		t.Errorf("M13R-02: rollback c3 -> c1 crossed the TRANSFORM release c2 without a rollback plan (HTTP %d)", w.Code)
	}
}

// M13R-02: the same rollback with an acknowledged plan succeeds and the history row carries the plan hash; a
// rollback that crosses only pointer-safe releases still needs no plan.
func TestM13R02RollbackAcrossWithAPlanAndAdjacentHopsWithout(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	required := func(on bool) func(*metadata.Document) {
		return func(d *metadata.Document) {
			for i := range d.Definitions {
				if d.Definitions[i].ID == "field.order.note" && d.Definitions[i].Field != nil {
					d.Definitions[i].Field.Required = on
				}
			}
		}
	}
	c1, _ := e.build("500", required(true), e.carol)
	m13Code(t, "approve c1", e.httpApprove(e.dave.token, c1), 201)
	m13Code(t, "activate c1", e.httpActivate(e.erin.token, c1, "", 0), 200)
	c2 := m13apiBuildMigration(t, e, required(false), e.carol)
	m13Code(t, "approve c2 dave", e.httpApprove(e.dave.token, c2), 201)
	m13Code(t, "approve c2 erin", e.httpApprove(e.erin.token, c2), 201)
	m13Code(t, "activate c2", e.httpActivate(e.dave.token, c2, c1, 1), 200)
	c3, _ := e.build("500", func(d *metadata.Document) { required(false)(d); m13Relabel("Hop three")(d) }, e.carol)
	m13Code(t, "approve c3", e.httpApprove(e.dave.token, c3), 201)
	m13Code(t, "activate c3", e.httpActivate(e.erin.token, c3, c2, 2), 200)
	m13Code(t, "no plan across c2", e.httpRollback(e.erin.token, c1, c3, 3, nil), 409, "rollback_plan_required")
	if _, err := e.rollbackPort(e.erin.subject, c1, c3, 3, "", false); err == nil {
		t.Error("the database let the pointer cross a TRANSFORM release without a plan")
	} else {
		m13Refused(t, "port rollback across c2 without a plan", err, "55000")
	}
	plan := &m13Plan{StorageCompatible: true, ExternalEffectsReviewed: true, PlanHash: hexOf("plan across c2")}
	m13Code(t, "plan across c2", e.httpRollback(e.erin.token, c1, c3, 3, plan), 200)
	// A rollback that crosses only a pointer-safe release (a label-only change) needs no plan, whatever the
	// target's own class is.
	e2 := m13Setup(t)
	m13RequireStack(t, e2, true)
	d1, _ := e2.build("500", required(true), e2.carol)
	m13Code(t, "approve d1", e2.httpApprove(e2.dave.token, d1), 201)
	m13Code(t, "activate d1", e2.httpActivate(e2.erin.token, d1, "", 0), 200)
	d2, _ := e2.build("500", func(d *metadata.Document) { required(true)(d); m13Relabel("Hop two")(d) }, e2.carol)
	m13Code(t, "approve d2", e2.httpApprove(e2.dave.token, d2), 201)
	m13Code(t, "activate d2", e2.httpActivate(e2.erin.token, d2, d1, 1), 200)
	m13Code(t, "pointer-safe hop without a plan", e2.httpRollback(e2.erin.token, d1, d2, 2, nil), 200)
}

// M13R-05 at the database: the port and the table constraint refuse the invisible marks; the zero width joiner stays legal.
func TestM13R05NoteMarksAreRefusedByThePort(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for _, r := range []rune{0x061c, 0x200b, 0x200e, 0x200f, 0xfeff} {
		x, err := e.stageLevel("LOW")
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.rejectPort(e.dave.subject, x, "OTHER", "a"+string(r)+"b")
		m13Refused(t, fmt.Sprintf("note with U+%04X", r), err, "22023")
	}
	x, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.rejectPort(e.dave.subject, x, "OTHER", "family "+string([]rune{0x1f468, 0x200d, 0x1f469})); err != nil {
		t.Errorf("a note with a zero width joiner (emoji sequence) must stay legal: %v", err)
	}
}

// M13R-03: an approval counts at activation only while its approver still holds releases.approve. The database
// floor counts every active non-viewer member; the Go layer now asks the policy about the approver's CURRENT role,
// so a member demoted through the role administration route stops counting, the candidate reads STAGED again and
// restoring the role restores the approval (the record itself is never changed).
func TestM13R03DemotedApproverNoLongerCounts(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	root := "/v1/tenants/" + e.f.tenantA
	c1, _ := e.build("500", nil, e.carol)
	m13Code(t, "approve", e.httpApprove(e.dave.token, c1), 201)
	if st := m13Decode(e.httpCandidate(e.viewer, c1)).Status; st != "APPROVED" {
		t.Fatalf("one approval of a HIGH candidate reads %q, want APPROVED", st)
	}
	r5Assign(t, e.h, root, e.alice, e.dave.id, "member", 200)
	m13Code(t, "dave can no longer approve", e.httpApprove(e.dave.token, c1), 403)
	before := e.state()
	m13Code(t, "activation on the approval of a member who lost releases.approve", e.httpActivate(e.erin.token, c1, "", 0), 409, "approval_quorum_not_met")
	e.unchanged("refused activation", before)
	if st := m13Decode(e.httpCandidate(e.viewer, c1)).Status; st != "STAGED" {
		t.Errorf("a candidate whose only approver lost the authority reads %q, want STAGED", st)
	}
	r5Assign(t, e.h, root, e.alice, e.dave.id, "admin", 200)
	if st := m13Decode(e.httpCandidate(e.viewer, c1)).Status; st != "APPROVED" {
		t.Errorf("restoring the role must restore the approval: status %q", st)
	}
	m13Code(t, "activation after the role is restored", e.httpActivate(e.erin.token, c1, "", 0), 200)
}

// M13R-03: a deactivated approver does not count either, and the view agrees with the refusal.
func TestM13R03DeactivatedApproverNoLongerCounts(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c1, _ := e.build("500", nil, e.carol)
	m13Code(t, "approve", e.httpApprove(e.dave.token, c1), 201)
	e.operatorExec("deactivate dave", `UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, e.f.tenantA, e.dave.id)
	m13Code(t, "activation on the approval of a deactivated member", e.httpActivate(e.erin.token, c1, "", 0), 409, "approval_quorum_not_met")
	if st := m13Decode(e.httpCandidate(e.viewer, c1)).Status; st != "STAGED" {
		t.Errorf("a candidate whose only approver was deactivated reads %q, want STAGED", st)
	}
}

// M13R-03: a stored custom role carries the authority through its grants; reassigning the member to a role
// without the grant removes the approval from the count, and the role administration route is the real path.
func TestM13R03ReassignedCustomApproverNoLongerCounts(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.useAPI(func(a *access.API) { a.PolicyLoader = (policy.RoleStore{}).Load })
	// Role heads are part of the candidate's policy bundle: publish them before the candidate is built.
	e.publishRole("release_approver", 0, "releases.read", "releases.approve")
	e.publishRole("release_reader", 0, "releases.read")
	root := "/v1/tenants/" + e.f.tenantA
	frank := e.member("frank", "viewer")
	r5Assign(t, e.h, root, e.alice, frank.id, "release_approver", 200)
	c1, _ := e.build("500", nil, e.carol)
	m13Code(t, "frank approves through the stored role", e.httpApprove(frank.token, c1), 201)
	if st := m13Decode(e.httpCandidate(e.viewer, c1)).Status; st != "APPROVED" {
		t.Fatalf("status %q, want APPROVED", st)
	}
	r5Assign(t, e.h, root, e.alice, frank.id, "release_reader", 200)
	m13Code(t, "frank can no longer approve", e.httpApprove(frank.token, c1), 403)
	m13Code(t, "activation on the approval of a member reassigned to a reader", e.httpActivate(e.erin.token, c1, "", 0), 409, "approval_quorum_not_met")
	if st := m13Decode(e.httpCandidate(e.viewer, c1)).Status; st != "STAGED" {
		t.Errorf("status %q, want STAGED", st)
	}
	r5Assign(t, e.h, root, e.alice, frank.id, "release_approver", 200)
	m13Code(t, "activation once the role is back", e.httpActivate(e.erin.token, c1, "", 0), 200)
}

// M13R-03: a CRITICAL release needs two approvals that BOTH still hold the authority.
func TestM13R03CriticalQuorumNeedsBothApproversToHold(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	root := "/v1/tenants/" + e.f.tenantA
	required := func(on bool) func(*metadata.Document) {
		return func(d *metadata.Document) {
			for i := range d.Definitions {
				if d.Definitions[i].ID == "field.order.note" && d.Definitions[i].Field != nil {
					d.Definitions[i].Field.Required = on
				}
			}
		}
	}
	c1, _ := e.build("500", required(true), e.carol)
	m13Code(t, "approve c1", e.httpApprove(e.dave.token, c1), 201)
	m13Code(t, "activate c1", e.httpActivate(e.erin.token, c1, "", 0), 200)
	c2 := m13apiBuildMigration(t, e, required(false), e.carol) // CRITICAL
	m13Code(t, "approve c2 dave", e.httpApprove(e.dave.token, c2), 201)
	m13Code(t, "approve c2 erin", e.httpApprove(e.erin.token, c2), 201)
	r5Assign(t, e.h, root, e.alice, e.erin.id, "member", 200)
	m13Code(t, "one of two approvers lost the authority", e.httpActivate(e.dave.token, c2, c1, 1), 409, "approval_quorum_not_met")
	if st := m13Decode(e.httpCandidate(e.viewer, c2)).Status; st != "STAGED" {
		t.Errorf("status %q, want STAGED", st)
	}
	r5Assign(t, e.h, root, e.alice, e.erin.id, "admin", 200)
	m13Code(t, "both hold again", e.httpActivate(e.dave.token, c2, c1, 1), 200)
}

// M13R-03: policy.RoleStore.Holds, which activation uses for approvers who are not the caller, answers exactly
// what the real approve route answers for a member of each role (built-in, custom allowed on all applications,
// allowed on another application only, allowed with an explicit DENY on this application, read only).
func TestM13R03HoldsMatchesDecide(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.useAPI(func(a *access.API) { a.PolicyLoader = (policy.RoleStore{}).Load })
	custom := func(name string, grants ...map[string]any) {
		t.Helper()
		def := map[string]any{"schema_version": "tenant-capabilities-v1", "name": name, "grants": grants}
		w := m5Call(e.h, "/v1/tenants/"+e.f.tenantA+"/roles", e.alice, map[string]any{"operation_id": uuid(t), "expected_version": 0, "active": true, "definition": def})
		if w.Code/100 != 2 {
			t.Fatal("publish role", name, w.Code, w.Body.String())
		}
	}
	all := map[string]string{"app_id": "*", "entity_id": "*", "field_id": "*"}
	here := map[string]string{"app_id": e.app, "entity_id": "*", "field_id": "*"}
	other := map[string]string{"app_id": "app.other_app", "entity_id": "*", "field_id": "*"}
	custom("holds_everywhere", map[string]any{"effect": "ALLOW", "actions": []string{"releases.approve"}, "resource": all})
	custom("holds_elsewhere", map[string]any{"effect": "ALLOW", "actions": []string{"releases.approve"}, "resource": other})
	custom("holds_but_denied_here", map[string]any{"effect": "ALLOW", "actions": []string{"releases.approve"}, "resource": all}, map[string]any{"effect": "DENY", "actions": []string{"releases.approve"}, "resource": here})
	custom("reads_only", map[string]any{"effect": "ALLOW", "actions": []string{"releases.read", "candidates.read"}, "resource": all})
	root := "/v1/tenants/" + e.f.tenantA
	roles := []struct {
		role string
		who  m13Actor
		want bool
	}{
		{"owner", e.person("alice"), true},
		{"admin", e.dave, true},
		{"member", e.member("mia", "member"), false},
		{"viewer", e.member("vera", "viewer"), false},
		{"holds_everywhere", e.member("cora", "viewer"), true},
		{"holds_elsewhere", e.member("otto", "viewer"), false},
		{"holds_but_denied_here", e.member("dina", "viewer"), false},
		{"reads_only", e.member("rita", "viewer"), false},
	}
	for _, r := range roles {
		if r.role != "owner" && r.role != "admin" && r.role != "member" && r.role != "viewer" {
			r5Assign(t, e.h, root, e.alice, r.who.id, r.role, 200)
		}
	}
	c1, _ := e.build("500", nil, e.carol) // HIGH: only carol (requester) and bob (acceptance author) are barred from approving
	resource := policy.Resource{TenantID: e.f.tenantA, Type: "preview", AppID: e.app}
	granted := 0
	for _, r := range roles {
		w := e.httpApprove(r.who.token, c1)
		viaRoute := w.Code == 201 || w.Code == 200
		if !viaRoute && (w.Code != 403 || m13Decode(w).Code != "forbidden") {
			t.Fatalf("%s: approve answered %d %s (a policy denial is 403 forbidden)", r.role, w.Code, w.Body.String())
		}
		var viaHolds bool
		if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) (err error) {
			viaHolds, err = (policy.RoleStore{}).Holds(ctx, q, a, r.who.id, r.role, "releases.approve", resource)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if viaRoute != viaHolds || viaRoute != r.want {
			t.Errorf("%s: approve route allows=%v, Holds=%v, expected %v", r.role, viaRoute, viaHolds, r.want)
		}
		if built := r.role == "owner" || r.role == "admin" || r.role == "member" || r.role == "viewer"; built && policy.NativeHolds(e.f.tenantA, r.who.id, r.role, "releases.approve", resource) != r.want {
			t.Errorf("%s: NativeHolds disagrees with the route", r.role)
		}
		if viaHolds {
			granted++
		}
	}
	if granted != 3 {
		t.Fatalf("%d roles hold the authority, want owner, admin and the custom role that is not denied here", granted)
	}
}

// M13R-04: a single body key spelled with U+017F (long s, folds to "s") is accepted as the field.
func TestM13R04KeyAlias(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c1, _ := e.build("500", nil, e.carol)
	body := `{"artifact_ha` + `\` + `u017fh":"` + c1 + `"}` // JSON escape of U+017F inside the key
	if w := m13apiRaw(e.h, e.root+"/candidates/"+c1+"/approve", e.dave.token, "application/json", body); w.Code != 400 {
		t.Errorf("M13R-04: approve with the key artifact_ha<U+017F>h answered %d (a misspelled key must be 400)", w.Code)
	}
}

// M13R-05: bidirectional and invisible formatting marks other than U+202A-202E and U+2066-2069 are stored in a rejection note.
func TestM13R05NoteFormatting(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for _, r := range []rune{0x200E, 0x200F, 0x061C} {
		x, _ := e.build("500", m13Relabel(fmt.Sprintf("Note %X", r)), e.carol)
		if w := e.httpReject(e.dave.token, x, "OTHER", "a"+string(r)+"b"); w.Code == 201 {
			t.Errorf("M13R-05: rejection note with U+%04X was stored", r)
		}
	}
}

// M13R-06: the recorded runtime components are never compared with the running binary.
func TestM13R06RuntimeComponentsUnbound(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash, err := e.stageDoctored(func(b *promotion.BundleV2) {
		b.RuntimeContract.Components.WorkflowDSL = b.RuntimeContract.Components.WorkflowDSL[:len(b.RuntimeContract.Components.WorkflowDSL)-1] // build without DSL 2.2.0
		b.RuntimeContract.Components.SupportedIR = []string{"9.9.9"}
		b.RuntimeContract.Components.ToolCatalog = "tool-catalog-v0"
		raw, _ := json.Marshal(b.RuntimeContract.Components)
		sum := sha256.Sum256(raw)
		b.RuntimeContract.ComponentsHash = hex.EncodeToString(sum[:])
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := e.httpApprove(e.dave.token, hash); w.Code == 201 {
		t.Errorf("M13R-06: a candidate recorded for another runtime component set was approved (HTTP %d)", w.Code)
	}
}

// hexOf is a stable 64-hex stand-in for a plan hash.
func hexOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
