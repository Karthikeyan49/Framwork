//go:build integration

package integration

// Shared multi-actor fixture for M13 (ADR039: approval, rejection, activation
// and rollback). It extends the M12 fixture with extra tenant-A members, a
// doctored-candidate stager, raw callers for the four database ports, HTTP
// helpers for the six ADR039 routes and operator-side readers. Everything is raw
// SQL and HTTP so it compiles without the M13 Go store or handlers.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

const m13Issuer = "https://test-issuer.example"

// m13Actor is one person: user id, token subject and a bearer token.
type m13Actor struct{ name, id, subject, token string }

// m13Env is the M12 fixture plus carol, dave and erin (admins of tenant A).
// Persons by name: alice (owner A), bob (admin A, owner B), viewer (viewer A),
// carol, dave, erin and anyone added with addMember.
type m13Env struct {
	*m12Env
	carol, dave, erin m13Actor
	people            map[string]m13Actor
	tmpl              *m13Template
}

// m13Template is a real, verified HIGH first-release candidate requested by
// carol; source author, requirement decider and sandbox freezer alice;
// acceptance author bob. Doctored candidates are derived from its body.
type m13Template struct {
	hash      string
	raw       []byte
	requester m13Actor
	art       m12Artifacts
}

func m13Setup(t *testing.T) *m13Env {
	t.Helper()
	e := &m13Env{m12Env: m12Setup(t), people: map[string]m13Actor{}}
	e.people["alice"] = m13Actor{"alice", e.f.alice, e.f.aliceSubject, e.alice}
	e.people["bob"] = m13Actor{"bob", e.f.bob, e.f.bobSubject, e.bob}
	e.people["viewer"] = m13Actor{"viewer", e.f.viewer, e.f.viewerSubject, e.viewer}
	e.carol = e.member("carol", "admin")
	e.dave = e.member("dave", "admin")
	e.erin = e.member("erin", "admin")
	return e
}

// ------------------------------------------------------------------ members

// addMember provisions a new user with an active tenant-A membership through
// the operator connection (the same approach newIAM uses).
func (e *m13Env) addMember(name, role string) (userID, subject, token string) {
	a := e.addMemberIn(e.f.tenantA, name, role)
	return a.id, a.subject, a.token
}

func (e *m13Env) member(name, role string) m13Actor {
	e.t.Helper()
	e.addMember(name, role)
	return e.people[name]
}

// addMemberIn provisions a new user with an active membership of any tenant.
func (e *m13Env) addMemberIn(tenantID, name, role string) m13Actor {
	t := e.t
	t.Helper()
	ctx := context.Background()
	subject := uuid(t)
	tx, err := e.f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("operator transaction unavailable")
	}
	defer tx.Rollback()
	id, err := (identity.Store{}).Provision(ctx, tx, m13Issuer, subject, name, true)
	if err != nil {
		t.Fatal("provision user", err)
	}
	if err = (tenant.Store{}).Provision(ctx, tx, tenantID, "Release workspace", id, role, true); err != nil {
		t.Fatal("provision membership", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.f.operator.Exec(`DELETE FROM iam.memberships WHERE user_id=$1`, id)
		_, _ = e.f.operator.Exec(`DELETE FROM iam.users WHERE id=$1`, id)
	})
	a := m13Actor{name: name, id: id, subject: subject, token: e.f.token(t, subject, nil)}
	e.people[name] = a
	return a
}

// person returns a named actor; unknown names fail the test.
func (e *m13Env) person(name string) m13Actor {
	e.t.Helper()
	a, ok := e.people[name]
	if !ok {
		e.t.Fatal("unknown person", name)
	}
	return a
}

// fresh re-signs an actor's token (tokens expire after 15 minutes).
func (e *m13Env) fresh(a m13Actor) m13Actor {
	a.token = e.f.token(e.t, a.subject, nil)
	return a
}

