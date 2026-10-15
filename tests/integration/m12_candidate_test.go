//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/sandbox"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------- fixtures

type m12Env struct {
	t                            *testing.T
	f                            *iamFixture
	h                            http.Handler
	app, root                    string
	alice, bob, viewer, outsider string // bearer tokens: owner, admin, viewer (tenant A), owner of tenant B
	release                      fixtureReleaseGate
}

func m12Setup(t *testing.T) *m12Env {
	t.Helper()
	m8Database(t)
	f := newIAM(t)
	ctx := context.Background()
	tx, err := f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = (tenant.Store{}).Provision(ctx, tx, f.tenantA, "Release workspace", f.bob, "admin", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	api := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	app := "app.orders_" + strings.ReplaceAll(uuid(t), "-", "")
	return &m12Env{t: t, f: f, h: h, app: app, root: "/v1/tenants/" + f.tenantA + "/preview/" + app,
		alice: f.token(t, f.aliceSubject, nil), bob: f.token(t, f.bobSubject, nil), viewer: f.token(t, f.viewerSubject, nil), outsider: f.token(t, f.bobSubject, nil)}
}

// orderDocument is the purchase-order application: start routes amount > threshold
// USD into an approval by admin and everything else straight to completion.
func orderDocument(app, threshold string) metadata.Document {
	version, namespace := metadata.WorkflowSchemaVersion, "example.orders"
	def := func(id, label, kind string) metadata.Definition {
		return metadata.Definition{ID: id, SchemaVersion: version, Namespace: namespace, Label: label, Kind: kind}
	}
	doc := metadata.Document{SchemaVersion: version, AppID: app, Namespace: namespace}
	entity := def("entity.order", "Order", "entity")
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
		{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + threshold + `","currency":"USD"}`)}}}
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

func (e *m12Env) register(token string, threshold string, expected int64) int64 {
	e.t.Helper()
	raw, _ := json.Marshal(orderDocument(e.app, threshold))
	w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", token, expected, raw)
	if w.Code != 201 {
		e.t.Fatal("register source", w.Code, w.Body.String())
	}
	return expected + 1
}

// sandbox creates and freezes a sandbox for the revision and returns its id.
func (e *m12Env) sandbox(token string, revision int64) string {
	e.t.Helper()
	witness := m9rDatasetWitness(e.t, e.h, fmt.Sprintf("%s/versions/%d", e.root, revision), token)
	box := m11Create(e.t, e.h, e.root, token, witness, "BLOCK", 200)
	if w := m5Call(e.h, e.root+"/sandboxes/"+box.ID+"/freeze", token, struct{}{}); w.Code != 200 {
		e.t.Fatal("freeze sandbox", w.Code, w.Body.String())
	}
	return box.ID
}

func thresholdSet(app, amount string) intent.RequirementSet {
	semantics, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: amount,
		ApproverRoles: []string{"admin"}, Context: map[string]json.RawMessage{"field.order.note": json.RawMessage(`"regular"`)}})
	return intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: app, Requirements: []intent.RequirementV2{{
		ID: "po-approval", Category: intent.CategoryApprovalThreshold, Requiredness: intent.Mandatory, RequirednessBasis: intent.BasisPolicyFloor,
		Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: semantics}}}
}

func acceptanceFor(set intent.RequirementSet, above string) intent.AcceptanceSpec {
	return intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{{
		ID: "above-threshold", RequirementID: "po-approval",
		Given:  map[string]json.RawMessage{"field.order.amount": json.RawMessage(`{"amount":"` + above + `","currency":"USD"}`)},
		Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}}}}
}

func (e *m12Env) post(path, token string, body any) *httptest.ResponseRecorder {
	return m5Call(e.h, e.root+path, token, body)
}
func (e *m12Env) get(path, token string) *httptest.ResponseRecorder {
	return m3Call(e.h, "GET", e.root+path, token, 0, nil)
}

