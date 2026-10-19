//go:build integration

package integration

// HTTP-level tests of the ADR039 routes (approve, reject, activate, rollback,
// release history, active release) and of the candidate lifecycle view. They
// reuse the M13 fixture (m13_fixture_test.go) and drive the API only; the
// operator connection reads evidence and changes memberships, never release
// state.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// ------------------------------------------------------------------ helpers

func m13apiRelease(t *testing.T, w *httptest.ResponseRecorder, want int) access.ReleaseView {
	t.Helper()
	if w.Code != want {
		t.Fatal("release response", w.Code, w.Body.String())
	}
	var v access.ReleaseView
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("release contract", w.Body.String())
	}
	return v
}

func m13apiHistory(t *testing.T, w *httptest.ResponseRecorder) []m13apiEntry {
	t.Helper()
	if w.Code != 200 {
		t.Fatal("history response", w.Code, w.Body.String())
	}
	var v struct {
		History []m13apiEntry `json:"history"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.History == nil {
		t.Fatal("history contract", w.Body.String())
	}
	return v.History
}

type m13apiEntry struct {
	Seq          int64  `json:"seq"`
	Kind         string `json:"kind"`
	ArtifactHash string `json:"artifact_hash"`
	PreviousHash string `json:"previous_hash"`
	Generation   int64  `json:"generation"`
	ActorID      string `json:"actor_id"`
	RiskLevel    string `json:"risk_level"`
	StorageClass string `json:"storage_class"`
	PlanHash     string `json:"plan_hash"`
}

// m13apiExpect asserts a status and (when code is not empty) an error code.
func m13apiExpect(t *testing.T, what string, w *httptest.ResponseRecorder, status int, code string) m13Reply {
	t.Helper()
	r := m13Decode(w)
	if w.Code != status || code != "" && r.Code != code {
		t.Errorf("%s: %d %s (want %d %s)", what, w.Code, w.Body.String(), status, code)
	}
	return r
}

// m13apiAudit counts audit rows of e.app for an action and allowed flag.
func (e *m13Env) m13apiAudit(action string, allowed bool) int {
	e.t.Helper()
	var n int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND allowed=$3 AND state->>'metadata_app'=$4`, e.f.tenantA, action, allowed, e.app).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// m13apiAuditState returns the audit state documents of e.app for an action.
func (e *m13Env) m13apiAuditState(action string) []map[string]any {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT state FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND state->>'metadata_app'=$3 ORDER BY occurred_at`, e.f.tenantA, action, e.app)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		var state map[string]any
		if err = rows.Scan(&raw); err != nil || json.Unmarshal(raw, &state) != nil {
			e.t.Fatal("audit state", err)
		}
		out = append(out, state)
	}
	return out
}

// m13apiRaw posts exact bytes with a chosen content type.
func m13apiRaw(h http.Handler, path, token, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func m13apiPublishRole(t *testing.T, e *m13Env, name string, expected int64) {
	t.Helper()
	role := `{"schema_version":"tenant-capabilities-v1","name":"` + name + `","grants":[{"effect":"ALLOW","actions":["candidates.read"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		_, err := (policy.RoleStore{}).Publish(ctx, q, a, uuid(t), expected, []byte(role), true)
		return err
	}); err != nil {
		t.Fatal("publish role", err)
	}
}

// m13apiBuildMigration stages a candidate whose storage evolves (not EQUAL): the
// order document (threshold 500, edited), a requirement set with the threshold
// and a mandatory data_migration requirement allowing TRANSFORM, and an
// acceptance spec by bob covering both. Source author alice, freezer alice.
func m13apiBuildMigration(t *testing.T, e *m13Env, edit func(*metadata.Document), requester m13Actor) string {
	t.Helper()
	revision, requirements := e.heads()
	doc := orderDocument(e.app, "500")
	edit(&doc)
	raw, _ := json.Marshal(doc)
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, revision, raw); w.Code != 201 {
		t.Fatal("register source", w.Code, w.Body.String())
	}
	revision++
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, "500")
	set.Requirements = append(set.Requirements, intent.RequirementV2{ID: "po-migration", Category: intent.CategoryDataMigration, Requiredness: intent.Mandatory,
		RequirednessBasis: intent.BasisPolicyFloor, Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: json.RawMessage(`{"max_class":"TRANSFORM"}`)})
	w := e.requirements(e.alice, set, requirements)
	if w.Code != 201 {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	var stored promotion.StoredRequirements
	_ = json.Unmarshal(w.Body.Bytes(), &stored)
	spec := acceptanceFor(set, "500.01")
	spec.Cases = append(spec.Cases, intent.AcceptanceCase{ID: "migration-holds", RequirementID: "po-migration", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}})
	if w = e.acceptance(e.bob, stored.Version, spec); w.Code != 201 {
		t.Fatal("acceptance", w.Code, w.Body.String())
	}
	var accepted promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	art := m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: accepted.ID, sandboxID: sandbox}
	return viewOf(t, e.candidate(requester.token, art), 201).ArtifactHash
}

func m13apiSorted(reasons []string) bool {
	return len(reasons) > 0 && sort.StringsAreSorted(reasons)
}