func (e *m13Env) operatorExec(what, query string, args ...any) {
	e.t.Helper()
	r, err := e.f.operator.Exec(query, args...)
	if err != nil {
		e.t.Fatal(what, err)
	}
	if n, _ := r.RowsAffected(); n != 1 {
		e.t.Fatalf("%s: %d rows changed", what, n)
	}
}

// setRole changes a tenant-A member's role mid-test (operator SQL).
func (e *m13Env) setRole(userID, role string) {
	e.t.Helper()
	e.operatorExec("set role", `UPDATE iam.memberships SET role=$3 WHERE tenant_id=$1 AND user_id=$2`, e.f.tenantA, userID, role)
}

// setMemberActive deactivates (removes) or reactivates a tenant-A membership.
func (e *m13Env) setMemberActive(userID string, active bool) {
	e.t.Helper()
	e.operatorExec("set membership", `UPDATE iam.memberships SET active=$3 WHERE tenant_id=$1 AND user_id=$2`, e.f.tenantA, userID, active)
}

// setUserActive disables or re-enables the user account itself.
func (e *m13Env) setUserActive(userID string, active bool) {
	e.t.Helper()
	e.operatorExec("set user", `UPDATE iam.users SET active=$2 WHERE id=$1`, userID, active)
}

// ---------------------------------------------------------------- candidates

func (e *m13Env) heads() (revision, requirements int64) {
	e.t.Helper()
	if err := e.f.operator.QueryRow(`SELECT (SELECT coalesce(max(revision),0) FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2),
 (SELECT coalesce(max(version),0) FROM control.requirement_sets WHERE tenant_id=$1 AND app_id=$2)`, e.f.tenantA, e.app).Scan(&revision, &requirements); err != nil {
		e.t.Fatal("heads", err)
	}
	return revision, requirements
}

// prepareDoc registers doc as the next source revision (author source), freezes
// a sandbox for it (alice), appends the threshold requirement set (alice) and
// has author write the acceptance spec.
func (e *m13Env) prepareDoc(doc metadata.Document, threshold string, source, author m13Actor) m12Artifacts {
	t := e.t
	t.Helper()
	revision, requirements := e.heads()
	raw, _ := json.Marshal(doc)
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", source.token, revision, raw); w.Code != 201 {
		t.Fatal("register source", w.Code, w.Body.String())
	}
	revision++
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, threshold)
	w := e.requirements(e.alice, set, requirements)
	if w.Code != 201 {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	var stored promotion.StoredRequirements
	_ = json.Unmarshal(w.Body.Bytes(), &stored)
	w = e.acceptance(author.token, stored.Version, acceptanceFor(set, threshold+".01"))
	if w.Code != 201 {
		t.Fatal("acceptance", w.Code, w.Body.String())
	}
	var spec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &spec)
	return m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
}

// build stages a real candidate through the M12 HTTP builder: the order
// document with threshold (optionally changed by edit), source author alice,
// acceptance author bob, requested by requester. The base is the active release.
func (e *m13Env) build(threshold string, edit func(*metadata.Document), requester m13Actor) (string, m12Artifacts) {
	e.t.Helper()
	doc := orderDocument(e.app, threshold)
	if edit != nil {
		edit(&doc)
	}
	art := e.prepareDoc(doc, threshold, e.person("alice"), e.person("bob"))
	w := e.candidate(requester.token, art)
	if w.Code != 201 {
		e.t.Fatal("build candidate", w.Code, w.Body.String())
	}
	var v struct {
		Hash string `json:"artifact_hash"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || !promotion.ValidHash(v.Hash) {
		e.t.Fatal("candidate contract", w.Body.String())
	}
	return v.Hash, art
}

// bundle reads a stored bundle body through the operator connection.
func (e *m13Env) bundle(hash string) []byte {
	e.t.Helper()
	var raw []byte
	if err := e.f.operator.QueryRow(`SELECT body FROM control.release_bundles WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, e.f.tenantA, e.app, hash).Scan(&raw); err != nil {
		e.t.Fatal("bundle", err)
	}
	return raw
}