func (e *m12Env) requirements(token string, set intent.RequirementSet, expected int64) *httptest.ResponseRecorder {
	raw, err := set.Canonical()
	if err != nil {
		e.t.Fatal(err)
	}
	return e.post("/requirements", token, map[string]any{"expected_version": expected, "requirements": json.RawMessage(raw)})
}

func (e *m12Env) acceptance(token string, version int64, spec intent.AcceptanceSpec) *httptest.ResponseRecorder {
	raw, err := spec.Canonical()
	if err != nil {
		e.t.Fatal(err)
	}
	return e.post("/acceptance-specs", token, map[string]any{"requirements_version": version, "acceptance": json.RawMessage(raw)})
}

type m12Artifacts struct {
	revision, requirementsVersion int64
	acceptanceID, sandboxID       string
}

// prepare registers a source revision and everything a candidate needs.
func (e *m12Env) prepare(threshold string, expectedRevision, expectedRequirements int64, sourceToken, acceptanceToken string) m12Artifacts {
	e.t.Helper()
	revision := e.register(sourceToken, threshold, expectedRevision)
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, threshold)
	w := e.requirements(e.alice, set, expectedRequirements)
	if w.Code != 201 {
		e.t.Fatal("requirements", w.Code, w.Body.String())
	}
	var stored promotion.StoredRequirements
	_ = json.Unmarshal(w.Body.Bytes(), &stored)
	above := threshold + ".01"
	w = e.acceptance(acceptanceToken, stored.Version, acceptanceFor(set, above))
	if w.Code != 201 {
		e.t.Fatal("acceptance", w.Code, w.Body.String())
	}
	var spec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &spec)
	return m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
}

func (a m12Artifacts) request() map[string]any {
	return map[string]any{"revision": a.revision, "requirement_set_version": a.requirementsVersion, "acceptance_spec_id": a.acceptanceID, "sandbox_id": a.sandboxID}
}

func (e *m12Env) candidate(token string, a m12Artifacts) *httptest.ResponseRecorder {
	return e.post("/candidates", token, a.request())
}

func viewOf(t *testing.T, w *httptest.ResponseRecorder, want int) access.CandidateView {
	t.Helper()
	if w.Code != want {
		t.Fatal("candidate response", w.Code, w.Body.String())
	}
	var v access.CandidateView
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("candidate contract", w.Body.String())
	}
	return v
}

// run executes fn as the given subject inside the runtime transaction envelope.
func (e *m12Env) run(subject string, fn func(context.Context, postgres.Executor, tenant.Access) error) error {
	ctx := context.Background()
	return postgres.WithRuntime(ctx, e.f.db, func(ctx context.Context, q postgres.Executor) error {
		p, err := e.f.verifier.Verify(e.f.token(e.t, subject, nil))
		if err != nil {
			return err
		}
		u, err := (identity.Store{}).Resolve(ctx, q, p)
		if err != nil {
			return err
		}
		if err = (tenant.Store{}).BindActor(ctx, q, u); err != nil {
			return err
		}
		a, err := (tenant.Store{}).Resolve(ctx, q, u, e.f.tenantA)
		if err != nil {
			return err
		}
		return fn(ctx, q, a)
	})
}

func sqlState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

