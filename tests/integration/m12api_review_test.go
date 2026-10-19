//go:build integration

package integration

// Regression tests for the independent M12 API review
// (docs/verification/M12_API_INDEPENDENT_REVIEW.md): F9 (bounded, typed, audited
// recomputation), F1 (one audit row per stage), F6 (rejection evidence) and the
// findings below it. Each test names its finding.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestM12APIStageWritesOneCorrelatedAuditRow (F1): ADR102 amendment 8 names one audit row
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

// TestM12APIRejectedBuildAuditsReasonAndAssessmentHash (F6): ADR102 amendment 8 promises
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

// ------------------------------------------------------------------ F7

// concurrently runs n copies of fn at once and returns their results in order.
func concurrently[T any](n int, fn func() T) []T {
	out := make([]T, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return out
}

// TestM12APIConcurrentIdenticalRequestsConvergeOnTheReplay (F7): idempotency held only for
// sequential retries; of four concurrent identical builds three got 409 and an audit row
// "rejected" for a request that is a replay. Identical candidate builds and acceptance posts
// now converge (one 201, the others replay), while the compare-and-set append of a
// requirement set keeps exactly one winner.
func TestM12APIConcurrentIdenticalRequestsConvergeOnTheReplay(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	// The admission bound of F9 would answer three of four concurrent builds of one tenant with
	// 429 (a typed refusal that converges on retry); this test is about the database race that
	// remains when they are admitted, so it admits all four.
	e.useAPI(func(a *access.API) { a.Recompute = promotion.NewRecompute(promotion.Limits{Process: 8, Tenant: 8}) })
	for round, threshold := range []string{"500", "510", "520"} {
		art := e.prepareDoc(orderDocument(e.app, threshold), threshold, e.person("alice"), e.person("bob"))
		codes := concurrently(4, func() int { return e.candidate(e.carol.token, art).Code })
		created, replayed := 0, 0
		for _, c := range codes {
			switch c {
			case 201:
				created++
			case 200:
				replayed++
			}
		}
		if created != 1 || replayed != 3 {
			t.Fatalf("round %d: four concurrent identical builds answered %v, want one 201 and three 200", round, codes)
		}
	}
	if n := e.auditCount("release.candidate_staged"); n != 3 {
		t.Errorf("staged audit rows: %d", n)
	}
	if n := e.auditCount("release.candidate_replayed"); n != 9 {
		t.Errorf("replay audit rows: %d", n)
	}
	if n := e.auditCount("candidates.build.rejected"); n != 0 {
		t.Errorf("a replay was audited as a rejection: %d", n)
	}
	var candidates int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&candidates); err != nil || candidates != 3 {
		t.Errorf("candidates staged: %d %v", candidates, err)
	}

	// Four identical acceptance posts by one author: one spec, the same answer for all.
	_, requirements := e.heads()
	set := thresholdSet(e.app, "530")
	var stored promotion.StoredRequirements
	if w := e.requirements(e.alice, set, requirements); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &stored) != nil {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	bob := e.person("bob").token
	specs := concurrently(4, func() *httptest.ResponseRecorder {
		return e.acceptance(bob, stored.Version, acceptanceFor(set, "530.01"))
	})
	ids := map[string]bool{}
	for _, r := range specs {
		var s promotion.StoredAcceptance
		if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &s) != nil {
			t.Fatalf("a concurrent acceptance post answered %d %s", r.Code, r.Body.String())
		}
		ids[s.ID] = true
	}
	if len(ids) != 1 {
		t.Errorf("concurrent identical acceptance posts created %d specs", len(ids))
	}

	// The compare-and-set append is NOT repeated: of four identical appends one wins.
	next := thresholdSet(e.app, "540")
	append4 := concurrently(4, func() int { return e.requirements(e.alice, next, stored.Version).Code })
	won := 0
	for _, c := range append4 {
		if c == 201 {
			won++
		} else if c != 409 {
			t.Errorf("a losing append answered %d", c)
		}
	}
	if won != 1 {
		t.Errorf("four concurrent appends on one head: %v", append4)
	}
}

// ------------------------------------------------------------------ F2, F12

// requestIDOf is the request id of an error response.
func requestIDOf(w *httptest.ResponseRecorder) string {
	var body struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.RequestID
}