// template builds the shared real HIGH candidate once (see m13Template).
func (e *m13Env) template() *m13Template {
	e.t.Helper()
	if e.tmpl == nil {
		hash, art := e.build("500", nil, e.carol)
		e.tmpl = &m13Template{hash: hash, raw: e.bundle(hash), requester: e.carol, art: art}
	}
	return e.tmpl
}

// stageDoctored stages a modified copy of the template body directly through
// control.stage_candidate, as the template requester (carol), exactly as a
// compromised API process could. Before mutate runs, the copy is bound to the
// current active pointer and its RequesterID is set to the acting user; mutate
// has the last word. Returns the new artifact hash.
func (e *m13Env) stageDoctored(mutate func(*promotion.BundleV2)) (string, error) {
	return e.stageDoctoredAs(e.template().requester, mutate)
}

func (e *m13Env) stageDoctoredAs(actor m13Actor, mutate func(*promotion.BundleV2)) (string, error) {
	e.t.Helper()
	var b promotion.BundleV2
	if err := json.Unmarshal(e.template().raw, &b); err != nil { // deep copy
		e.t.Fatal(err)
	}
	b.BaseRelease, b.BaseGeneration, _ = e.activePointer()
	b.RequesterID = actor.id
	if mutate != nil {
		mutate(&b)
	}
	body, err := json.Marshal(b)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.stageRaw(actor, body)
}

// stageRaw calls control.stage_candidate with exact bytes.
func (e *m13Env) stageRaw(actor m13Actor, body []byte) (string, error) {
	hash := promotion.Hash(body)
	var staged bool
	err := e.portIn(actor.subject, e.f.tenantA, &staged, `SELECT control.stage_candidate($1,$2,$3)`, e.app, hash, body)
	return hash, err
}

// stageLevel stages a doctored candidate whose Risk.Level and HighRisk are
// consistently set to level; a nonce reason makes every call a new hash.
func (e *m13Env) stageLevel(level string) (string, error) {
	return e.stageLevelAs(e.template().requester, level)
}

func (e *m13Env) stageLevelAs(actor m13Actor, level string) (string, error) {
	nonce := "m13-nonce-" + uuid(e.t)
	return e.stageDoctoredAs(actor, func(b *promotion.BundleV2) {
		b.Risk.Level = level
		b.HighRisk = level == "HIGH" || level == "CRITICAL"
		b.Risk.Reasons = append(append([]string{}, b.Risk.Reasons...), nonce)
	})
}

// ----------------------------------------------------------------- raw ports

// bindDirect binds a runtime transaction to subject in tenantID by setting the
// identity GUCs directly. This is what any API process, honest or compromised,
// can do: no Go membership or policy check runs, so only the database decides.
func (e *m13Env) bindDirect(ctx context.Context, q postgres.Executor, subject, tenantID string) error {
	var id string
	if err := e.f.operator.QueryRowContext(ctx, `SELECT id::text FROM iam.users WHERE issuer=$1 AND subject=$2`, m13Issuer, subject).Scan(&id); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `SELECT pg_catalog.set_config('app.auth_issuer',$1,true),pg_catalog.set_config('app.auth_subject',$2,true),
 pg_catalog.set_config('app.actor_id',$3,true),pg_catalog.set_config('app.tenant_id',$4,true)`, m13Issuer, subject, id, tenantID)
	return err
}

// portIn runs one single-value query as subject in tenantID (direct binding) and
// commits when it succeeds.
func (e *m13Env) portIn(subject, tenantID string, dest any, query string, args ...any) error {
	return postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, subject, tenantID); err != nil {
			return err
		}
		return q.QueryRowContext(ctx, query, args...).Scan(dest)
	})
}