func (e *m12Env) auditCount(action string) int {
	var n int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND allowed=$4 AND state->>'metadata_app'=$3`, e.f.tenantA, action, e.app, !strings.HasSuffix(action, ".rejected")).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// ------------------------------------------------------------------- tests

func TestM12CandidateBuilderEndToEndAuthorityAndReplay(t *testing.T) {
	e := m12Setup(t)
	art := e.prepare("500", 0, 0, e.alice, e.bob)

	// Authority-bearing fields are never accepted from a request.
	for _, key := range []string{"outcome", "high_risk", "requester", "requester_id", "provenance", "verification", "dependencies", "risk", "sandbox", "author", "base_release", "policy_hash", "tools_hash", "report"} {
		body := art.request()
		body[key] = "forged"
		if w := e.post("/candidates", e.alice, body); w.Code != 400 {
			t.Errorf("forged field %q accepted: %d", key, w.Code)
		}
	}
	// Roles: a viewer reads but cannot build or author; another tenant sees nothing.
	for _, c := range []struct {
		name string
		w    *httptest.ResponseRecorder
	}{
		{"viewer builds", e.candidate(e.viewer, art)},
		{"viewer decides requirements", e.requirements(e.viewer, thresholdSet(e.app, "500"), 1)},
		{"viewer authors acceptance", e.acceptance(e.viewer, art.requirementsVersion, acceptanceFor(thresholdSet(e.app, "500"), "500.01"))},
	} {
		if c.w.Code != 403 {
			t.Errorf("%s: %d", c.name, c.w.Code)
		}
	}
	for _, path := range []string{"/requirements", "/requirements/1", "/acceptance-specs/" + art.acceptanceID, "/candidates"} {
		if w := e.get(path, e.viewer); w.Code != 200 {
			t.Errorf("viewer read %s: %d", path, w.Code)
		}
		if w := m3Call(e.h, "GET", e.root+path, e.outsider, 0, nil); w.Code == 200 && e.f.bob == e.f.alice {
			t.Errorf("cross-tenant read %s", path)
		}
	}

	w := e.candidate(e.alice, art)
	c := viewOf(t, w, 201)
	if !c.Staged || !c.HighRisk || c.RiskLevel != "HIGH" || c.RequesterID != e.f.alice || c.AcceptanceAuthor != e.f.bob || c.SandboxFrozenBy != e.f.alice || c.BaseRelease != "" || c.StorageClass != "ADDITIVE" {
		t.Fatalf("%+v", c)
	}
	if len(c.Separation.Excluded) != 2 || c.Separation.Approvals != 1 || c.RequirementsVersion != 1 || c.AcceptanceID != art.acceptanceID || c.SandboxID != art.sandboxID {
		t.Fatalf("%+v", c)
	}
	// Replay returns the same artifact without staging twice.
	again := viewOf(t, e.candidate(e.alice, art), 200)
	if again.Staged || again.ArtifactHash != c.ArtifactHash {
		t.Fatalf("replay %+v", again)
	}
	detail := viewOf(t, e.get("/candidates/"+c.ArtifactHash, e.viewer), 200)
	if detail.Stale == nil || *detail.Stale || len(detail.VerificationReport) == 0 || detail.ArtifactHash != c.ArtifactHash {
		t.Fatalf("a fresh candidate must not be stale: %+v", detail)
	}
	if w := e.get("/candidates/"+strings.Repeat("0", 64), e.alice); w.Code != 404 {
		t.Fatal("unknown candidate", w.Code)
	}

	// The derived index and the stored bytes agree with the response.
	var level, requester, author, freezer string
	var high bool
	if err := e.f.operator.QueryRow(`SELECT risk_level,high_risk,requester_id::text,acceptance_author::text,sandbox_frozen_by::text FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, e.f.tenantA, e.app, c.ArtifactHash).Scan(&level, &high, &requester, &author, &freezer); err != nil {
		t.Fatal(err)
	}
	if level != "HIGH" || !high || requester != e.f.alice || author != e.f.bob || freezer != e.f.alice {
		t.Fatal("derived index", level, requester, author, freezer)
	}
	if n := e.auditCount("release.candidate_staged"); n != 1 {
		t.Fatalf("staged audit entries: %d", n)
	}
	if n := e.auditCount("requirements.decide"); n != 1 {
		t.Fatalf("requirement audit entries: %d", n)
	}

	// Requirement versions are compare-and-set: a stale writer conflicts.
	if w := e.requirements(e.alice, thresholdSet(e.app, "500"), 0); w.Code != 409 {
		t.Fatalf("stale requirement append: %d", w.Code)
	}
	if w := e.requirements(e.alice, thresholdSet("app.other", "500"), 1); w.Code != 400 {
		t.Fatalf("a requirement set for another application: %d", w.Code)
	}
}