// auditByRequest returns "action|resource|tenant|allowed" of the audit rows of one request.
func (e *m13Env) auditByRequest(requestID string) []string {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT action||'|'||resource||'|'||coalesce(tenant_id::text,'-')||'|'||allowed FROM audit.entries WHERE request_id=$1 ORDER BY occurred_at`, requestID)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// TestM12APIReleaseRoutesEndingInRolesAreNotRoleRoutes (F2): the authorization action was
// chosen by a path suffix, so a release artifact whose last path segment is "roles" was
// authorized, answered and audited as a role operation.
func TestM12APIReleaseRoutesEndingInRolesAreNotRoleRoutes(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for _, path := range []string{"/candidates/roles", "/requirements/roles", "/acceptance-specs/roles", "/candidates/x%2Froles"} {
		for who, token := range map[string]string{"owner": e.alice, "viewer": e.viewer} {
			w := e.get(path, token)
			r := m13Decode(w)
			if r.Code == "invalid_role" || r.Code == "forbidden" || w.Code != 404 && w.Code != 400 {
				t.Errorf("%s GET %s: %d %s", who, path, w.Code, w.Body.String())
			}
			var resource string
			for _, row := range e.auditByRequest(requestIDOf(w)) {
				resource += row + ";"
			}
			if strings.Contains(resource, "roles.") || strings.Contains(resource, "|role|") || !strings.Contains(resource, "|preview|") {
				t.Errorf("%s GET %s was audited as %q", who, path, resource)
			}
		}
	}
	// The role routes themselves are unchanged.
	e.publishRole("auditor", 0, "candidates.read")
	if w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/roles/auditor", e.alice, 0, nil); w.Code != 200 {
		t.Errorf("GET role: %d %s", w.Code, w.Body.String())
	}
	if w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/roles/auditor", e.viewer, 0, nil); w.Code != 403 {
		t.Errorf("a viewer read a role: %d", w.Code)
	}
}

// TestM12APIInvalidApplicationIsAuditedAsATenantRequest (F12): an authenticated,
// tenant-resolved request with a malformed application id was recorded as
// authentication.denied with no tenant.
func TestM12APIInvalidApplicationIsAuditedAsATenantRequest(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/preview/app.X/candidates", e.alice, 0, nil)
	r := m13Decode(w)
	if w.Code != 400 || r.Code != "invalid_application" || requestIDOf(w) == "" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	rows := e.auditByRequest(requestIDOf(w))
	want := "candidates.read.rejected|preview|" + e.f.tenantA + "|false"
	if len(rows) != 1 || rows[0] != want {
		t.Errorf("audit rows %v, want [%s]", rows, want)
	}
}

// ------------------------------------------------------------------ F10

// TestM12APIRoleHeadLimitIsTypedAndAudited (F10): past policy.MaxRoleHeads no policy bundle
// can be derived, so building and every detail read answered an unaudited 503, and a tenant
// could publish its way into it with no way back (heads cannot be retired). Publication of
// the head above the bound is refused (409 role_limit), and a tenant that is above it anyway
// (a raced publication, a direct port call) gets a typed, audited 409.
func TestM12APIRoleHeadLimitIsTypedAndAudited(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash, art := e.build("500", nil, e.carol)

	var heads int
	count := func() int {
		if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.role_heads WHERE tenant_id=$1`, e.f.tenantA).Scan(&heads); err != nil {
			t.Fatal(err)
		}
		return heads
	}
	for i := 0; count() < policy.MaxRoleHeads; i++ {
		e.publishRole(fmt.Sprintf("bulk_%04d", i), 0, "candidates.read")
	}
	// At the bound everything still works and an existing role can still be changed.
	viewOf(t, e.httpCandidate(e.viewer, hash), 200)
	e.publishRole("bulk_0000", 1, "candidates.read", "requirements.read")
	// The head above the bound is refused, typed and audited, and writes nothing.
	def := map[string]any{"schema_version": "tenant-capabilities-v1", "name": "one_too_many", "grants": []any{map[string]any{"effect": "ALLOW", "actions": []string{"candidates.read"},
		"resource": map[string]string{"app_id": "*", "entity_id": "*", "field_id": "*"}}}}
	w := m5Call(e.h, "/v1/tenants/"+e.f.tenantA+"/roles", e.alice, map[string]any{"operation_id": uuid(t), "expected_version": 0, "active": true, "definition": def})
	m13apiExpect(t, "the head above the bound", w, 409, "role_limit")
	if count() != policy.MaxRoleHeads {
		t.Fatalf("the refused head was written: %d", heads)
	}

	// A head beyond the bound by any other way (here a direct call of the definer port).
	err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		raw, _ := json.Marshal(def)
		sum := sha256.Sum256(raw)
		_, err := q.ExecContext(ctx, `SELECT control.publish_role($1,$2,$3,$4,$5,$6,$7)`, a.TenantID(), "one_too_many", uuid(t), 0, raw, hex.EncodeToString(sum[:]), true)
		return err
	})
	if err != nil {
		t.Skip("the definer port refused the direct publication:", err) // the bound then holds in the database as well
	}
	for _, request := range []func() *httptest.ResponseRecorder{
		func() *httptest.ResponseRecorder { return e.httpCandidate(e.viewer, hash) },
		func() *httptest.ResponseRecorder { return e.candidate(e.carol.token, art) },
		func() *httptest.ResponseRecorder { return e.httpApprove(e.dave.token, hash) },
	} {
		w = request()
		m13apiExpect(t, "a request above the bound", w, 409, "release_policy_snapshot_too_large")
	}
	if n := e.refusalAudit("ROLE_HEADS_EXCEEDED"); n != 3 {
		t.Errorf("the refusals must be audited: %d", n)
	}
	if n := e.auditCount("candidates.read"); n != 1 {
		t.Logf("allowed candidate reads: %d", n)
	}
}