func (e *m13Env) approvePort(subject, hash string) (int, error) {
	var n int
	err := e.portIn(subject, e.f.tenantA, &n, `SELECT control.approve_release($1,$2)`, e.app, hash)
	return n, err
}

func (e *m13Env) rejectPort(subject, hash, reason, note string) (bool, error) {
	var created bool
	err := e.portIn(subject, e.f.tenantA, &created, `SELECT control.reject_release($1,$2,$3,$4)`, e.app, hash, reason, note)
	return created, err
}

func (e *m13Env) activatePort(subject, hash, expectedBase string, expectedGeneration int64) (int64, error) {
	var generation int64
	err := e.portIn(subject, e.f.tenantA, &generation, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, hash, expectedBase, expectedGeneration)
	return generation, err
}

func (e *m13Env) rollbackPort(subject, target, expectedCurrent string, expectedGeneration int64, planHash string, planAck bool) (int64, error) {
	var generation int64
	err := e.portIn(subject, e.f.tenantA, &generation, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, e.app, target, expectedCurrent, expectedGeneration, planHash, planAck)
	return generation, err
}

// --------------------------------------------------------------------- HTTP

// m13Plan is the rollback acknowledgement body of ADR039 section 7.
type m13Plan struct {
	StorageCompatible       bool   `json:"storage_compatible"`
	ExternalEffectsReviewed bool   `json:"external_effects_reviewed"`
	RecoveryRequired        bool   `json:"recovery_required"`
	PlanHash                string `json:"plan_hash"`
}

func m13SafePlan() *m13Plan {
	return &m13Plan{StorageCompatible: true, ExternalEffectsReviewed: true, PlanHash: strings.Repeat("c", 64)}
}

func (e *m13Env) postAt(root, path, token string, body any) *httptest.ResponseRecorder {
	return m5Call(e.h, root+path, token, body)
}

func (e *m13Env) getAt(root, path, token string) *httptest.ResponseRecorder {
	return m3Call(e.h, "GET", root+path, token, 0, nil)
}

func (e *m13Env) httpApprove(token, hash string) *httptest.ResponseRecorder {
	return e.post("/candidates/"+hash+"/approve", token, map[string]any{"artifact_hash": hash})
}

func (e *m13Env) httpReject(token, hash, reason, note string) *httptest.ResponseRecorder {
	return e.post("/candidates/"+hash+"/reject", token, map[string]any{"artifact_hash": hash, "reason": reason, "note": note})
}

func (e *m13Env) httpActivate(token, hash, expectedBase string, expectedGeneration int64) *httptest.ResponseRecorder {
	return e.post("/candidates/"+hash+"/activate", token, map[string]any{"artifact_hash": hash, "expected_base": expectedBase, "expected_generation": expectedGeneration})
}

func (e *m13Env) httpRollback(token, target, expectedCurrent string, expectedGeneration int64, plan *m13Plan) *httptest.ResponseRecorder {
	body := map[string]any{"target": target, "expected_current": expectedCurrent, "expected_generation": expectedGeneration}
	if plan != nil {
		body["plan"] = plan
	}
	return e.post("/releases/rollback", token, body)
}

func (e *m13Env) httpHistory(token string) *httptest.ResponseRecorder {
	return e.get("/releases", token)
}

func (e *m13Env) httpActive(token string) *httptest.ResponseRecorder {
	return e.get("/releases/active", token)
}

func (e *m13Env) httpCandidate(token, hash string) *httptest.ResponseRecorder {
	return e.get("/candidates/"+hash, token)
}

// m13Reply is the generic decoded JSON reply (error code, stale reasons and the
// ADR039 candidate detail fields), decoded without the Go response types.
type m13Reply struct {
	Code              string            `json:"code"`
	Message           string            `json:"message"`
	Reasons           []string          `json:"reasons"`
	StaleReasons      []string          `json:"stale_reasons"`
	Status            string            `json:"status"`
	ArtifactHash      string            `json:"artifact_hash"`
	RiskLevel         string            `json:"risk_level"`
	Stale             *bool             `json:"stale"`
	Approvals         []json.RawMessage `json:"approvals"`
	ApprovalsRequired int               `json:"approvals_required"`
	Rejections        []json.RawMessage `json:"rejections"`
	Active            json.RawMessage   `json:"active"`
	Generation        int64             `json:"generation"`
}