func TestM12DatabaseEnforcesTheBoundariesTheAPIClaims(t *testing.T) {
	e := m12Setup(t)
	art := e.prepare("500", 0, 0, e.alice, e.bob)
	c := viewOf(t, e.candidate(e.alice, art), 201)

	// The runtime role cannot write any release artifact directly.
	for _, check := range []struct{ table, privilege string }{
		{"control.release_bundles", "INSERT"}, {"control.release_bundles", "UPDATE"}, {"control.release_bundles", "DELETE"}, {"control.release_bundles", "TRUNCATE"},
		{"control.requirement_sets", "INSERT"}, {"control.acceptance_specs", "INSERT"}, {"control.release_candidates", "INSERT"},
	} {
		var has bool
		if err := e.f.operator.QueryRow(`SELECT has_table_privilege('framework_app',$1,$2)`, check.table, check.privilege).Scan(&has); err != nil || has {
			t.Errorf("framework_app has %s on %s", check.privilege, check.table)
		}
	}
	var anyColumn bool
	if err := e.f.operator.QueryRow(`SELECT has_any_column_privilege('framework_app','control.release_bundles','INSERT')`).Scan(&anyColumn); err != nil || anyColumn {
		t.Fatal("framework_app can still insert release_bundles columns", err)
	}
	// The worker drift guard covers the new tables: a column grant must trip it.
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	for _, table := range []string{"control.requirement_sets", "control.acceptance_specs", "control.release_candidates"} {
		if _, err = e.f.operator.Exec(`GRANT SELECT(app_id) ON ` + table + ` TO framework_worker`); err != nil {
			t.Fatal(err)
		}
		var safe bool
		scanErr := worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe)
		if _, err = e.f.operator.Exec(`REVOKE SELECT(app_id) ON ` + table + ` FROM framework_worker`); err != nil {
			t.Fatal(err)
		}
		if scanErr != nil || safe {
			t.Fatal("worker drift guard missed a column grant on", table, scanErr)
		}
	}
	// Evidence is immutable even for the operator.
	for _, statement := range []string{
		`UPDATE control.release_bundles SET body=body WHERE app_id=$1`, `DELETE FROM control.release_bundles WHERE app_id=$1`,
		`UPDATE control.requirement_sets SET body=body WHERE app_id=$1`, `DELETE FROM control.requirement_sets WHERE app_id=$1`,
		`UPDATE control.acceptance_specs SET body=body WHERE app_id=$1`, `DELETE FROM control.acceptance_specs WHERE app_id=$1`,
		`UPDATE control.release_candidates SET risk_level='LOW' WHERE app_id=$1`, `DELETE FROM control.release_candidates WHERE app_id=$1`,
	} {
		if _, err := e.f.operator.Exec(statement, e.app); err == nil {
			t.Errorf("immutable evidence changed: %s", statement)
		}
	}
	for _, table := range []string{"control.release_bundles", "control.release_approvals", "control.requirement_sets", "control.acceptance_specs", "control.release_candidates"} {
		tx, err := e.f.operator.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`TRUNCATE ` + table + ` CASCADE`); err == nil {
			t.Errorf("TRUNCATE %s succeeded", table)
		}
		_ = tx.Rollback()
	}

	// Calling the port directly with a doctored body fails on database-known facts.
	var raw []byte
	if err := e.f.operator.QueryRow(`SELECT body FROM control.release_bundles WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, e.f.tenantA, e.app, c.ArtifactHash).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var b promotion.BundleV2
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	stage := func(subject string, mutate func(*promotion.BundleV2)) error {
		doctored := b
		mutate(&doctored)
		body, _ := json.Marshal(doctored)
		return e.run(subject, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
			var staged bool
			return q.QueryRowContext(ctx, `SELECT control.stage_candidate($1,$2,$3)`, e.app, promotion.Hash(body), body).Scan(&staged)
		})
	}
	other := strings.Repeat("e", 64)
	denials := map[string]struct {
		subject string
		mutate  func(*promotion.BundleV2)
		state   string
	}{
		"high risk flag cleared":          {e.f.aliceSubject, func(x *promotion.BundleV2) { x.HighRisk = false }, "22023"},
		"level downgraded, flag kept":     {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Risk.Level = "LOW" }, "22023"},
		"requester is someone else":       {e.f.aliceSubject, func(x *promotion.BundleV2) { x.RequesterID = e.f.bob }, "22023"},
		"another tenant":                  {e.f.aliceSubject, func(x *promotion.BundleV2) { x.TenantID = e.f.tenantB }, "22023"},
		"another application":             {e.f.aliceSubject, func(x *promotion.BundleV2) { x.AppID = "app.other" }, "22023"},
		"bundle version":                  {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Version = "1.0.0" }, "22023"},
		"verification outcome":            {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Verification.Outcome = "BLOCKED" }, "22023"},
		"requirement set row mismatch":    {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Requirements.Hash = other }, "55000"},
		"requirement set version missing": {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Requirements.Version = 9 }, "55000"},
		"acceptance row mismatch":         {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Acceptance.Hash = other }, "55000"},
		"acceptance author claimed":       {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Acceptance.AuthorID = e.f.alice }, "55000"},
		"acceptance bound to another set": {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Acceptance.RequirementsHash = other }, "55000"},
		"sandbox evidence hash":           {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Sandbox.EvidenceHash = other }, "55000"},
		"sandbox evidence text":           {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Sandbox.Evidence += " " }, "55000"},
		"sandbox freezer claimed":         {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Sandbox.FrozenBy = e.f.bob }, "55000"},
		"sandbox for another ir":          {e.f.aliceSubject, func(x *promotion.BundleV2) { x.IR.Hash = other }, "55000"},
		"source revision missing":         {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Source.Revision = 9 }, "55000"},
		"source hash":                     {e.f.aliceSubject, func(x *promotion.BundleV2) { x.Source.Hash = other }, "55000"},
		"base claimed but none active":    {e.f.aliceSubject, func(x *promotion.BundleV2) { x.BaseRelease, x.BaseGeneration = other, 1 }, "55000"},
		"viewer cannot stage":             {e.f.viewerSubject, func(x *promotion.BundleV2) {}, "42501"},
		"requester must be the actor":     {e.f.bobSubject, func(x *promotion.BundleV2) {}, "22023"},
	}
	for name, d := range denials {
		err := stage(d.subject, d.mutate)
		if err == nil || sqlState(err) != d.state {
			t.Errorf("%s: %v (want %s)", name, err, d.state)
		}
	}
	// The untouched body is accepted as an idempotent restage.
	if err := stage(e.f.aliceSubject, func(*promotion.BundleV2) {}); err != nil {
		t.Fatalf("idempotent restage: %v", err)
	}
}

func TestM12BuildTimeSeparationOfDuties(t *testing.T) {
	e := m12Setup(t)
	// The requester authors the acceptance: refused by the builder.
	own := e.prepare("500", 0, 0, e.alice, e.alice)
	if w := e.candidate(e.alice, own); w.Code != 400 {
		t.Fatalf("self-authored acceptance: %d %s", w.Code, w.Body.String())
	}
	// The source author authors the acceptance (bob wrote revision 2 and its
	// acceptance, alice requests): the database port refuses it.
	art := e.prepare("500", 1, 1, e.bob, e.bob)
	if w := e.candidate(e.alice, art); w.Code != 403 {
		t.Fatalf("source author authored the acceptance: %d %s", w.Code, w.Body.String())
	}
	if n := e.auditCount("candidates.build.rejected"); n < 2 {
		t.Fatalf("rejections must be audited: %d", n)
	}
	var staged int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&staged); err != nil || staged != 0 {
		t.Fatal("a refused candidate was staged", staged, err)
	}
}

func TestM12WrongBehaviorIsRejectedWithEvidence(t *testing.T) {
	e := m12Setup(t)
	// The source routes only amounts above 5000 to approval; the independent
	// requirement says 500. The verifier must catch it and nothing is staged.
	revision := e.register(e.alice, "5000", 0)
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, "500")
	var stored promotion.StoredRequirements
	w := e.requirements(e.alice, set, 0)
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &stored) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	var spec promotion.StoredAcceptance
	w = e.acceptance(e.bob, stored.Version, acceptanceFor(set, "500.01"))
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &spec) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	art := m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
	w = e.candidate(e.alice, art)
	if w.Code != 422 {
		t.Fatalf("a wrong implementation was accepted: %d %s", w.Code, w.Body.String())
	}
	var rejection struct {
		Reason string `json:"reason"`
		Report struct {
			Outcome  string `json:"outcome"`
			Findings []struct {
				Code   string `json:"code"`
				CaseID string `json:"case_id"`
			} `json:"findings"`
		} `json:"verification_report"`
		Assessment struct {
			Level string `json:"level"`
		} `json:"risk_assessment"`
	}
	if json.Unmarshal(w.Body.Bytes(), &rejection) != nil || rejection.Reason != promotion.RejectVerificationFailed || rejection.Report.Outcome != "FAIL" || len(rejection.Report.Findings) == 0 || rejection.Report.Findings[0].Code != "APPROVAL_MISMATCH" {
		t.Fatalf("%s", w.Body.String())
	}
	if n := e.auditCount("candidates.build.rejected"); n != 1 {
		t.Fatalf("rejection audit: %d", n)
	}

	// A money change with no requirement at all is blocked, not accepted.
	empty := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: e.app, Requirements: []intent.RequirementV2{}}
	w = e.requirements(e.alice, empty, stored.Version)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var emptyStored promotion.StoredRequirements
	_ = json.Unmarshal(w.Body.Bytes(), &emptyStored)
	emptySpec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: empty.Hash(), Cases: []intent.AcceptanceCase{}}
	w = e.acceptance(e.bob, emptyStored.Version, emptySpec)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var emptyAcceptance promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &emptyAcceptance)
	w = e.candidate(e.alice, m12Artifacts{revision: revision, requirementsVersion: emptyStored.Version, acceptanceID: emptyAcceptance.ID, sandboxID: sandbox})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "OMITTED_REQUIREMENT") || !strings.Contains(w.Body.String(), promotion.RejectVerificationBlocked) {
		t.Fatalf("omitted requirement: %d %s", w.Code, w.Body.String())
	}
	// An acceptance case that contradicts the requirement is refused at authoring.
	wrong := acceptanceFor(set, "500.01")
	wrong.Cases[0].Expect = intent.Expectation{Outcome: intent.OutcomeNoApproval}
	if w = e.acceptance(e.bob, stored.Version, wrong); w.Code != 400 || !strings.Contains(w.Body.String(), "ORACLE_DISAGREES") {
		t.Fatalf("oracle: %d %s", w.Code, w.Body.String())
	}
	var staged int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&staged); err != nil || staged != 0 {
		t.Fatal("a rejected candidate was staged", staged, err)
	}
}

func TestM12ApprovalActivationStalenessAndRollbackOnBuilderCandidates(t *testing.T) {
	e := m12Setup(t)
	store := promotion.Store{}
	gate := fixtureReleaseGate(true)
	supported := compiler.SupportedIRVersions()

	one := e.prepare("500", 0, 0, e.alice, e.bob)
	c1 := viewOf(t, e.candidate(e.alice, one), 201)
	load := func(hash string) promotion.Candidate {
		t.Helper()
		var c promotion.Candidate
		if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
			var err error
			c, _, err = store.LoadCandidate(ctx, q, a, e.app, hash, supported)
			return err
		}); err != nil {
			t.Fatal("load candidate", err)
		}
		return c
	}
	cand1 := load(c1.ArtifactHash)

	// The requester cannot approve their own HIGH risk candidate (Go and SQL).
	if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		_, err := store.Approve(ctx, q, a, cand1, cand1.Hash(), gate)
		return err
	}); err == nil {
		t.Fatal("self approval")
	}
	var approval1 promotion.Approval
	if err := e.run(e.f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var err error
		approval1, err = store.Approve(ctx, q, a, cand1, cand1.Hash(), gate)
		return err
	}); err != nil {
		t.Fatal("independent approval", err)
	}
	if err := e.run(e.f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		return store.Activate(ctx, q, a, cand1, approval1, 0, "", cand1.Header().Dependencies, gate)
	}); err != nil {
		t.Fatal("first activation", err)
	}

	// c1 was built as a first release; with an active release it is now stale.
	d1 := viewOf(t, e.get("/candidates/"+c1.ArtifactHash, e.alice), 200)
	if d1.Stale == nil || !*d1.Stale || !hasReason(d1.StaleReasons, promotion.StaleBaseChanged) {
		t.Fatalf("%+v", d1)
	}

	// Release 2 evolves from the active release; the base is derived, not claimed.
	two := e.prepare("600", 1, 1, e.alice, e.bob)
	body := two.request()
	body["expected_base"] = strings.Repeat("0", 64)
	if w := e.post("/candidates", e.alice, body); w.Code != 409 {
		t.Fatalf("a wrong asserted base: %d %s", w.Code, w.Body.String())
	}
	body["expected_base"] = ""
	if w := e.post("/candidates", e.alice, body); w.Code != 409 {
		t.Fatalf("an asserted empty base while a release is active: %d", w.Code)
	}
	body["expected_base"] = c1.ArtifactHash
	c2 := viewOf(t, e.post("/candidates", e.alice, body), 201)
	if c2.BaseRelease != c1.ArtifactHash || c2.BaseGeneration != 1 || c2.StorageClass != "EQUAL" || !c2.HighRisk {
		t.Fatalf("%+v", c2)
	}
	cand2 := load(c2.ArtifactHash)
	if d2 := viewOf(t, e.get("/candidates/"+c2.ArtifactHash, e.alice), 200); d2.Stale == nil || *d2.Stale {
		t.Fatalf("a fresh second candidate is stale: %+v", d2)
	}
	// The diff and storage class recompute from the actual base bytes.
	var baseProgram compiler.Program
	if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var err error
		baseProgram, _, _, err = store.Active(ctx, q, a, e.app, supported)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := cand2.VerifyAgainstBase(baseProgram, supported); err != nil {
		t.Fatal("base recomputation:", err)
	}

	// A policy change (a new tenant role) makes the candidate stale.
	role := `{"schema_version":"tenant-capabilities-v1","name":"auditor","grants":[{"effect":"ALLOW","actions":["candidates.read"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		_, err := (policy.RoleStore{}).Publish(ctx, q, a, uuid(t), 0, []byte(role), true)
		return err
	}); err != nil {
		t.Fatal("publish role", err)
	}
	d2 := viewOf(t, e.get("/candidates/"+c2.ArtifactHash, e.alice), 200)
	if d2.Stale == nil || !*d2.Stale || !hasReason(d2.StaleReasons, promotion.StalePolicyChanged) {
		t.Fatalf("a role change must stale the candidate: %+v", d2)
	}

	// Approval and activation of the second release, then compatibility and rollback.
	var approval2 promotion.Approval
	if err := e.run(e.f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var err error
		if approval2, err = store.Approve(ctx, q, a, cand2, cand2.Hash(), gate); err != nil {
			return err
		}
		return store.Activate(ctx, q, a, cand2, approval2, 1, cand1.Hash(), cand2.Header().Dependencies, gate)
	}); err != nil {
		t.Fatal("second activation", err)
	}
	if err := e.run(e.f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		return store.Activate(ctx, q, a, cand1, approval1, 2, cand2.Hash(), cand1.Header().Dependencies, gate)
	}); err == nil {
		t.Fatal("an approval of one artifact activated another over a moved base")
	}
	if err := promotion.CheckActiveCompatibility(context.Background(), e.f.operator, supported); err != nil {
		t.Fatal("supported active IR", err)
	}
	if err := promotion.CheckActiveCompatibility(context.Background(), e.f.operator, []string{"2.0.0"}); err == nil {
		t.Fatal("unsupported platform deployment accepted")
	}
	plan := promotion.RollbackPlan{StorageCompatible: true, ExternalEffectsReviewed: true, PlanHash: strings.Repeat("c", 64)}
	if err := e.run(e.f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		if e := store.Rollback(ctx, q, a, cand1, cand2.Hash(), 2, plan, gate); e != nil {
			return e
		}
		p, h, gen, e := store.Active(ctx, q, a, e.app, supported)
		if e != nil {
			return e
		}
		if h != cand1.Hash() || gen != 3 || p.Hash() != cand1.Header().IRHash {
			t.Fatal("rollback restored the wrong release")
		}
		return nil
	}); err != nil {
		t.Fatal("rollback", err)
	}
	d2 = viewOf(t, e.get("/candidates/"+c2.ArtifactHash, e.alice), 200)
	if d2.Stale == nil || !*d2.Stale {
		t.Fatal("a candidate whose base is no longer active must be stale")
	}
	// A bundle 1.0.0 can no longer be staged by a tenant.
	legacy := legacyBundle(t, e)
	if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		_, err := store.Stage(ctx, q, a, legacy, gate)
		return err
	}); !errors.Is(err, promotion.ErrGate) {
		t.Fatalf("bundle 1.0.0 staging: %v", err)
	}
}