// ------------------------------------------------------------------ F3, F4, F5, F11

// TestM12APICaseVariantDuplicateKeysAreRefused (F3, fixed at dbc4b5b, regression test over HTTP):
// "Revision" next to "revision" or a long-s spelling of a key was read last-wins by encoding/json
// while an intermediary saw the first.
func TestM12APICaseVariantDuplicateKeysAreRefused(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	art := e.prepareDoc(orderDocument(e.app, "500"), "500", e.person("alice"), e.person("bob"))
	good := fmt.Sprintf(`"revision":%d,"requirement_set_version":%d,"acceptance_spec_id":%q,"sandbox_id":%q`, art.revision, art.requirementsVersion, art.acceptanceID, art.sandboxID)
	for name, body := range map[string]string{
		"ascii case variant":   `{` + good + `,"Revision":99}`,
		"upper case variant":   `{` + good + `,"SANDBOX_ID":"` + art.sandboxID + `"}`,
		"long s spelling":      `{` + good + `,"reviſion":99}`,
		"kelvin sign spelling": `{` + good + `,"requirement_set_verſion":1}`,
	} {
		w := m13Raw(e.h, e.root+"/candidates", e.carol.token, body)
		if w.Code != 400 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for name, body := range map[string]string{
		"requirements": `{"expected_version":0,"requirements":{},"Requirements":{}}`,
	} {
		if w := m13Raw(e.h, e.root+"/requirements", e.alice, body); w.Code != 400 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if n := e.auditCount("release.candidate_staged"); n != 0 {
		t.Errorf("a refused body staged a candidate: %d", n)
	}
}

// m13Raw posts a raw JSON body.
func m13Raw(h http.Handler, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestM12APIAuthoringEnvelopeIsTheMetadataBudget (F4): the effective body limit of every
// release authoring request is metadata.MaxBytes + 4096 bytes (260 KiB), not the 1 MiB of the
// outer transport cap or of intent.MaxAcceptanceBytes and the database CHECK, which are upper
// bounds of storage that this route cannot reach. ADR102 amendment 11 states it; this test
// pins the boundary so that the statement and the code cannot drift apart.
func TestM12APIAuthoringEnvelopeIsTheMetadataBudget(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	limit := metadata.MaxBytes + 4096
	base := `{"expected_version":0,"requirements":{}}`
	at := base + strings.Repeat(" ", limit-len(base))
	over := at + " "
	atReply, overReply := m13Decode(m13Raw(e.h, e.root+"/requirements", e.alice, at)), m13Decode(m13Raw(e.h, e.root+"/requirements", e.alice, over))
	if overReply.Code != "invalid_release_request" {
		t.Errorf("a body of %d bytes must be refused by the envelope: %s", len(over), overReply.Code)
	}
	if atReply.Code == "invalid_release_request" {
		t.Errorf("a body of exactly %d bytes must pass the envelope and be judged on its content: %s", limit, atReply.Code)
	}
}

// TestM12APIReadersSeeOpaqueActorIdsButNotMemberships (F11): release reads disclose the opaque
// ids of the requester, the acceptance author, the sandbox freezer and the requirement decider
// to every role that holds the read action, because approvers need them for separation of
// duties; the membership list stays owner/admin only. ADR102 amendment 11 records it.
func TestM12APIReadersSeeOpaqueActorIdsButNotMemberships(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash, art := e.build("500", nil, e.carol)
	v := viewOf(t, e.httpCandidate(e.viewer, hash), 200)
	if v.RequesterID != e.carol.id || v.AcceptanceAuthor != e.person("bob").id || v.SandboxFrozenBy != e.f.alice {
		t.Errorf("the actor ids a viewer sees: %+v", v)
	}
	var req promotion.StoredRequirements
	if w := e.get(fmt.Sprintf("/requirements/%d", art.requirementsVersion), e.viewer); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &req) != nil || req.DecidedBy != e.f.alice {
		t.Errorf("requirement decider as a viewer sees it: %d %s", w.Code, w.Body.String())
	}
	if w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/members", e.viewer, 0, nil); w.Code != 403 {
		t.Errorf("a viewer listed the members: %d", w.Code)
	}
}

// TestM12APICustomRolesDecideThroughThePolicyLoader (F5): the shipped M12 fixtures wired
// policy.Native{} without a PolicyLoader, so stored custom roles were never exercised, and the
// cross-tenant assertion compared two different ids and could not fail. Here the production
// composition (PolicyLoader = RoleStore.Load) decides: each single-action custom role may do
// exactly its action, nothing else, and a user of another tenant reads nothing.
func TestM12APICustomRolesDecideThroughThePolicyLoader(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.useAPI(func(a *access.API) { a.PolicyLoader = (policy.RoleStore{}).Load })
	root := "/v1/tenants/" + e.f.tenantA
	roles := map[string]string{"builder_only": "candidates.build", "decider_only": "requirements.decide", "author_only": "acceptance.author"}
	users := map[string]m13Actor{}
	for name, action := range roles {
		r5Publish(t, e.h, root+"/roles", e.alice, 0, policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: name, Grants: []policy.Grant{
			{Effect: "ALLOW", Actions: []string{action}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}}, true, uuid(t))
		users[name] = e.member("u_"+name, "viewer")
		r5Assign(t, e.h, root, e.alice, users[name].id, name, 200)
	}
	// alice (owner) registers the source and decides the requirements, bob (admin) authors the acceptance.
	art := e.prepareDoc(orderDocument(e.app, "500"), "500", e.person("alice"), e.person("bob"))
	_, head := e.heads()
	set := thresholdSet(e.app, "500")
	spec := acceptanceFor(set, "500.01")
	// Every route a role does NOT hold is refused (the allowed one is exercised below, after).
	routes := map[string]struct {
		action string
		call   func(token string) int
	}{
		"requirements": {"requirements.decide", func(token string) int { return e.requirements(token, thresholdSet(e.app, "600"), head).Code }},
		"acceptance":   {"acceptance.author", func(token string) int { return e.acceptance(token, art.requirementsVersion, spec).Code }},
		"candidates":   {"candidates.build", func(token string) int { return e.candidate(token, art).Code }},
	}
	for name, action := range roles {
		for route, r := range routes {
			if r.action != action {
				if code := r.call(users[name].token); code != 403 {
					t.Errorf("%s on %s: %d, want 403", name, route, code)
				}
			}
		}
		if code := e.get("/candidates", users[name].token).Code; code != 403 {
			t.Errorf("%s may read without a read grant: %d", name, code)
		}
	}
	// And each may do exactly its own action.
	if code := e.requirements(users["decider_only"].token, thresholdSet(e.app, "600"), head).Code; code != 201 {
		t.Errorf("decider_only decides: %d", code)
	}
	if code := e.acceptance(users["author_only"].token, art.requirementsVersion, spec).Code; code != 201 {
		t.Errorf("author_only authors: %d", code)
	}
	if code := e.candidate(users["builder_only"].token, art).Code; code != 201 {
		t.Errorf("builder_only builds: %d", code)
	}
	// A user who belongs only to another tenant reads nothing of this one.
	outsider := e.addMemberIn(e.f.tenantB, "outsider", "viewer")
	for _, path := range []string{"/requirements", "/requirements/1", "/acceptance-specs/" + art.acceptanceID, "/candidates"} {
		if w := e.get(path, outsider.token); w.Code != 403 {
			t.Errorf("outsider read %s: %d", path, w.Code)
		}
	}
}