func m13Decode(w *httptest.ResponseRecorder) m13Reply {
	var r m13Reply
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if len(r.Reasons) == 0 {
		r.Reasons = r.StaleReasons
	}
	return r
}

// ------------------------------------------------------------------ readers

// activePointer reads the active release of e.app (operator connection).
func (e *m13Env) activePointer() (hash string, generation int64, ok bool) {
	e.t.Helper()
	err := e.f.operator.QueryRow(`SELECT artifact_hash,generation FROM control.active_releases WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&hash, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false
	}
	if err != nil {
		e.t.Fatal("active pointer", err)
	}
	return hash, generation, true
}

type m13HistoryRow struct {
	seq, generation                                                int64
	kind, hash, previous, actor, riskLevel, storageClass, planHash string
}

// historyRows reads control.release_history of e.app in seq order.
func (e *m13Env) historyRows() []m13HistoryRow {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT seq,kind,artifact_hash,previous_hash,generation,actor_id::text,coalesce(risk_level,''),coalesce(storage_class,''),coalesce(plan_hash,'')
 FROM control.release_history WHERE tenant_id=$1 AND app_id=$2 ORDER BY seq`, e.f.tenantA, e.app)
	if err != nil {
		e.t.Fatal("history", err)
	}
	defer rows.Close()
	var out []m13HistoryRow
	for rows.Next() {
		var h m13HistoryRow
		if err = rows.Scan(&h.seq, &h.kind, &h.hash, &h.previous, &h.generation, &h.actor, &h.riskLevel, &h.storageClass, &h.planHash); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, h)
	}
	if rows.Err() != nil {
		e.t.Fatal(rows.Err())
	}
	return out
}

