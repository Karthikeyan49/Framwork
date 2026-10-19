//go:build integration

package integration

// Regression tests for the independent M12 API review
// (docs/verification/M12_API_INDEPENDENT_REVIEW.md): F9 (bounded, typed, audited
// recomputation), F1 (one audit row per stage), F6 (rejection evidence) and the
// findings below it. Each test names its finding.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
)

// refusalAudit counts the audit rows of e.app that carry a refusal code.
func (e *m13Env) refusalAudit(code string) int {
	e.t.Helper()
	var n int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND state->>'refusal_code'=$2 AND state->>'metadata_app'=$3`, e.f.tenantA, code, e.app).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// TestM12APIColdRecomputationIsBoundedTypedAndAudited (F9): a request that would start a
// cold recomputation beyond the per-tenant or per-process bound is refused at once with a
// typed 429 / 503 and Retry-After, audited with a closed code, and writes nothing, which
// holds for the M13 approve and activate routes as well (fail closed). What was computed
// before stays remembered, so the retry after the bound is released is served, and a
// remembered artifact needs no token at all.
func TestM12APIColdRecomputationIsBoundedTypedAndAudited(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash, _ := e.build("500", nil, e.carol)

	// A restarted process: nothing is remembered, one cold computation per tenant and two
	// per process may run.
	rc := promotion.NewRecompute(promotion.Limits{Process: 2, Tenant: 1, RetryAfter: 3 * time.Second})
	e.useAPI(func(a *access.API) { a.Recompute = rc })
	tenantScope := promotion.WithTenant(context.Background(), e.f.tenantA)

	hold, err := rc.Admit(tenantScope)
	if err != nil {
		t.Fatal(err)
	}
	w := e.httpCandidate(e.viewer, hash)
	m13apiExpect(t, "detail read at the tenant bound", w, 429, "release_busy")
	if w.Header().Get("Retry-After") != "3" {
		t.Errorf("Retry-After of the tenant refusal: %q", w.Header().Get("Retry-After"))
	}
	if n := e.refusalAudit("RECOMPUTE_BUSY_TENANT"); n != 1 {
		t.Errorf("the tenant refusal must be audited once: %d", n)
	}
	// A cold M13 decision is refused the same way and decides nothing.
	before := e.state()
	w = e.httpApprove(e.dave.token, hash)
	m13apiExpect(t, "approval at the tenant bound", w, 429, "release_busy")
	if w.Header().Get("Retry-After") != "3" {
		t.Errorf("Retry-After of the refused approval: %q", w.Header().Get("Retry-After"))
	}
	e.unchanged("approval refused as busy", before)
	hold()

	// Another tenant holds both process tokens: this tenant is told 503, not 429.
	other := promotion.WithTenant(context.Background(), "other-tenant-1")
	third := promotion.WithTenant(context.Background(), "other-tenant-2")
	h1, err1 := rc.Admit(other)
	h2, err2 := rc.Admit(third)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	w = e.httpCandidate(e.viewer, hash)
	m13apiExpect(t, "detail read at the process bound", w, 503, "release_busy")
	if w.Header().Get("Retry-After") != "3" {
		t.Errorf("Retry-After of the process refusal: %q", w.Header().Get("Retry-After"))
	}
	if n := e.refusalAudit("RECOMPUTE_BUSY_PROCESS"); n != 1 {
		t.Errorf("the process refusal must be audited once: %d", n)
	}
	h1()
	h2()

	// The bound released, the retry is served; the next read needs no computation.
	view := viewOf(t, e.httpCandidate(e.viewer, hash), 200)
	if view.ArtifactHash != hash || view.Stale == nil || *view.Stale {
		t.Fatalf("the retried read: %+v", view)
	}
	cold := rc.Stats()
	hold, err = rc.Admit(tenantScope)
	if err != nil {
		t.Fatal(err)
	}
	viewOf(t, e.httpCandidate(e.viewer, hash), 200) // a remembered artifact takes no token
	viewOf(t, e.httpApprove(e.dave.token, hash), 201)
	hold()
	if s := rc.Stats(); s.Hits <= cold.Hits || s.Computes != cold.Computes {
		t.Errorf("remembered artifacts were recomputed: before %+v after %+v", cold, s)
	}
	if rel := m13apiRelease(t, e.httpActivate(e.erin.token, hash, "", 0), 200); rel.Generation != 1 {
		t.Fatalf("activation after the refusals: %+v", rel)
	}
	if s := rc.Stats(); s.Active != 0 {
		t.Errorf("admission tokens leaked: %+v", s)
	}
	e.checkHistory()
}

// TestM12APIStageWritesOneCorrelatedAuditRow (F1): ADR038 amendment 8 names one audit row
// for a stage, release.candidate_staged, and one for a replay. The M6R store-level row
// release.staged (no request id, a stale contract version) is gone.
func TestM12APIStageWritesOneCorrelatedAuditRow(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash, art := e.build("500", nil, e.carol)

	count := func(action string) (n int) {
		if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND (state->>'artifact_hash'=$3 OR state->>'metadata_app'=$4)`, e.f.tenantA, action, hash, e.app).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("release.staged"); n != 0 {
		t.Errorf("a stage wrote %d store-level release.staged rows", n)
	}
	if n := count("release.candidate_staged"); n != 1 {
		t.Errorf("a stage must write exactly one release.candidate_staged row: %d", n)
	}
	viewOf(t, e.candidate(e.carol.token, art), 200) // the replay
	if n, s := count("release.candidate_replayed"), count("release.staged"); n != 1 || s != 0 {
		t.Errorf("a replay writes one release.candidate_replayed and no release.staged: %d, %d", n, s)
	}
	var withRequest int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='release.candidate_staged' AND request_id<>'' AND decision_id<>'' AND state->>'metadata_app'=$2`, e.f.tenantA, e.app).Scan(&withRequest); err != nil || withRequest != 1 {
		t.Errorf("the stage row is not correlated with the request and the decision: %d %v", withRequest, err)
	}
}

// TestM12APIRejectedBuildAuditsReasonAndAssessmentHash (F6): ADR038 amendment 8 promises
// the report and assessment hashes of a rejection; the closed reason and the hash of the
// risk assessment were missing from the audit row.
func TestM12APIRejectedBuildAuditsReasonAndAssessmentHash(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	isHash := func(v any) bool {
		s, _ := v.(string)
		return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
	}

	// A first release with a rename intent that renames nothing: refused before any report.
	art := e.prepareDoc(orderDocument(e.app, "500"), "500", e.person("alice"), e.person("bob"))
	request := art.request()
	request["evolution_intent"] = map[string]any{"renames": []map[string]string{{"entity_id": "entity.order", "from": "field.order.note", "to": "field.order.memo"}}}
	w := e.post("/candidates", e.carol.token, request)
	var rejection struct {
		Reason string `json:"reason"`
	}
	if w.Code != 422 || json.Unmarshal(w.Body.Bytes(), &rejection) != nil || rejection.Reason == "" {
		t.Fatalf("a first release with an unused evolution intent: %d %s", w.Code, w.Body.String())
	}
	states := e.m13apiAuditState("candidates.build.rejected")
	if len(states) != 1 || states[0]["refusal_code"] != rejection.Reason {
		t.Fatalf("the audit row does not carry the reason %q: %v", rejection.Reason, states)
	}

	// A wrong implementation: the verifier report and the risk assessment are both hashed.
	revision, requirements := e.heads()
	raw, _ := json.Marshal(orderDocument(e.app, "5000"))
	if r := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, revision, raw); r.Code != 201 {
		t.Fatal("register", r.Code, r.Body.String())
	}
	sandbox := e.sandbox(e.alice, revision+1)
	set := thresholdSet(e.app, "500")
	var stored promotion.StoredRequirements
	if r := e.requirements(e.alice, set, requirements); r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &stored) != nil {
		t.Fatal("requirements", r.Code, r.Body.String())
	}
	var spec promotion.StoredAcceptance
	if r := e.acceptance(e.bob, stored.Version, acceptanceFor(set, "500.01")); r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &spec) != nil {
		t.Fatal("acceptance", r.Code, r.Body.String())
	}
	wrong := m12Artifacts{revision: revision + 1, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
	if w = e.candidate(e.carol.token, wrong); w.Code != 422 {
		t.Fatalf("a wrong implementation was staged: %d %s", w.Code, w.Body.String())
	}
	states = e.m13apiAuditState("candidates.build.rejected")
	if len(states) != 2 {
		t.Fatalf("rejection rows: %v", states)
	}
	last := states[1]
	if last["refusal_code"] != promotion.RejectVerificationFailed || !isHash(last["assessment_hash"]) || !isHash(last["artifact_hash"]) || last["verifier_outcome"] != "FAIL" {
		t.Errorf("the verifier rejection lacks its reason or hashes: %v", last)
	}
}