func m13apiHas(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------- tests

// TestM13APIFullFlowApproveActivateHistoryRollback: build, refuse the excluded
// parties, approve by eligible third persons, activate, read the active release
// and history, release twice more and roll back in both rollback classes.
func TestM13APIFullFlowApproveActivateHistoryRollback(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c1, _ := e.build("500", nil, e.carol)

	v := viewOf(t, e.httpCandidate(e.viewer, c1), 200)
	if v.Status != "STAGED" || v.Approvals == nil || len(v.Approvals) != 0 || v.Rejections == nil || len(v.Rejections) != 0 || v.ApprovalsRequired != 1 || v.Active || v.RiskLevel != "HIGH" || v.Rollback != "DATA_NOT_REVERSIBLE" {
		t.Fatalf("fresh candidate lifecycle: %+v", v)
	}

	// HIGH: the requester and the acceptance author are refused; the reply
	// names the excluded role, never an actor id.
	before := e.state()
	for _, c := range []struct {
		actor m13Actor
		role  string
	}{{e.carol, "requester"}, {e.person("bob"), "acceptance_author"}} {
		w := e.httpApprove(c.actor.token, c1)
		r := m13apiExpect(t, "excluded "+c.role, w, 403, "separation_of_duties")
		if !strings.Contains(r.Message, c.role) {
			t.Errorf("separation refusal does not name the role %s: %s", c.role, r.Message)
		}
		for _, id := range []string{e.carol.id, e.f.bob, e.f.alice, e.dave.id, e.erin.id} {
			if strings.Contains(w.Body.String(), id) {
				t.Errorf("separation refusal leaks an actor id: %s", w.Body.String())
			}
		}
	}
	m13apiExpect(t, "activation without approval", e.httpActivate(e.erin.token, c1, "", 0), 409, "approval_quorum_not_met")
	e.unchanged("refused approvals and activation", before)

	// Eligible approvers: dave, then alice (the sandbox freezer is not excluded
	// at HIGH). A repeated approval is idempotent.
	v = viewOf(t, e.httpApprove(e.dave.token, c1), 201)
	if v.Status != "APPROVED" || len(v.Approvals) != 1 || v.Approvals[0].ActorID != e.dave.id || v.ApprovalsRequired != 1 {
		t.Fatalf("after one approval: %+v", v)
	}
	if v = viewOf(t, e.httpApprove(e.dave.token, c1), 200); len(v.Approvals) != 1 {
		t.Fatalf("a repeated approval added a row: %+v", v.Approvals)
	}
	if v = viewOf(t, e.httpApprove(e.alice, c1), 201); len(v.Approvals) != 2 {
		t.Fatalf("second approver: %+v", v.Approvals)
	}

	r1 := m13apiRelease(t, e.httpActivate(e.erin.token, c1, "", 0), 200)
	if r1.ArtifactHash != c1 || r1.Generation != 1 || r1.BundleVersion != "2.0.0" || r1.Candidate == nil || r1.Candidate.Status != "ACTIVE" || !r1.Candidate.Active || r1.Rollback != "DATA_NOT_REVERSIBLE" || !r1.RollbackPlanRequired {
		t.Fatalf("activation view: %+v", r1)
	}
	// A replayed activation is refused (the candidate's base is no longer the
	// pointer) and changes nothing.
	before = e.state()
	m13apiExpect(t, "replayed activation", e.httpActivate(e.erin.token, c1, "", 0), 409, "candidate_stale")
	e.unchanged("replayed activation", before)

	active := m13apiRelease(t, e.httpActive(e.viewer), 200)
	if active.ArtifactHash != c1 || active.Generation != 1 || active.Candidate == nil || active.Candidate.ArtifactHash != c1 {
		t.Fatalf("active release: %+v", active)
	}
	h := m13apiHistory(t, e.httpHistory(e.viewer))
	if len(h) != 1 || h[0].Seq != 1 || h[0].Kind != "ACTIVATE" || h[0].ArtifactHash != c1 || h[0].PreviousHash != "" || h[0].Generation != 1 || h[0].ActorID != e.erin.id ||
		h[0].RiskLevel != "HIGH" || h[0].StorageClass != "ADDITIVE" || h[0].PlanHash != "" {
		t.Fatalf("history after the first activation: %+v", h)
	}

	// Second release evolves from the first.
	c2, _ := e.build("600", nil, e.carol)
	v2 := viewOf(t, e.httpCandidate(e.alice, c2), 200)
	if v2.BaseRelease != c1 || v2.BaseGeneration != 1 || v2.Status != "STAGED" || v2.Stale == nil || *v2.Stale {
		t.Fatalf("second candidate: %+v", v2)
	}
	viewOf(t, e.httpApprove(e.dave.token, c2), 201)
	m13apiExpect(t, "activation over an empty expected base", e.httpActivate(e.erin.token, c2, "", 0), 409, "base_changed")
	if r := m13apiRelease(t, e.httpActivate(e.erin.token, c2, c1, 1), 200); r.Generation != 2 || r.ArtifactHash != c2 {
		t.Fatalf("second activation: %+v", r)
	}
	if v = viewOf(t, e.httpCandidate(e.viewer, c1), 200); v.Status != "SUPERSEDED" || v.Active {
		t.Fatalf("replaced release: %+v", v)
	}

	// Roll back from c2 to c1: the class of the CURRENT release (c2) decides.
	current := m13apiRelease(t, e.httpActive(e.viewer), 200)
	var firstPlan *m13Plan
	rollbackRefusals := 0
	if current.RollbackPlanRequired {
		m13apiExpect(t, "rollback without the required plan", e.httpRollback(e.erin.token, c1, c2, 2, nil), 409, "rollback_plan_required")
		rollbackRefusals++
		firstPlan = m13SafePlan()
	} else if current.Rollback != "POINTER_SAFE" {
		t.Fatalf("plan-free rollback class must be exactly POINTER_SAFE: %+v", current)
	}
	if r := m13apiRelease(t, e.httpRollback(e.erin.token, c1, c2, 2, firstPlan), 200); r.ArtifactHash != c1 || r.Generation != 3 || r.Candidate == nil || r.Candidate.Status != "ACTIVE" {
		t.Fatalf("rollback to the first release: %+v", r)
	}

	// c3 is approved but never activated: not a rollback target.
	c3, _ := e.build("700", nil, e.carol)
	viewOf(t, e.httpApprove(e.dave.token, c3), 201)

	// Now c1 (DATA_NOT_REVERSIBLE) is current: rolling back needs an acknowledged
	// plan; every refusal leaves the release state alone.
	before = e.state()
	safe := m13SafePlan()
	recovery := m13SafePlan()
	recovery.RecoveryRequired = true
	unacknowledged := m13SafePlan()
	unacknowledged.StorageCompatible = false
	for _, c := range []struct {
		name            string
		target, current string
		generation      int64
		plan            *m13Plan
		code            string
	}{
		{"no plan", c2, c1, 3, nil, "rollback_plan_required"},
		{"recovery required", c2, c1, 3, recovery, "recovery_required"},
		{"unacknowledged plan", c2, c1, 3, unacknowledged, "rollback_plan_required"},
		{"wrong expected current", c2, c2, 3, safe, "release_changed"},
		{"wrong expected generation", c2, c1, 2, safe, "release_changed"},
		{"target is current", c1, c1, 3, safe, "rollback_target_is_current"},
		{"never activated target", c3, c1, 3, safe, "rollback_target_not_released"},
	} {
		m13apiExpect(t, "rollback: "+c.name, e.httpRollback(e.erin.token, c.target, c.current, c.generation, c.plan), 409, c.code)
		rollbackRefusals++
	}
	e.unchanged("refused rollbacks", before)
	if r := m13apiRelease(t, e.httpRollback(e.erin.token, c2, c1, 3, safe), 200); r.ArtifactHash != c2 || r.Generation != 4 {
		t.Fatalf("planned rollback: %+v", r)
	}

	// History is newest first, dense and chained; plan hashes are recorded.
	h = m13apiHistory(t, e.httpHistory(e.viewer))
	if len(h) != 4 || h[0].Seq != 4 || h[0].Kind != "ROLLBACK" || h[0].ArtifactHash != c2 || h[0].PreviousHash != c1 || h[0].Generation != 4 || h[0].PlanHash != safe.PlanHash ||
		h[1].Kind != "ROLLBACK" || h[1].ArtifactHash != c1 || h[1].PreviousHash != c2 || h[2].Kind != "ACTIVATE" || h[2].ArtifactHash != c2 || h[3].ArtifactHash != c1 {
		t.Fatalf("history: %+v", h)
	}
	if firstPlan == nil && h[1].PlanHash != "" || firstPlan != nil && h[1].PlanHash != firstPlan.PlanHash {
		t.Fatalf("first rollback plan hash: %+v", h[1])
	}
	e.checkHistory()

	// A candidate built on the replaced pointer turns stale and cannot be approved.
	r := m13apiExpect(t, "approval after the base was rolled away", e.httpApprove(e.erin.token, c3), 409, "candidate_stale")
	if !m13apiHas(r.Reasons, "BASE_CHANGED") || !m13apiSorted(r.Reasons) {
		t.Fatalf("stale reasons: %v", r.Reasons)
	}

	// Audit: one allowed entry per effect, refusals audited as .rejected.
	for action, want := range map[string]int{"release.approved": 4, "release.approval_replayed": 1, "release.activated": 2, "release.rolled_back": 2} {
		if n := e.m13apiAudit(action, true); n != want {
			t.Errorf("audit %s: %d entries, want %d", action, n, want)
		}
	}
	for action, want := range map[string]int{"releases.approve.rejected": 3, "releases.activate.rejected": 3, "releases.rollback.rejected": rollbackRefusals} {
		if n := e.m13apiAudit(action, false); n != want {
			t.Errorf("audit %s: %d entries, want %d", action, n, want)
		}
	}
	activated := e.m13apiAuditState("release.activated")
	if len(activated) != 2 || activated[0]["artifact_hash"] != c1 || activated[0]["release_generation"] != float64(1) || activated[0]["approval_count"] != float64(2) ||
		activated[0]["approvals_required"] != float64(1) || activated[0]["risk_level"] != "HIGH" || activated[0]["storage_class"] != "ADDITIVE" {
		t.Fatalf("activation audit state: %v", activated)
	}
	if rolled := e.m13apiAuditState("release.rolled_back"); len(rolled) != 2 || rolled[1]["plan_hash"] != safe.PlanHash || rolled[1]["previous_hash"] != c1 || rolled[1]["rollback_class"] != "DATA_NOT_REVERSIBLE" || rolled[1]["release_generation"] != float64(4) {
		t.Fatalf("rollback audit state: %v", rolled)
	}
}

// TestM13APIForgedFieldsStrictBodiesAndHashBinding: no authority-bearing field
// is accepted, bodies are strict, and the body hash must equal the route hash.
func TestM13APIForgedFieldsStrictBodiesAndHashBinding(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c, _ := e.build("500", nil, e.carol)
	other := strings.Repeat("e", 64)
	before := e.state()

	forged := []string{"actor_id", "actor", "approver", "approvals", "approvals_required", "quorum", "parties", "requester_id", "risk_level", "high_risk", "separation", "status", "generation", "tenant_id", "excluded"}
	for _, key := range forged {
		body := map[string]any{"artifact_hash": c, key: "forged"}
		m13apiExpect(t, "approve forged "+key, e.post("/candidates/"+c+"/approve", e.dave.token, body), 400, "invalid_release_request")
		body = map[string]any{"artifact_hash": c, "reason": "OTHER", "note": "", key: "forged"}
		m13apiExpect(t, "reject forged "+key, e.post("/candidates/"+c+"/reject", e.dave.token, body), 400, "invalid_release_request")
		body = map[string]any{"artifact_hash": c, "expected_base": "", "expected_generation": 0, key: "forged"}
		m13apiExpect(t, "activate forged "+key, e.post("/candidates/"+c+"/activate", e.dave.token, body), 400, "invalid_release_request")
		body = map[string]any{"target": c, "expected_current": other, "expected_generation": 1, key: "forged"}
		m13apiExpect(t, "rollback forged "+key, e.post("/releases/rollback", e.dave.token, body), 400, "invalid_release_request")
	}
	for _, key := range []string{"class", "rollback", "rollback_class", "acknowledged"} {
		plan := map[string]any{"storage_compatible": true, "external_effects_reviewed": true, "recovery_required": false, "plan_hash": strings.Repeat("c", 64), key: "POINTER_SAFE"}
		m13apiExpect(t, "rollback plan forged "+key, e.post("/releases/rollback", e.dave.token, map[string]any{"target": c, "expected_current": other, "expected_generation": 1, "plan": plan}), 400, "invalid_release_request")
	}
	for name, body := range map[string]map[string]any{
		"reject without reason":   {"artifact_hash": c, "note": ""},
		"reject unknown reason":   {"artifact_hash": c, "reason": "BECAUSE", "note": ""},
		"reject lowercase reason": {"artifact_hash": c, "reason": "withdrawn", "note": ""},
		"reject control note":     {"artifact_hash": c, "reason": "OTHER", "note": "bell\u0007"},
		"reject newline note":     {"artifact_hash": c, "reason": "OTHER", "note": "line\nbreak"},
		"reject bidi note":        {"artifact_hash": c, "reason": "OTHER", "note": "abc‮def"},
		"reject long note":        {"artifact_hash": c, "reason": "OTHER", "note": strings.Repeat("n", 281)},
	} {
		m13apiExpect(t, name, e.post("/candidates/"+c+"/reject", e.dave.token, body), 400, "invalid_release_request")
	}
	for name, body := range map[string]map[string]any{
		"activate without expected base":       {"artifact_hash": c, "expected_generation": 0},
		"activate without expected generation": {"artifact_hash": c, "expected_base": ""},
		"activate empty base at generation 1":  {"artifact_hash": c, "expected_base": "", "expected_generation": 1},
		"activate base at generation 0":        {"artifact_hash": c, "expected_base": other, "expected_generation": 0},
		"activate uppercase base":              {"artifact_hash": c, "expected_base": strings.ToUpper(other), "expected_generation": 1},
		"activate negative generation":         {"artifact_hash": c, "expected_base": "", "expected_generation": -1},
		"activate fractional generation":       {"artifact_hash": c, "expected_base": other, "expected_generation": 1.5},
		"activate null generation":             {"artifact_hash": c, "expected_base": "", "expected_generation": nil},
	} {
		m13apiExpect(t, name, e.post("/candidates/"+c+"/activate", e.dave.token, body), 400, "invalid_release_request")
	}
	for name, body := range map[string]map[string]any{
		"rollback without generation":   {"target": c, "expected_current": other},
		"rollback generation 0":         {"target": c, "expected_current": other, "expected_generation": 0},
		"rollback short target":         {"target": c[:63], "expected_current": other, "expected_generation": 1},
		"rollback plan without hash":    {"target": c, "expected_current": other, "expected_generation": 1, "plan": map[string]any{"storage_compatible": true, "external_effects_reviewed": true}},
		"rollback plan with a bad hash": {"target": c, "expected_current": other, "expected_generation": 1, "plan": map[string]any{"storage_compatible": true, "external_effects_reviewed": true, "plan_hash": "zz"}},
	} {
		m13apiExpect(t, name, e.post("/releases/rollback", e.dave.token, body), 400, "invalid_release_request")
	}
	// The body hash must equal the hash of the route.
	m13apiExpect(t, "approve hash mismatch", e.post("/candidates/"+c+"/approve", e.dave.token, map[string]any{"artifact_hash": other}), 400, "hash_mismatch")
	m13apiExpect(t, "reject hash mismatch", e.post("/candidates/"+c+"/reject", e.dave.token, map[string]any{"artifact_hash": other, "reason": "OTHER", "note": ""}), 400, "hash_mismatch")
	m13apiExpect(t, "activate hash mismatch", e.post("/candidates/"+c+"/activate", e.dave.token, map[string]any{"artifact_hash": other, "expected_base": "", "expected_generation": 0}), 400, "hash_mismatch")
	m13apiExpect(t, "approve without hash", e.post("/candidates/"+c+"/approve", e.dave.token, map[string]any{}), 400, "hash_mismatch")
	// Duplicate keys, trailing documents, other media types, empty and oversized bodies.
	approve := e.root + "/candidates/" + c + "/approve"
	for name, w := range map[string]*httptest.ResponseRecorder{
		"duplicate key":     m13apiRaw(e.h, approve, e.dave.token, "application/json", `{"artifact_hash":"`+other+`","artifact_hash":"`+c+`"}`),
		"trailing body":     m13apiRaw(e.h, approve, e.dave.token, "application/json", `{"artifact_hash":"`+c+`"} {"artifact_hash":"`+c+`"}`),
		"case alias":        m13apiRaw(e.h, approve, e.dave.token, "application/json", `{"Artifact_Hash":"`+c+`"}`),
		"text/plain":        m13apiRaw(e.h, approve, e.dave.token, "text/plain", `{"artifact_hash":"`+c+`"}`),
		"empty body":        m13apiRaw(e.h, approve, e.dave.token, "application/json", ``),
		"array body":        m13apiRaw(e.h, approve, e.dave.token, "application/json", `["`+c+`"]`),
		"over one mebibyte": m13apiRaw(e.h, e.root+"/candidates/"+c+"/reject", e.dave.token, "application/json", `{"artifact_hash":"`+c+`","reason":"OTHER","note":"`+strings.Repeat("x", 1<<20)+`"}`),
	} {
		if w.Code != 400 && !(name == "over one mebibyte" && w.Code == 413) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	e.unchanged("strict body refusals", before)
	if n := e.m13apiAudit("release.approved", true) + e.m13apiAudit("release.rejected", true); n != 0 {
		t.Fatalf("a refused body produced a decision: %d", n)
	}
}

// TestM13APIRoleMatrixCustomRoleTenantAndAppScope: default roles, a custom role
// holding only releases.approve, other tenants and other applications.
func TestM13APIRoleMatrixCustomRoleTenantAndAppScope(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	// Custom roles take effect through the policy loader. Publishing changes the
	// policy bundle, so it happens before the candidate is built.
	hp := r5Handler(t, e.f, audit.Postgres{})
	tenantRoot := "/v1/tenants/" + e.f.tenantA
	r5Publish(t, hp, tenantRoot+"/roles", e.alice, 0, policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "release_approver", Grants: []policy.Grant{
		{Effect: "ALLOW", Actions: []string{"releases.approve"}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}}, true, uuid(t))
	frank := e.member("frank", "member")
	r5Assign(t, hp, tenantRoot, e.alice, frank.id, "release_approver", 200)
	// A role named viewer can never hold release authority.
	for _, action := range []string{"releases.approve", "releases.activate", "releases.rollback"} {
		w := m5Call(hp, tenantRoot+"/roles", e.alice, map[string]any{"operation_id": uuid(t), "expected_version": 0, "active": true, "definition": policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "viewer",
			Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{action}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}}})
		if w.Code != 400 {
			t.Errorf("viewer role granted %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	mallory := e.member("mallory", "member")
	c, _ := e.build("500", nil, e.carol)
	other := strings.Repeat("e", 64)
	before := e.state()

	// Viewer and member: read, never decide.
	for _, who := range []m13Actor{e.person("viewer"), mallory} {
		m13apiExpect(t, who.name+" approves", e.httpApprove(who.token, c), 403, "forbidden")
		m13apiExpect(t, who.name+" rejects", e.httpReject(who.token, c, "OTHER", ""), 403, "forbidden")
		m13apiExpect(t, who.name+" activates", e.httpActivate(who.token, c, "", 0), 403, "forbidden")
		m13apiExpect(t, who.name+" rolls back", e.httpRollback(who.token, c, other, 1, nil), 403, "forbidden")
		if v := viewOf(t, e.httpCandidate(who.token, c), 200); v.Status != "STAGED" {
			t.Errorf("%s candidate view: %+v", who.name, v)
		}
		m13apiHistory(t, e.httpHistory(who.token))
		m13apiExpect(t, who.name+" reads the empty pointer", e.httpActive(who.token), 404, "not_found")
	}
	m13apiExpect(t, "unauthenticated approval", e.httpApprove("", c), 401, "unauthenticated")
	e.unchanged("default role refusals", before)

	// The custom role approves and can do nothing else.
	if w := m5Call(hp, e.root+"/candidates/"+c+"/approve", frank.token, map[string]any{"artifact_hash": c}); w.Code != 201 {
		t.Fatalf("custom approver: %d %s", w.Code, w.Body.String())
	}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"activate":  m5Call(hp, e.root+"/candidates/"+c+"/activate", frank.token, map[string]any{"artifact_hash": c, "expected_base": "", "expected_generation": 0}),
		"rollback":  m5Call(hp, e.root+"/releases/rollback", frank.token, map[string]any{"target": c, "expected_current": other, "expected_generation": 1}),
		"candidate": m3Call(hp, "GET", e.root+"/candidates/"+c, frank.token, 0, nil),
		"history":   m3Call(hp, "GET", e.root+"/releases", frank.token, 0, nil),
		"active":    m3Call(hp, "GET", e.root+"/releases/active", frank.token, 0, nil),
	} {
		if w.Code != 403 {
			t.Errorf("custom approver %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	// Approving and activating are separate authorities: the owner activates on the strength of the
	// custom approver's approval, which the database counts and the policy confirms. The activation
	// goes through the same loader-configured handler: a handler that knows only the built-in roles
	// would (correctly) not count the approval of a member who holds a custom role.
	if r := m13apiRelease(t, m5Call(hp, e.root+"/candidates/"+c+"/activate", e.alice, map[string]any{"artifact_hash": c, "expected_base": "", "expected_generation": 0}), 200); r.Generation != 1 || r.Candidate == nil || len(r.Candidate.Approvals) != 1 || r.Candidate.Approvals[0].ActorID != frank.id {
		t.Fatalf("activation on a custom-role approval: %+v", r)
	}

	// Another tenant sees nothing of tenant A, and tenant A's hash means nothing
	// in tenant B or in another application.
	trent := e.addMemberIn(e.f.tenantB, "trent", "owner")
	before = e.state()
	for name, w := range map[string]*httptest.ResponseRecorder{
		"approve":   e.httpApprove(trent.token, c),
		"reject":    e.httpReject(trent.token, c, "OTHER", ""),
		"activate":  e.httpActivate(trent.token, c, "", 0),
		"rollback":  e.httpRollback(trent.token, c, other, 1, nil),
		"candidate": e.httpCandidate(trent.token, c),
		"history":   e.httpHistory(trent.token),
		"active":    e.httpActive(trent.token),
	} {
		m13apiExpect(t, "other tenant "+name, w, 403, "forbidden")
	}
	rootB := "/v1/tenants/" + e.f.tenantB + "/preview/" + e.app
	rootX := "/v1/tenants/" + e.f.tenantA + "/preview/app.other_" + strings.ReplaceAll(uuid(t), "-", "")
	for _, s := range []struct {
		name, root, token string
	}{{"tenant B", rootB, trent.token}, {"other app", rootX, e.alice}} {
		m13apiExpect(t, s.name+" approve", e.postAt(s.root, "/candidates/"+c+"/approve", s.token, map[string]any{"artifact_hash": c}), 404, "not_found")
		m13apiExpect(t, s.name+" reject", e.postAt(s.root, "/candidates/"+c+"/reject", s.token, map[string]any{"artifact_hash": c, "reason": "OTHER", "note": ""}), 404, "not_found")
		m13apiExpect(t, s.name+" activate", e.postAt(s.root, "/candidates/"+c+"/activate", s.token, map[string]any{"artifact_hash": c, "expected_base": "", "expected_generation": 0}), 404, "not_found")
		m13apiExpect(t, s.name+" candidate", e.getAt(s.root, "/candidates/"+c, s.token), 404, "not_found")
		m13apiExpect(t, s.name+" active", e.getAt(s.root, "/releases/active", s.token), 404, "not_found")
		m13apiExpect(t, s.name+" rollback", e.postAt(s.root, "/releases/rollback", s.token, map[string]any{"target": other, "expected_current": c, "expected_generation": 1}), 409, "no_active_release")
		if hist := m13apiHistory(t, e.getAt(s.root, "/releases", s.token)); len(hist) != 0 {
			t.Errorf("%s history shows tenant A releases: %+v", s.name, hist)
		}
	}
	// An unknown candidate is a 404 on every route.
	unknown := strings.Repeat("d", 64)
	m13apiExpect(t, "unknown approve", e.httpApprove(e.dave.token, unknown), 404, "not_found")
	m13apiExpect(t, "unknown reject", e.httpReject(e.dave.token, unknown, "OTHER", ""), 404, "not_found")
	m13apiExpect(t, "unknown activate", e.httpActivate(e.dave.token, unknown, c, 1), 404, "not_found")
	m13apiExpect(t, "unknown candidate", e.httpCandidate(e.dave.token, unknown), 404, "not_found")
	m13apiExpect(t, "unknown rollback target", e.httpRollback(e.dave.token, unknown, c, 1, nil), 404, "not_found")
	e.unchanged("cross-scope requests", before)
}

// TestM13APIStaleAtApproveAndActivate: a policy head change and a moved base
// make the candidate stale at the approval and at the activation gate.
func TestM13APIStaleAtApproveAndActivate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)

	// Policy change before the approval.
	c1, _ := e.build("500", nil, e.carol)
	m13apiPublishRole(t, e, "auditor", 0)
	before := e.state()
	r := m13apiExpect(t, "approval after a policy change", e.httpApprove(e.dave.token, c1), 409, "candidate_stale")
	if strings.Join(r.Reasons, ",") != "POLICY_CHANGED" {
		t.Fatalf("stale reasons: %v", r.Reasons)
	}
	e.unchanged("stale approval", before)
	if states := e.m13apiAuditState("releases.approve.rejected"); len(states) != 1 || states[0]["artifact_hash"] != c1 || fmt.Sprint(states[0]["stale_reasons"]) != "[POLICY_CHANGED]" {
		t.Fatalf("stale refusal audit: %v", states)
	}

	// Policy change between the approval and the activation.
	c2, _ := e.build("600", nil, e.carol)
	viewOf(t, e.httpApprove(e.dave.token, c2), 201)
	m13apiPublishRole(t, e, "auditor", 1)
	before = e.state()
	r = m13apiExpect(t, "activation after a policy change", e.httpActivate(e.erin.token, c2, "", 0), 409, "candidate_stale")
	if strings.Join(r.Reasons, ",") != "POLICY_CHANGED" {
		t.Fatalf("stale reasons at activation: %v", r.Reasons)
	}
	e.unchanged("stale activation", before)

	// The base moves: two first-release candidates, one activated first.
	c3, _ := e.build("700", nil, e.carol)
	c4, _ := e.build("800", nil, e.carol)
	viewOf(t, e.httpApprove(e.dave.token, c3), 201)
	viewOf(t, e.httpApprove(e.dave.token, c4), 201)
	m13apiRelease(t, e.httpActivate(e.erin.token, c3, "", 0), 200)
	before = e.state()
	r = m13apiExpect(t, "activation over a moved base", e.httpActivate(e.erin.token, c4, "", 0), 409, "candidate_stale")
	if !m13apiHas(r.Reasons, "BASE_CHANGED") || !m13apiSorted(r.Reasons) {
		t.Fatalf("moved base reasons: %v", r.Reasons)
	}
	r = m13apiExpect(t, "approval over a moved base", e.httpApprove(e.erin.token, c4), 409, "candidate_stale")
	if !m13apiHas(r.Reasons, "BASE_CHANGED") || !m13apiSorted(r.Reasons) {
		t.Fatalf("moved base reasons at approval: %v", r.Reasons)
	}
	if v := viewOf(t, e.httpCandidate(e.viewer, c4), 200); v.Stale == nil || !*v.Stale || len(v.Approvals) != 1 {
		t.Fatalf("stale candidate view: %+v", v)
	}
	e.unchanged("moved base refusals", before)

	// A fresh candidate on the new base: the expected base must be exactly the
	// pointer the candidate was built on.
	c5, _ := e.build("900", nil, e.carol)
	viewOf(t, e.httpApprove(e.dave.token, c5), 201)
	m13apiExpect(t, "expected empty base", e.httpActivate(e.erin.token, c5, "", 0), 409, "base_changed")
	m13apiExpect(t, "expected wrong generation", e.httpActivate(e.erin.token, c5, c3, 2), 409, "base_changed")
	m13apiExpect(t, "expected wrong base", e.httpActivate(e.erin.token, c5, c4, 1), 409, "base_changed")
	if rel := m13apiRelease(t, e.httpActivate(e.erin.token, c5, c3, 1), 200); rel.Generation != 2 {
		t.Fatalf("activation on the exact base: %+v", rel)
	}
	e.checkHistory()
}

// TestM13APIFailedVerificationAndRejectionPermanence: a self-consistent forged
// body staged directly through the port never passes the API, and rejections
// are permanent for the exact hash.
func TestM13APIFailedVerificationAndRejectionPermanence(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()

	// A compromised API process lowers the risk to LOW (consistently, so the
	// stage port accepts it) to let the requester approve alone.
	forged, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal("stage forged candidate", err)
	}
	critical, err := e.stageLevel("CRITICAL")
	if err != nil {
		t.Fatal("stage forged critical candidate", err)
	}
	before := e.state()
	for name, w := range map[string]*httptest.ResponseRecorder{
		"requester approves":    e.httpApprove(e.carol.token, forged),
		"third person approves": e.httpApprove(e.dave.token, forged),
		"activate":              e.httpActivate(e.dave.token, forged, "", 0),
		"reject":                e.httpReject(e.dave.token, forged, "OTHER", ""),
		"read":                  e.httpCandidate(e.viewer, forged),
		"critical approve":      e.httpApprove(e.dave.token, critical),
	} {
		m13apiExpect(t, "forged candidate: "+name, w, 409, "candidate_failed_verification")
		if strings.Contains(w.Body.String(), "recompute:") || strings.Contains(w.Body.String(), "risk") {
			t.Errorf("failed verification details leaked: %s", w.Body.String())
		}
	}
	e.unchanged("forged candidates", before)
	if got := e.approvalsOf(forged); len(got) != 0 {
		t.Fatal("a forged candidate was approved", got)
	}
	found := false
	for _, s := range e.m13apiAuditState("releases.approve.rejected") {
		found = found || s["artifact_hash"] == forged && s["verifier_outcome"] == "RESTORE_FAILED"
	}
	if !found {
		t.Fatal("the failed verification is not audited")
	}

	// Rejection: the requester may withdraw; a third person rejects; both are
	// permanent and bound to the exact hash.
	note := "needs-a-second-look-" + uuid(t)
	v := viewOf(t, e.httpReject(e.dave.token, tmpl.hash, "RISK_UNACCEPTABLE", note), 201)
	if v.Status != "REJECTED" || len(v.Rejections) != 1 || v.Rejections[0].ActorID != e.dave.id || v.Rejections[0].Reason != "RISK_UNACCEPTABLE" || v.Rejections[0].Note != note {
		t.Fatalf("rejection view: %+v", v)
	}
	if v = viewOf(t, e.httpReject(e.dave.token, tmpl.hash, "OTHER", "again"), 200); len(v.Rejections) != 1 || v.Rejections[0].Reason != "RISK_UNACCEPTABLE" {
		t.Fatalf("a repeated rejection changed the record: %+v", v.Rejections)
	}
	if v = viewOf(t, e.httpReject(e.carol.token, tmpl.hash, "WITHDRAWN", ""), 201); len(v.Rejections) != 2 {
		t.Fatalf("withdrawal by the requester: %+v", v.Rejections)
	}
	before = e.state()
	m13apiExpect(t, "approval after rejection", e.httpApprove(e.erin.token, tmpl.hash), 409, "candidate_rejected")
	m13apiExpect(t, "activation after rejection", e.httpActivate(e.erin.token, tmpl.hash, "", 0), 409, "candidate_rejected")
	// An identical rebuild is the same artifact and stays rejected.
	if v = viewOf(t, e.candidate(e.carol.token, tmpl.art), 200); v.ArtifactHash != tmpl.hash || v.Staged || v.Status != "REJECTED" {
		t.Fatalf("identical rebuild: %+v", v)
	}
	e.unchanged("rejected candidate", before)
	// The note never reaches audit; the reason does.
	var leaked int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND state::text LIKE '%'||$2||'%'`, e.f.tenantA, note).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("rejection note copied into audit: %d %v", leaked, err)
	}
	if states := e.m13apiAuditState("release.rejected"); len(states) != 2 || states[0]["reject_reason"] != "RISK_UNACCEPTABLE" || states[1]["reject_reason"] != "WITHDRAWN" {
		t.Fatalf("rejection audit: %v", states)
	}
	if n := e.m13apiAudit("release.rejection_replayed", true); n != 1 {
		t.Fatalf("replayed rejection audit: %d", n)
	}
	// A rebuild with a changed input (a new requirement set version) is a new
	// hash and can proceed.
	fresh, _ := e.build("500", nil, e.carol)
	if fresh == tmpl.hash {
		t.Fatal("a changed input kept the rejected hash")
	}
	if v = viewOf(t, e.httpApprove(e.erin.token, fresh), 201); v.Status != "APPROVED" {
		t.Fatalf("rebuilt candidate: %+v", v)
	}
}

// TestM13APICriticalQuorumOnATransformRelease: a real CRITICAL candidate (a
// second release relaxing a required field: TRANSFORM storage with unverified
// volume) needs two distinct approvers beyond requester, acceptance author and
// sandbox freezer, and the database re-counts approvers at activation.
func TestM13APICriticalQuorumOnATransformRelease(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	// The first release makes the note required; the second relaxes it again
	// (RELAX_REQUIRED: TRANSFORM storage). The record id must stay required.
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
	viewOf(t, e.httpApprove(e.dave.token, c1), 201)
	m13apiRelease(t, e.httpActivate(e.erin.token, c1, "", 0), 200)

	c2 := m13apiBuildMigration(t, e, required(false), e.carol)
	v := viewOf(t, e.httpCandidate(e.viewer, c2), 200)
	if v.RiskLevel != "CRITICAL" || v.StorageClass != "TRANSFORM" || v.ApprovalsRequired != 2 || len(v.Separation.Excluded) != 3 || v.Status != "STAGED" {
		t.Fatalf("critical candidate: %+v", v)
	}
	before := e.state()
	for _, c := range []struct {
		actor m13Actor
		role  string
	}{{e.carol, "requester"}, {e.person("bob"), "acceptance_author"}, {e.person("alice"), "sandbox_freezer"}} {
		if r := m13apiExpect(t, "critical "+c.role, e.httpApprove(c.actor.token, c2), 403, "separation_of_duties"); !strings.Contains(r.Message, c.role) {
			t.Errorf("refusal does not name %s: %s", c.role, r.Message)
		}
	}
	e.unchanged("excluded critical approvers", before)
	if v = viewOf(t, e.httpApprove(e.dave.token, c2), 201); v.Status != "STAGED" || len(v.Approvals) != 1 {
		t.Fatalf("one of two approvals: %+v", v)
	}
	if v = viewOf(t, e.httpApprove(e.dave.token, c2), 200); len(v.Approvals) != 1 {
		t.Fatalf("one approver twice is not a quorum: %+v", v)
	}
	m13apiExpect(t, "critical activation with one approver", e.httpActivate(e.erin.token, c2, c1, 1), 409, "approval_quorum_not_met")
	if v = viewOf(t, e.httpApprove(e.erin.token, c2), 201); v.Status != "APPROVED" || len(v.Approvals) != 2 {
		t.Fatalf("two approvals: %+v", v)
	}
	// erin is demoted: the handler narrows the quorum to approvers who still hold releases.approve
	// (M13R-03) and refuses before the port. The port's own re-count of live memberships under its
	// lock is what the direct port tests and the concurrent demotion tests cover.
	e.setRole(e.erin.id, "viewer")
	before = e.state()
	m13apiExpect(t, "activation after an approver was demoted", e.httpActivate(e.dave.token, c2, c1, 1), 409, "approval_quorum_not_met")
	if v = viewOf(t, e.httpCandidate(e.viewer, c2), 200); v.Status != "STAGED" || len(v.Approvals) != 2 {
		t.Fatalf("the view must agree with the refusal and keep the recorded approvals: %+v", v)
	}
	e.unchanged("quorum re-check", before)
	e.setRole(e.erin.id, "admin")
	r := m13apiRelease(t, e.httpActivate(e.dave.token, c2, c1, 1), 200)
	if r.Generation != 2 || r.Rollback != "DATA_NOT_REVERSIBLE" || !r.RollbackPlanRequired {
		t.Fatalf("critical activation: %+v", r)
	}
	if h := e.checkHistory(); len(h) != 2 || h[1].riskLevel != "CRITICAL" || h[1].storageClass != "TRANSFORM" || h[1].actor != e.dave.id {
		t.Fatalf("critical history: %+v", h)
	}
}

// TestM13APIConcurrentActivationsExactlyOneWins: two eligible candidates on the
// same (empty) base race; exactly one becomes active and history has one row.
func TestM13APIConcurrentActivationsExactlyOneWins(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	a, _ := e.build("500", nil, e.carol)
	b, _ := e.build("600", nil, e.carol)
	viewOf(t, e.httpApprove(e.dave.token, a), 201)
	viewOf(t, e.httpApprove(e.dave.token, b), 201)
	start := make(chan struct{})
	codes := make([]int, 2)
	bodies := make([]string, 2)
	var wg sync.WaitGroup
	for i, hash := range []string{a, b} {
		wg.Add(1)
		go func(i int, hash string) {
			defer wg.Done()
			<-start
			w := e.httpActivate(e.erin.token, hash, "", 0)
			codes[i], bodies[i] = w.Code, w.Body.String()
		}(i, hash)
	}
	close(start)
	wg.Wait()
	sort.Ints(codes)
	if codes[0] != 200 || codes[1] != 409 {
		t.Fatalf("racing activations: %v %v", codes, bodies)
	}
	pointer, generation, _ := e.activePointer()
	if h := e.checkHistory(); len(h) != 1 || generation != 1 || h[0].hash != pointer || pointer != a && pointer != b {
		t.Fatalf("race left %s@%d with history %+v", pointer, generation, h)
	}
}

// TestM13APIProductionConfigurationExposesNoReleaseRoute: without preview (or
// without a registry) none of the six routes exists.
func TestM13APIProductionConfigurationExposesNoReleaseRoute(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash := strings.Repeat("a", 64)
	for name, api := range map[string]access.API{
		"preview disabled": {DB: e.f.db, Auth: e.f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: false},
		"no registry":      {DB: e.f.db, Auth: e.f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, PreviewEnabled: true},
	} {
		h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
		for _, path := range []string{"/candidates/" + hash + "/approve", "/candidates/" + hash + "/reject", "/candidates/" + hash + "/activate", "/releases/rollback"} {
			if w := m5Call(h, e.root+path, e.alice, map[string]any{"artifact_hash": hash}); w.Code != 413 {
				t.Errorf("%s: POST %s with a body: %d %s", name, path, w.Code, w.Body.String())
			}
			if w := m5Call(h, e.root+path, e.alice, nil); w.Code != 404 {
				t.Errorf("%s: POST %s: %d %s", name, path, w.Code, w.Body.String())
			}
		}
		for _, path := range []string{"/releases", "/releases/active", "/candidates/" + hash} {
			if w := m3Call(h, "GET", e.root+path, e.alice, 0, nil); w.Code != 404 {
				t.Errorf("%s: GET %s: %d %s", name, path, w.Code, w.Body.String())
			}
		}
	}
	// The preview configuration of the same tenant does expose them.
	if w := e.httpHistory(e.alice); w.Code != 200 {
		t.Fatalf("preview history: %d %s", w.Code, w.Body.String())
	}
}