func (e *m13Env) actorsOf(table, hash string) []string {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT actor_id::text FROM `+table+` WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, e.f.tenantA, e.app, hash)
	if err != nil {
		e.t.Fatal(table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// approvalsOf and rejectionsOf return the sorted actor ids recorded for a hash.
func (e *m13Env) approvalsOf(hash string) []string {
	return e.actorsOf("control.release_approvals", hash)
}
func (e *m13Env) rejectionsOf(hash string) []string {
	return e.actorsOf("control.release_rejections", hash)
}

// m13State is everything an M13 request may change for e.app.
type m13State struct {
	pointer                        string
	generation                     int64
	history, approvals, rejections int
}

func (e *m13Env) state() m13State {
	e.t.Helper()
	var s m13State
	s.pointer, s.generation, _ = e.activePointer()
	if err := e.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.release_history WHERE tenant_id=$1 AND app_id=$2),
 (SELECT count(*) FROM control.release_approvals WHERE tenant_id=$1 AND app_id=$2),(SELECT count(*) FROM control.release_rejections WHERE tenant_id=$1 AND app_id=$2)`,
		e.f.tenantA, e.app).Scan(&s.history, &s.approvals, &s.rejections); err != nil {
		e.t.Fatal("state", err)
	}
	return s
}

// unchanged fails when the release state differs from before.
func (e *m13Env) unchanged(what string, before m13State) {
	e.t.Helper()
	if after := e.state(); after != before {
		e.t.Errorf("%s changed release state: before %+v after %+v", what, before, after)
	}
}

// checkHistory asserts the ADR039 history invariants: seq dense from 1,
// generation strictly +1 per row (starting at the first recorded generation),
// previous_hash chaining and the pointer equal to the last row.
func (e *m13Env) checkHistory() []m13HistoryRow {
	e.t.Helper()
	rows := e.historyRows()
	for i, h := range rows {
		if h.seq != int64(i+1) {
			e.t.Errorf("history seq not dense: row %d has seq %d", i, h.seq)
		}
		if i > 0 {
			if h.generation != rows[i-1].generation+1 {
				e.t.Errorf("history generation not +1: %d after %d", h.generation, rows[i-1].generation)
			}
			if h.previous != rows[i-1].hash {
				e.t.Errorf("history previous_hash %s does not chain to %s", h.previous, rows[i-1].hash)
			}
		} else if h.kind == "ACTIVATE" && h.generation == 1 && h.previous != "" {
			e.t.Errorf("first activation has a previous hash %q", h.previous)
		}
	}
	hash, generation, ok := e.activePointer()
	if len(rows) > 0 && (!ok || rows[len(rows)-1].hash != hash || rows[len(rows)-1].generation != generation) {
		e.t.Errorf("pointer %s@%d is not the last history row %+v", hash, generation, rows[len(rows)-1])
	}
	return rows
}

// ------------------------------------------------------------------- guards

// m13RequireStack skips (loudly) until the M13 ports and, when routes is true,
// the ADR039 routes exist. Once they exist it never skips.
func m13RequireStack(t *testing.T, e *m13Env, routes bool) {
	t.Helper()
	var ports int
	var tables bool
	if err := e.f.operator.QueryRow(`SELECT (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control'
 AND p.proname IN('approve_release','reject_release','activate_release','rollback_release')),
 to_regclass('control.release_history') IS NOT NULL AND to_regclass('control.release_rejections') IS NOT NULL`).Scan(&ports, &tables); err != nil {
		t.Fatal("stack probe", err)
	}
	if ports < 4 || !tables {
		t.Skipf("M13 database ports/tables are not migrated yet (ports=%d, tables=%v): merge claude/m13-ports and reset the isolated database", ports, tables)
	}
	if !routes {
		return
	}
	w := e.httpActive(e.alice)
	if w.Code == 501 || w.Code == 404 && m13Decode(w).Message == "Route not found" {
		t.Skipf("ADR039 routes are not registered yet (GET releases/active: %d): merge claude/m13-api", w.Code)
	}
}

// TestM13FixtureStagesDoctoredCandidates checks the fixture itself against the
// M12 stage port: the template is a real HIGH candidate with three distinct
// parties, doctored levels stage with a consistent index row, and the database
// still refuses an acceptance author as requester.
func TestM13FixtureStagesDoctoredCandidates(t *testing.T) {
	e := m13Setup(t)
	tmpl := e.template()
	var level, requester, author, freezer string
	var high bool
	row := func(hash string) {
		t.Helper()
		if err := e.f.operator.QueryRow(`SELECT risk_level,high_risk,requester_id::text,acceptance_author::text,sandbox_frozen_by::text FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`,
			e.f.tenantA, e.app, hash).Scan(&level, &high, &requester, &author, &freezer); err != nil {
			t.Fatal("candidate row", err)
		}
	}
	row(tmpl.hash)
	if level != "HIGH" || !high || requester != e.carol.id || author != e.f.bob || freezer != e.f.alice {
		t.Fatalf("template parties: %s %v %s %s %s", level, high, requester, author, freezer)
	}
	for _, want := range []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"} {
		hash, err := e.stageLevel(want)
		if err != nil {
			t.Fatalf("stage %s: %v", want, err)
		}
		row(hash)
		if hash == tmpl.hash || level != want || high != (want == "HIGH" || want == "CRITICAL") || requester != e.carol.id {
			t.Fatalf("doctored %s: %s %v %s", want, level, high, requester)
		}
	}
	if _, err := e.stageLevelAs(e.person("bob"), "LOW"); sqlState(err) != "42501" {
		t.Fatalf("acceptance author staged as requester: %v", err)
	}
	if hash, _, ok := e.activePointer(); ok {
		t.Fatal("nothing is active yet", hash)
	}
}