// legacyBundle builds a bundle 1.0.0 the way the retired in-process composer did.
func legacyBundle(t *testing.T, e *m12Env) promotion.Candidate {
	t.Helper()
	raw, err := json.Marshal(orderDocument(e.app, "500"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := m3Registry(t).Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostics := compiler.Compile(source, compiler.PlatformCatalog())
	if !program.Valid() {
		t.Fatal(diagnostics)
	}
	i := intent.Contract{Version: "1.0.0", Requirements: []intent.Requirement{{ID: "approval", Mandatory: true, Status: "RESOLVED", EntityID: "entity.order", FieldID: "field.order.amount", Operator: "GT", Threshold: "500", Currency: "USD", ApproverRole: "admin", Trigger: "submit"}}}
	acceptance := intent.Acceptance{Version: "1.0.0", IntentHash: i.Hash(), AuthorActorID: e.f.bob, Provenance: "HUMAN_INDEPENDENT", Cases: []intent.Case{{RequirementID: "approval", Input: "501 USD", Expected: "admin approval"}}}
	acceptanceRaw, _ := json.Marshal(acceptance)
	h := strings.Repeat("a", 64)
	c, err := promotion.Build(source, program, promotion.Bundle{Version: "1.0.0", RequesterID: e.f.alice, HighRisk: true,
		Dependencies: promotion.Dependencies{PolicyHash: h, ToolsHash: h, SchemaHash: h}, Intent: i, Acceptance: acceptance,
		Sandbox:      sandbox.Context{ID: "sandbox.test", DataMode: "SYNTHETIC_SEPARATE", ToolMode: "BLOCK"},
		Verification: promotion.Verification{Version: "1.0.0", SandboxID: "sandbox.test", IntentHash: i.Hash(), AcceptanceHash: promotion.Hash(acceptanceRaw), ReportHash: h, Outcome: "PASS"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func hasReason(list []promotion.StaleReason, want promotion.StaleReason) bool {
	for _, r := range list {
		if r == want {
			return true
		}
	}
	return false
}
