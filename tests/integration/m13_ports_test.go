//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

// Port-level tests of migration 49 (ADR039 sections 2-5 and 8). Everything here
// calls the SQL ports directly with doctored arguments; no HTTP handler and no Go
// promotion store is involved, so the database floors are tested on their own.

// portsEnv is the M12 environment plus three more admins of tenant A. Every
// candidate staged by the fixture has the same DB-stamped parties: requester and
// source author alice, acceptance author bob, sandbox freezer carol. dave and
// erin are independent approvers, so a CRITICAL quorum is possible.
type portsEnv struct {
	*m12Env
	carol, dave, erin          string // user ids
	carolSub, daveSub, erinSub string // subjects
	body                       map[string]any
	nonce                      int
}

type portsOp func(context.Context, postgres.Executor) error

func portsMember(t *testing.T, f *iamFixture, tenantID, name, role string) (id, subject string) {
	t.Helper()
	ctx := context.Background()
	subject = uuid(t)
	tx, err := f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if id, err = (identity.Store{}).Provision(ctx, tx, "https://test-issuer.example", subject, name, true); err != nil {
		t.Fatal(err)
	}
	if err = (tenant.Store{}).Provision(ctx, tx, tenantID, "Release workspace", id, role, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id, subject
}

func portsSetup(t *testing.T) *portsEnv {
	t.Helper()
	e := m12Setup(t)
	p := &portsEnv{m12Env: e}
	p.carol, p.carolSub = portsMember(t, e.f, e.f.tenantA, "Carol Ports", "admin")
	p.dave, p.daveSub = portsMember(t, e.f, e.f.tenantA, "Dave Ports", "admin")
	p.erin, p.erinSub = portsMember(t, e.f, e.f.tenantA, "Erin Ports", "admin")
	revision := e.register(e.alice, "500", 0)
	sandbox := e.sandbox(e.f.token(t, p.carolSub, nil), revision)
	set := thresholdSet(e.app, "500")
	var stored struct {
		Version int64 `json:"version"`
	}
	if w := e.requirements(e.alice, set, 0); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &stored) != nil {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	var spec struct {
		ID string `json:"id"`
	}
	if w := e.acceptance(e.bob, stored.Version, acceptanceFor(set, "500.01")); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &spec) != nil {
		t.Fatal("acceptance", w.Code, w.Body.String())
	}
	art := m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
	if w := e.candidate(e.alice, art); w.Code != 201 {
		t.Fatal("candidate", w.Code, w.Body.String())
	}
	var raw []byte
	var level, requester, author, freezer string
	if err := e.f.operator.QueryRow(`SELECT b.body,c.risk_level,c.requester_id::text,c.acceptance_author::text,c.sandbox_frozen_by::text FROM control.release_candidates c
	 JOIN control.release_bundles b USING(tenant_id,app_id,artifact_hash) WHERE c.tenant_id=$1 AND c.app_id=$2`, e.f.tenantA, e.app).Scan(&raw, &level, &requester, &author, &freezer); err != nil {
		t.Fatal(err)
	}
	if level != "HIGH" || requester != e.f.alice || author != e.f.bob || freezer != p.carol {
		t.Fatal("fixture parties", level, requester == e.f.alice, author == e.f.bob, freezer == p.carol)
	}
	p.body = portsDecode(t, raw)
	return p
}

func portsDecode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func portsHash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// stage restages the real body with a chosen risk level (HighRisk kept
// consistent), a fresh nonce in an unchecked field and optional mutations, as
// the requester, through control.stage_candidate. It returns the new hash.
func (p *portsEnv) stage(t *testing.T, level string, mutate func(body, risk map[string]any)) string {
	t.Helper()
	raw, _ := json.Marshal(p.body)
	b := portsDecode(t, raw)
	risk := b["Risk"].(map[string]any)
	risk["Level"] = level
	b["HighRisk"] = level == "HIGH" || level == "CRITICAL"
	p.nonce++
	risk["PolicyHash"] = fmt.Sprintf("%064x", p.nonce)
	if mutate != nil {
		mutate(b, risk)
	}
	if raw, _ = json.Marshal(b); len(raw) == 0 {
		t.Fatal("marshal")
	}
	hash := portsHash(raw)
	var staged bool
	if err := p.call(p.f.aliceSubject, p.f.tenantA, &staged, `SELECT control.stage_candidate($1,$2,$3)`, p.app, hash, raw); err != nil || !staged {
		t.Fatalf("stage %s: %v %v", level, staged, err)
	}
	return hash
}

// onBase makes a staged body claim the given active base.
func onBase(base string, generation int64, extra func(body, risk map[string]any)) func(body, risk map[string]any) {
	return func(b, r map[string]any) {
		b["BaseRelease"], b["BaseGeneration"] = base, generation
		if extra != nil {
			extra(b, r)
		}
	}
}

func rollbackClass(class any) func(body, risk map[string]any) {
	return func(_, r map[string]any) { r["Rollback"] = class }
}

// port runs fn as subject bound to tenantID inside the runtime transaction.
func (p *portsEnv) port(subject, tenantID string, fn portsOp) error {
	return postgres.WithRuntime(context.Background(), p.f.db, func(ctx context.Context, q postgres.Executor) error {
		principal, err := p.f.verifier.Verify(p.f.token(p.t, subject, nil))
		if err != nil {
			return err
		}
		u, err := (identity.Store{}).Resolve(ctx, q, principal)
		if err != nil {
			return err
		}
		if err = (tenant.Store{}).BindActor(ctx, q, u); err != nil {
			return err
		}
		if _, err = (tenant.Store{}).Resolve(ctx, q, u, tenantID); err != nil {
			return err
		}
		return fn(ctx, q)
	})
}

func (p *portsEnv) call(subject, tenantID string, dest any, query string, args ...any) error {
	return p.port(subject, tenantID, func(ctx context.Context, q postgres.Executor) error {
		return q.QueryRowContext(ctx, query, args...).Scan(dest)
	})
}

// raw runs a port call in a runtime transaction whose context is set by hand
// (or not at all), bypassing tenant.Store resolution.
func (p *portsEnv) raw(gucs map[string]string, dest any, query string, args ...any) error {
	return postgres.WithRuntime(context.Background(), p.f.db, func(ctx context.Context, q postgres.Executor) error {
		for k, v := range gucs {
			if _, err := q.ExecContext(ctx, `SELECT set_config($1,$2,true)`, k, v); err != nil {
				return err
			}
		}
		return q.QueryRowContext(ctx, query, args...).Scan(dest)
	})
}

func (p *portsEnv) approve(subject, hash string) (int, error) {
	var n int
	return n, p.call(subject, p.f.tenantA, &n, `SELECT control.approve_release($1,$2)`, p.app, hash)
}
func (p *portsEnv) reject(subject, hash, reason, note string) (bool, error) {
	var b bool
	return b, p.call(subject, p.f.tenantA, &b, `SELECT control.reject_release($1,$2,$3,$4)`, p.app, hash, reason, note)
}
func (p *portsEnv) activate(subject, hash, base string, generation int64) (int64, error) {
	var g int64
	return g, p.call(subject, p.f.tenantA, &g, `SELECT control.activate_release($1,$2,$3,$4)`, p.app, hash, base, generation)
}
func (p *portsEnv) rollback(subject, target, current string, generation int64, plan string, ack bool) (int64, error) {
	var g int64
	return g, p.call(subject, p.f.tenantA, &g, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, p.app, target, current, generation, plan, ack)
}

func activateOp(app, hash, base string, generation int64) portsOp {
	return func(ctx context.Context, q postgres.Executor) error {
		var g int64
		return q.QueryRowContext(ctx, `SELECT control.activate_release($1,$2,$3,$4)`, app, hash, base, generation).Scan(&g)
	}
}

// race runs first in an open transaction, keeps it open until the second
// caller (on its own connection) has had time to reach any lock, then commits.
func (p *portsEnv) race(firstSubject string, first portsOp, secondSubject string, second portsOp) (error, error) {
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(held) }) }
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() {
		firstDone <- p.port(firstSubject, p.f.tenantA, func(ctx context.Context, q postgres.Executor) error {
			err := first(ctx, q)
			signal()
			if err != nil {
				return err
			}
			<-release
			return nil
		})
		signal()
	}()
	<-held
	go func() { secondDone <- p.port(secondSubject, p.f.tenantA, second) }()
	time.Sleep(150 * time.Millisecond)
	close(release)
	return <-firstDone, <-secondDone
}

func pgDetail(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Detail
	}
	return ""
}

func wantState(t *testing.T, what string, err error, states ...string) {
	t.Helper()
	got := sqlState(err)
	for _, s := range states {
		if got == s {
			return
		}
	}
	t.Errorf("%s: SQLSTATE %q (%v), want %v", what, got, err, states)
}

type portsHistoryRow struct {
	seq, generation      int64
	kind, hash, previous string
	actor, risk, storage string
	plan                 sql.NullString
}

func (p *portsEnv) history(t *testing.T, app string) []portsHistoryRow {
	t.Helper()
	rows, err := p.f.operator.Query(`SELECT seq,generation,kind,artifact_hash,previous_hash,actor_id::text,risk_level,storage_class,plan_hash FROM control.release_history WHERE tenant_id=$1 AND app_id=$2 ORDER BY seq`, p.f.tenantA, app)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []portsHistoryRow
	for rows.Next() {
		var r portsHistoryRow
		if err = rows.Scan(&r.seq, &r.generation, &r.kind, &r.hash, &r.previous, &r.actor, &r.risk, &r.storage, &r.plan); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (p *portsEnv) pointer(t *testing.T) (string, int64) {
	t.Helper()
	var h string
	var g int64
	if err := p.f.operator.QueryRow(`SELECT artifact_hash,generation FROM control.active_releases WHERE tenant_id=$1 AND app_id=$2`, p.f.tenantA, p.app).Scan(&h, &g); err != nil {
		t.Fatal("pointer", err)
	}
	return h, g
}

// checkChain asserts the history is a contiguous append-only chain that ends at the pointer.
func (p *portsEnv) checkChain(t *testing.T) []portsHistoryRow {
	t.Helper()
	rows := p.history(t, p.app)
	for i, r := range rows {
		if r.seq != int64(i+1) || (i > 0 && (r.generation != rows[i-1].generation+1 || r.previous != rows[i-1].hash)) {
			t.Fatalf("history is not a contiguous chain at %d: %+v", i, r)
		}
	}
	if len(rows) > 0 {
		h, g := p.pointer(t)
		if last := rows[len(rows)-1]; last.hash != h || last.generation != g {
			t.Fatalf("pointer %s/%d is not the last history entry %+v", h, g, last)
		}
	}
	return rows
}

func (p *portsEnv) setMembership(t *testing.T, user, role string, active bool) {
	t.Helper()
	if _, err := p.f.operator.Exec(`UPDATE iam.memberships SET role=$3,active=$4 WHERE tenant_id=$1 AND user_id=$2`, p.f.tenantA, user, role, active); err != nil {
		t.Fatal(err)
	}
}

// legacyBundleRow stores a bundle 1.0.0 as the operator (M6R rows have no candidate row).
func (p *portsEnv) legacyBundleRow(t *testing.T, app string) string {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"Version":"1.0.0","TenantID":%q,"AppID":%q,"HighRisk":false,"RequesterID":%q,"Nonce":%q}`, p.f.tenantA, app, p.f.alice, uuid(t)))
	hash := portsHash(body)
	if _, err := p.f.operator.Exec(`INSERT INTO control.release_bundles(tenant_id,app_id,artifact_hash,body,created_by) VALUES($1,$2,$3,$4,$5)`, p.f.tenantA, app, hash, body, p.f.alice); err != nil {
		t.Fatal("legacy bundle", err)
	}
	return hash
}

func portsRole(t *testing.T, key string) *sql.DB {
	t.Helper()
	db, err := postgres.Open(os.Getenv(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ------------------------------------------------------------------- tests

func TestM13PortsCallerAuthority(t *testing.T) {
	p := portsSetup(t)
	low := p.stage(t, "LOW", nil)
	other := strings.Repeat("e", 64)
	calls := []struct {
		name  string
		query string
		args  []any
	}{
		{"approve", `SELECT control.approve_release($1,$2)::text`, []any{p.app, low}},
		{"reject", `SELECT control.reject_release($1,$2,'OTHER','')::text`, []any{p.app, low}},
		{"activate", `SELECT control.activate_release($1,$2,'',0)::text`, []any{p.app, low}},
		{"rollback", `SELECT control.rollback_release($1,$2,$3,1,'',false)::text`, []any{p.app, low, other}},
	}
	var out string
	// A viewer, an unauthenticated session and forged tenant contexts are refused.
	outsider, outsiderSub := portsMember(t, p.f, p.f.tenantB, "Olga Ports", "member")
	identityOf := func(subject, id string) map[string]string {
		return map[string]string{"app.auth_issuer": "https://test-issuer.example", "app.auth_subject": subject, "app.actor_id": id}
	}
	forgedOutsider := identityOf(outsiderSub, outsider)
	forgedOutsider["app.tenant_id"] = p.f.tenantA
	forgedAlice := identityOf(p.f.aliceSubject, p.f.alice)
	forgedAlice["app.tenant_id"] = p.f.tenantB
	noTenant := identityOf(p.f.aliceSubject, p.f.alice)
	wrongSubject := identityOf(p.f.bobSubject, p.f.alice) // actor id of alice with bob's subject
	wrongSubject["app.tenant_id"] = p.f.tenantA
	for _, c := range calls {
		wantState(t, "viewer "+c.name, p.call(p.f.viewerSubject, p.f.tenantA, &out, c.query, c.args...), "42501")
		wantState(t, "unauthenticated "+c.name, p.raw(nil, &out, c.query, c.args...), "42501")
		wantState(t, "outsider forging tenant A "+c.name, p.raw(forgedOutsider, &out, c.query, c.args...), "42501")
		wantState(t, "member forging tenant B "+c.name, p.raw(forgedAlice, &out, c.query, c.args...), "42501")
		wantState(t, "no tenant "+c.name, p.raw(noTenant, &out, c.query, c.args...), "42501")
		wantState(t, "mismatched identity "+c.name, p.raw(wrongSubject, &out, c.query, c.args...), "42501")
	}
	// Worker and dispatcher roles cannot execute the ports at all.
	for _, key := range []string{"WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db := portsRole(t, key)
		for _, c := range calls {
			wantState(t, key+" "+c.name, db.QueryRow(c.query, c.args...).Scan(&out), "42501")
		}
	}
	// Another tenant's session sees no candidate (and no pointer) of tenant A.
	wantState(t, "other tenant approve", p.call(p.f.bobSubject, p.f.tenantB, &out, calls[0].query, calls[0].args...), "P0002")
	wantState(t, "other tenant reject", p.call(p.f.bobSubject, p.f.tenantB, &out, calls[1].query, calls[1].args...), "P0002")
	wantState(t, "other tenant activate", p.call(p.f.bobSubject, p.f.tenantB, &out, calls[2].query, calls[2].args...), "P0002")
	wantState(t, "other tenant rollback", p.call(p.f.bobSubject, p.f.tenantB, &out, calls[3].query, calls[3].args...), "55000")
	// Unknown hashes and M6R 1.0.0 bundles have no candidate row.
	legacy := p.legacyBundleRow(t, p.app)
	for _, hash := range []string{other, legacy} {
		_, err := p.approve(p.f.bobSubject, hash)
		wantState(t, "approve without candidate", err, "P0002")
		_, err = p.reject(p.f.bobSubject, hash, "OTHER", "")
		wantState(t, "reject without candidate", err, "P0002")
		_, err = p.activate(p.f.bobSubject, hash, "", 0)
		wantState(t, "activate without candidate", err, "P0002")
	}
	// Malformed arguments.
	malformed := []struct {
		name  string
		query string
		args  []any
	}{
		{"approve short hash", `SELECT control.approve_release($1,$2)::text`, []any{p.app, "abc"}},
		{"approve upper-case hash", `SELECT control.approve_release($1,$2)::text`, []any{p.app, strings.ToUpper(low)}},
		{"approve empty app", `SELECT control.approve_release('',$1)::text`, []any{low}},
		{"approve null hash", `SELECT control.approve_release($1,NULL)::text`, []any{p.app}},
		{"reject unknown reason", `SELECT control.reject_release($1,$2,'BECAUSE','')::text`, []any{p.app, low}},
		{"reject null reason", `SELECT control.reject_release($1,$2,NULL,'')::text`, []any{p.app, low}},
		{"reject long note", `SELECT control.reject_release($1,$2,'OTHER',$3)::text`, []any{p.app, low, strings.Repeat("n", 281)}},
		{"reject control note", `SELECT control.reject_release($1,$2,'OTHER',$3)::text`, []any{p.app, low, "line\nbreak"}},
		{"reject bidi note", `SELECT control.reject_release($1,$2,'OTHER',$3)::text`, []any{p.app, low, "abc‮def"}},
		{"activate negative generation", `SELECT control.activate_release($1,$2,'',-1)::text`, []any{p.app, low}},
		{"activate malformed base", `SELECT control.activate_release($1,$2,'zz',0)::text`, []any{p.app, low}},
		{"activate null base", `SELECT control.activate_release($1,$2,NULL,0)::text`, []any{p.app, low}},
		{"rollback generation zero", `SELECT control.rollback_release($1,$2,$3,0,'',false)::text`, []any{p.app, low, other}},
		{"rollback empty current", `SELECT control.rollback_release($1,$2,'',1,'',false)::text`, []any{p.app, low}},
		{"rollback malformed plan", `SELECT control.rollback_release($1,$2,$3,1,'xyz',true)::text`, []any{p.app, low, other}},
	}
	for _, m := range malformed {
		wantState(t, m.name, p.call(p.f.aliceSubject, p.f.tenantA, &out, m.query, m.args...), "22023")
	}
	// A 280-character note of plain text is accepted (boundary).
	if ok, err := p.reject(p.f.aliceSubject, low, "OTHER", strings.Repeat("é", 280)); err != nil || !ok {
		t.Fatal("a 280 character note", ok, err)
	}
	// Nothing above wrote an approval or a history entry.
	var approvals, history, rejections int
	if err := p.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.release_approvals WHERE app_id=$1),(SELECT count(*) FROM control.release_history WHERE app_id=$1),(SELECT count(*) FROM control.release_rejections WHERE app_id=$1)`, p.app).Scan(&approvals, &history, &rejections); err != nil || approvals != 0 || history != 0 || rejections != 1 {
		t.Fatal("refused calls left rows", approvals, history, rejections, err)
	}
}

func TestM13PortsSeparationQuorumAndRejection(t *testing.T) {
	p := portsSetup(t)
	low, medium := p.stage(t, "LOW", nil), p.stage(t, "MEDIUM", nil)
	high, critical := p.stage(t, "HIGH", nil), p.stage(t, "CRITICAL", nil)

	// LOW and MEDIUM have no database exclusion: the requester may approve.
	if n, err := p.approve(p.f.aliceSubject, low); err != nil || n != 1 {
		t.Fatal("requester approves LOW", n, err)
	}
	if n, err := p.approve(p.f.bobSubject, medium); err != nil || n != 1 {
		t.Fatal("acceptance author approves MEDIUM", n, err)
	}
	if n, err := p.approve(p.f.aliceSubject, medium); err != nil || n != 2 {
		t.Fatal("requester approves MEDIUM", n, err)
	}
	// HIGH excludes the requester and the acceptance author; the freezer may approve.
	for _, c := range []struct{ subject, role string }{{p.f.aliceSubject, "requester"}, {p.f.bobSubject, "acceptance_author"}} {
		_, err := p.approve(c.subject, high)
		wantState(t, "HIGH self approval by "+c.role, err, "42501")
		if d := pgDetail(err); d != c.role {
			t.Errorf("HIGH separation detail %q, want %q", d, c.role)
		}
	}
	if n, err := p.approve(p.carolSub, high); err != nil || n != 1 {
		t.Fatal("freezer approves HIGH", n, err)
	}
	if n, err := p.approve(p.carolSub, high); err != nil || n != 1 {
		t.Fatal("duplicate approval must be idempotent", n, err)
	}
	// CRITICAL also excludes the sandbox freezer and needs two distinct approvers.
	for _, c := range []struct{ subject, role string }{{p.f.aliceSubject, "requester"}, {p.f.bobSubject, "acceptance_author"}, {p.carolSub, "sandbox_freezer"}} {
		_, err := p.approve(c.subject, critical)
		wantState(t, "CRITICAL self approval by "+c.role, err, "42501")
		if d := pgDetail(err); d != c.role {
			t.Errorf("CRITICAL separation detail %q, want %q", d, c.role)
		}
	}
	for i := 0; i < 2; i++ {
		if n, err := p.approve(p.daveSub, critical); err != nil || n != 1 {
			t.Fatal("dave approves CRITICAL", i, n, err)
		}
	}
	_, err := p.activate(p.daveSub, critical, "", 0)
	wantState(t, "one approver twice is not a CRITICAL quorum", err, "55000")
	if n, err := p.approve(p.erinSub, critical); err != nil || n != 2 {
		t.Fatal("erin approves CRITICAL", n, err)
	}
	// Rejection is per actor, idempotent, open to the requester and permanent.
	rejected := p.stage(t, "HIGH", nil)
	if n, err := p.approve(p.daveSub, rejected); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if ok, err := p.reject(p.f.aliceSubject, rejected, "WITHDRAWN", "requester withdraws"); err != nil || !ok {
		t.Fatal("requester rejects", ok, err)
	}
	if ok, err := p.reject(p.f.aliceSubject, rejected, "OTHER", "again"); err != nil || ok {
		t.Fatal("repeat rejection must report false", ok, err)
	}
	if ok, err := p.reject(p.f.bobSubject, rejected, "RISK_UNACCEPTABLE", ""); err != nil || !ok {
		t.Fatal("second rejector", ok, err)
	}
	_, err = p.approve(p.erinSub, rejected)
	wantState(t, "approval after rejection", err, "55000")
	_, err = p.activate(p.daveSub, rejected, "", 0)
	wantState(t, "activation of a rejected hash", err, "55000")
	var reason, note string
	if err = p.f.operator.QueryRow(`SELECT reason,note FROM control.release_rejections WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3 AND actor_id=$4`, p.f.tenantA, p.app, rejected, p.f.alice).Scan(&reason, &note); err != nil || reason != "WITHDRAWN" || note != "requester withdraws" {
		t.Fatal("first rejection must be kept", reason, note, err)
	}
	if ok, err := p.reject(p.f.bobSubject, high, "OTHER", ""); err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, err = p.approve(p.daveSub, high)
	wantState(t, "approval of a HIGH candidate after rejection", err, "55000")

	// Quorum is re-counted at activation from live memberships.
	p.setMembership(t, p.dave, "viewer", true)
	_, err = p.activate(p.erinSub, critical, "", 0)
	wantState(t, "activation after an approver was demoted to viewer", err, "55000")
	p.setMembership(t, p.dave, "admin", false)
	_, err = p.activate(p.erinSub, critical, "", 0)
	wantState(t, "activation after an approver was removed", err, "55000")
	p.setMembership(t, p.dave, "admin", true)
	if _, err = p.f.operator.Exec(`UPDATE iam.users SET active=false WHERE id=$1`, p.erin); err != nil {
		t.Fatal(err)
	}
	_, err = p.activate(p.daveSub, critical, "", 0)
	wantState(t, "activation after an approver was disabled", err, "55000")
	if _, err = p.f.operator.Exec(`UPDATE iam.users SET active=true WHERE id=$1`, p.erin); err != nil {
		t.Fatal(err)
	}
	if n, err := p.approve(p.carolSub, low); err != nil || n != 2 {
		t.Fatal("LOW count", n, err)
	}
	// The requester may activate; quorum, not the activator, carries separation.
	gen, err := p.activate(p.f.aliceSubject, critical, "", 0)
	if err != nil || gen != 1 {
		t.Fatal("CRITICAL activation with quorum", gen, err)
	}
	rows := p.checkChain(t)
	storage, _ := p.body["Storage"].(map[string]any)["class"].(string)
	if len(rows) != 1 || rows[0].kind != "ACTIVATE" || rows[0].hash != critical || rows[0].previous != "" || rows[0].generation != 1 || rows[0].actor != p.f.alice || rows[0].risk != "CRITICAL" || rows[0].storage != storage || rows[0].plan.Valid {
		t.Fatalf("activation history %+v (storage %q)", rows, storage)
	}
	// Every other candidate was built on the empty base and is now stale.
	_, err = p.approve(p.carolSub, low)
	wantState(t, "approval of a stale candidate", err, "55000")
	_, err = p.activate(p.carolSub, medium, "", 0)
	wantState(t, "activation of a stale candidate", err, "55000")
}

func TestM13PortsActivationCompareAndSet(t *testing.T) {
	p := portsSetup(t)
	a, b := p.stage(t, "LOW", nil), p.stage(t, "LOW", nil)
	for _, h := range []string{a, b} {
		if _, err := p.approve(p.f.bobSubject, h); err != nil {
			t.Fatal(err)
		}
	}
	_, err := p.activate(p.f.bobSubject, a, strings.Repeat("e", 64), 0)
	wantState(t, "wrong expected base", err, "55000")
	_, err = p.activate(p.f.bobSubject, a, "", 1)
	wantState(t, "wrong expected generation", err, "55000")
	unapproved := p.stage(t, "LOW", nil)
	_, err = p.activate(p.f.bobSubject, unapproved, "", 0)
	wantState(t, "activation without any approval", err, "55000")
	noStorage := p.stage(t, "LOW", func(body, _ map[string]any) { delete(body["Storage"].(map[string]any), "class") })
	if _, err = p.approve(p.f.bobSubject, noStorage); err != nil {
		t.Fatal(err)
	}
	_, err = p.activate(p.f.bobSubject, noStorage, "", 0)
	wantState(t, "activation of a body without a storage class", err, "55000")

	// Two first activations on separate connections: exactly one wins.
	first, second := p.race(p.f.bobSubject, activateOp(p.app, a, "", 0), p.daveSub, activateOp(p.app, b, "", 0))
	if first != nil {
		t.Fatal("first activation", first)
	}
	wantState(t, "racing first activation", second, "40001", "55000", "55P03")
	if h, g := p.pointer(t); h != a || g != 1 {
		t.Fatal("pointer after the first-release race", h == a, g)
	}
	if rows := p.checkChain(t); len(rows) != 1 {
		t.Fatalf("history rows after the race: %d", len(rows))
	}
	// A first release cannot be activated twice and an active candidate is not re-applied.
	_, err = p.activate(p.f.bobSubject, b, "", 0)
	wantState(t, "second first-release activation", err, "55000")
	_, err = p.activate(p.f.bobSubject, a, "", 0)
	wantState(t, "re-activation of the active candidate", err, "55000")
	_, err = p.activate(p.f.bobSubject, a, a, 1)
	wantState(t, "re-activation claiming the active base", err, "55000")

	// Two activations over the same base and generation: exactly one wins.
	c := p.stage(t, "LOW", onBase(a, 1, nil))
	d := p.stage(t, "LOW", onBase(a, 1, nil))
	for _, h := range []string{c, d} {
		if _, err = p.approve(p.carolSub, h); err != nil {
			t.Fatal(err)
		}
	}
	first, second = p.race(p.daveSub, activateOp(p.app, c, a, 1), p.erinSub, activateOp(p.app, d, a, 1))
	if first != nil {
		t.Fatal("first activation over a base", first)
	}
	wantState(t, "racing activation over a base", second, "40001", "55000", "55P03")
	if h, g := p.pointer(t); h != c || g != 2 {
		t.Fatal("pointer after the base race", h == c, g)
	}
	_, err = p.activate(p.erinSub, d, a, 1)
	wantState(t, "loser retried with the stale base", err, "55000")
	_, err = p.activate(p.erinSub, d, c, 2)
	wantState(t, "expected base that is not the candidate base", err, "55000")

	// Approval racing activation: activation counts only committed approvals.
	critical := p.stage(t, "CRITICAL", onBase(c, 2, nil))
	if _, err = p.approve(p.daveSub, critical); err != nil {
		t.Fatal(err)
	}
	approveErin := func(ctx context.Context, q postgres.Executor) error {
		var n int
		return q.QueryRowContext(ctx, `SELECT control.approve_release($1,$2)`, p.app, critical).Scan(&n)
	}
	first, second = p.race(p.erinSub, approveErin, p.f.bobSubject, activateOp(p.app, critical, c, 2))
	if first != nil {
		t.Fatal("racing approval", first)
	}
	wantState(t, "activation racing an uncommitted approval", second, "55000")
	if gen, err := p.activate(p.f.bobSubject, critical, c, 2); err != nil || gen != 3 {
		t.Fatal("activation after the approval committed", gen, err)
	}
	rows := p.checkChain(t)
	if len(rows) != 3 || rows[1].hash != c || rows[2].hash != critical || rows[2].previous != c || rows[2].risk != "CRITICAL" {
		t.Fatalf("history %+v", rows)
	}
}

func TestM13PortsRollbackTargetsAndClassGate(t *testing.T) {
	p := portsSetup(t)
	plan := strings.Repeat("c", 64)
	activate := func(level string, base string, generation int64, class any) string {
		t.Helper()
		var h string
		if base == "" {
			h = p.stage(t, level, rollbackClass(class))
		} else {
			h = p.stage(t, level, onBase(base, generation, rollbackClass(class)))
		}
		if _, err := p.approve(p.daveSub, h); err != nil {
			t.Fatal(err)
		}
		if gen, err := p.activate(p.daveSub, h, base, generation); err != nil || gen != generation+1 {
			t.Fatal("activate", gen, err)
		}
		return h
	}
	r1 := activate("LOW", "", 0, "POINTER_SAFE")
	r2 := activate("HIGH", r1, 1, "POINTER_SAFE")
	never := p.stage(t, "LOW", onBase(r2, 2, nil))
	if _, err := p.approve(p.daveSub, never); err != nil {
		t.Fatal(err)
	}
	_, err := p.rollback(p.erinSub, never, r2, 2, plan, true)
	wantState(t, "rollback to an approved but never activated hash", err, "55000")
	_, err = p.rollback(p.erinSub, strings.Repeat("d", 64), r2, 2, plan, true)
	wantState(t, "rollback to an unknown hash", err, "55000")
	_, err = p.rollback(p.erinSub, r2, r2, 2, plan, true)
	wantState(t, "rollback to the current release", err, "55000")
	_, err = p.rollback(p.erinSub, r1, r1, 2, plan, true)
	wantState(t, "rollback with the wrong current hash", err, "55000")
	_, err = p.rollback(p.erinSub, r1, r2, 1, plan, true)
	wantState(t, "rollback with the wrong generation", err, "55000")
	_, err = p.rollback(p.erinSub, r1, r2, 2, "not-a-hash", true)
	wantState(t, "rollback with a malformed plan hash", err, "22023")
	var g int64
	wantState(t, "viewer rollback", p.call(p.f.viewerSubject, p.f.tenantA, &g, `SELECT control.rollback_release($1,$2,$3,2,'',false)`, p.app, r1, r2), "42501")
	// POINTER_SAFE: no plan needed.
	if g, err = p.rollback(p.erinSub, r1, r2, 2, "", false); err != nil || g != 3 {
		t.Fatal("pointer-safe rollback without a plan", g, err)
	}
	// DATA_NOT_REVERSIBLE: plan_ack AND a 64-hex plan hash are both required.
	r3 := activate("LOW", r1, 3, "DATA_NOT_REVERSIBLE")
	for _, c := range []struct {
		name string
		plan string
		ack  bool
	}{{"no plan", "", false}, {"ack without hash", "", true}, {"hash without ack", plan, false}} {
		_, err = p.rollback(p.erinSub, r2, r3, 4, c.plan, c.ack)
		wantState(t, "non pointer-safe rollback, "+c.name, err, "55000")
	}
	if g, err = p.rollback(p.erinSub, r2, r3, 4, plan, true); err != nil || g != 5 {
		t.Fatal("planned rollback", g, err)
	}
	// Unreadable classes (non-string, missing, wrong case) also require a plan.
	r4 := activate("LOW", r2, 5, 7)
	_, err = p.rollback(p.erinSub, r1, r4, 6, "", false)
	wantState(t, "rollback class that is not a string", err, "55000")
	if g, err = p.rollback(p.erinSub, r1, r4, 6, plan, true); err != nil || g != 7 {
		t.Fatal(g, err)
	}
	r5 := activate("LOW", r1, 7, "pointer_safe")
	_, err = p.rollback(p.erinSub, r1, r5, 8, "", false)
	wantState(t, "rollback class in the wrong case", err, "55000")
	if g, err = p.rollback(p.erinSub, r1, r5, 8, plan, true); err != nil || g != 9 {
		t.Fatal(g, err)
	}
	missing := p.stage(t, "LOW", onBase(r1, 9, func(_, r map[string]any) { delete(r, "Rollback") }))
	if _, err = p.approve(p.daveSub, missing); err != nil {
		t.Fatal(err)
	}
	if g, err = p.activate(p.daveSub, missing, r1, 9); err != nil || g != 10 {
		t.Fatal(g, err)
	}
	_, err = p.rollback(p.erinSub, r1, missing, 10, "", false)
	wantState(t, "rollback class missing", err, "55000")
	if g, err = p.rollback(p.erinSub, r1, missing, 10, plan, true); err != nil || g != 11 {
		t.Fatal(g, err)
	}

	rows := p.checkChain(t)
	kinds := ""
	for _, r := range rows {
		kinds += r.kind[:1]
	}
	if kinds != "AARARARARAR" {
		t.Fatalf("history kinds %s", kinds)
	}
	if rows[2].previous != r2 || rows[2].hash != r1 || rows[2].plan.Valid || rows[2].risk != "LOW" || rows[2].actor != p.erin {
		t.Fatalf("pointer-safe rollback row %+v", rows[2])
	}
	if rows[4].hash != r2 || rows[4].plan.String != plan || rows[4].risk != "HIGH" {
		t.Fatalf("planned rollback row %+v", rows[4])
	}
	for _, r := range rows {
		if r.kind == "ACTIVATE" && r.plan.Valid {
			t.Fatalf("activation row with a plan %+v", r)
		}
	}
}

func TestM13PortsTablesAreAppendOnlyIsolatedAndHygienic(t *testing.T) {
	p := portsSetup(t)
	first := p.stage(t, "LOW", nil)
	if _, err := p.approve(p.daveSub, first); err != nil {
		t.Fatal(err)
	}
	if _, err := p.activate(p.daveSub, first, "", 0); err != nil {
		t.Fatal(err)
	}
	spare := p.stage(t, "LOW", onBase(first, 1, nil))
	if _, err := p.approve(p.daveSub, spare); err != nil {
		t.Fatal(err)
	}
	if _, err := p.reject(p.erinSub, spare, "OTHER", "spare"); err != nil {
		t.Fatal(err)
	}
	tA, app := p.f.tenantA, p.app
	// Direct DML by the runtime role and the worker: no privilege at all.
	direct := []struct {
		name  string
		query string
		args  []any
	}{
		{"insert approval", `INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash) VALUES($1,$2,$3)`, []any{tA, app, spare}},
		{"update approval", `UPDATE control.release_approvals SET approved_at=approved_at WHERE app_id=$1`, []any{app}},
		{"delete approval", `DELETE FROM control.release_approvals WHERE app_id=$1`, []any{app}},
		{"truncate approvals", `TRUNCATE control.release_approvals`, nil},
		{"insert rejection", `INSERT INTO control.release_rejections(tenant_id,app_id,artifact_hash,reason) VALUES($1,$2,$3,'OTHER')`, []any{tA, app, first}},
		{"update rejection", `UPDATE control.release_rejections SET note='' WHERE app_id=$1`, []any{app}},
		{"delete rejection", `DELETE FROM control.release_rejections WHERE app_id=$1`, []any{app}},
		{"truncate rejections", `TRUNCATE control.release_rejections`, nil},
		{"insert pointer", `INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES($1,'app.ports_other',$2,1)`, []any{tA, spare}},
		{"move pointer", `UPDATE control.active_releases SET artifact_hash=$2,generation=generation+1 WHERE app_id=$1`, []any{app, spare}},
		{"delete pointer", `DELETE FROM control.active_releases WHERE app_id=$1`, []any{app}},
		{"truncate pointers", `TRUNCATE control.active_releases`, nil},
		{"insert history", `INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,risk_level,storage_class) VALUES($1,$2,2,'ACTIVATE',$3,$4,2,'LOW','EQUAL')`, []any{tA, app, spare, first}},
		{"update history", `UPDATE control.release_history SET risk_level='LOW' WHERE app_id=$1`, []any{app}},
		{"delete history", `DELETE FROM control.release_history WHERE app_id=$1`, []any{app}},
		{"truncate history", `TRUNCATE control.release_history`, nil},
	}
	worker := portsRole(t, "WORKER_DATABASE_URL")
	for _, d := range direct {
		err := p.port(p.f.aliceSubject, tA, func(ctx context.Context, q postgres.Executor) error {
			_, err := q.ExecContext(ctx, d.query, d.args...)
			return err
		})
		wantState(t, "framework_app "+d.name, err, "42501")
		tx, err := worker.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(d.query, d.args...)
		_ = tx.Rollback()
		wantState(t, "worker "+d.name, err, "42501")
	}
	// The operator (migrator, member of the table owner) hits the guards.
	var seq, gen int64
	var last string
	if err := p.f.operator.QueryRow(`SELECT seq,generation,artifact_hash FROM control.release_history WHERE tenant_id=$1 AND app_id=$2 ORDER BY seq DESC LIMIT 1`, tA, app).Scan(&seq, &gen, &last); err != nil {
		t.Fatal(err)
	}
	orphanApp := "app.ports_orphan_" + strings.ReplaceAll(uuid(t), "-", "")
	orphan := p.legacyBundleRow(t, orphanApp)
	operator := []struct {
		name, state string
		query       string
		args        []any
	}{
		{"insert approval", "42501", `INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash,actor_id) VALUES($1,$2,$3,$4)`, []any{tA, app, spare, p.erin}},
		{"update approval", "P0001", `UPDATE control.release_approvals SET approved_at=approved_at WHERE app_id=$1`, []any{app}},
		{"delete approval", "P0001", `DELETE FROM control.release_approvals WHERE app_id=$1`, []any{app}},
		{"truncate approvals", "P0001", `TRUNCATE control.release_approvals`, nil},
		{"insert rejection", "42501", `INSERT INTO control.release_rejections(tenant_id,app_id,artifact_hash,actor_id,reason) VALUES($1,$2,$3,$4,'OTHER')`, []any{tA, app, first, p.dave}},
		{"update rejection", "P0001", `UPDATE control.release_rejections SET note='' WHERE app_id=$1`, []any{app}},
		{"update no rejection row", "P0001", `UPDATE control.release_rejections SET note='' WHERE app_id='app.none'`, nil},
		{"delete rejection", "P0001", `DELETE FROM control.release_rejections WHERE app_id=$1`, []any{app}},
		{"truncate rejections", "P0001", `TRUNCATE control.release_rejections`, nil},
		{"insert chained history", "42501", `INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,actor_id,risk_level,storage_class) VALUES($1,$2,$3,'ACTIVATE',$4,$5,$6,$7,'LOW','EQUAL')`, []any{tA, app, seq + 1, spare, last, gen + 1, p.dave}},
		{"insert history with a gap", "55000", `INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,actor_id,risk_level,storage_class) VALUES($1,$2,$3,'ACTIVATE',$4,$5,$6,$7,'LOW','EQUAL')`, []any{tA, app, seq + 5, spare, last, gen + 1, p.dave}},
		{"update history", "P0001", `UPDATE control.release_history SET risk_level='LOW' WHERE app_id=$1`, []any{app}},
		{"delete history", "P0001", `DELETE FROM control.release_history WHERE app_id=$1`, []any{app}},
		{"truncate history", "P0001", `TRUNCATE control.release_history`, nil},
		{"insert unrecorded pointer", "55000", `INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES($1,$2,$3,1)`, []any{tA, orphanApp, orphan}},
		{"move pointer without history", "55000", `UPDATE control.active_releases SET artifact_hash=$2,generation=generation+1 WHERE app_id=$1`, []any{app, spare}},
		{"pointer generation jump", "55000", `UPDATE control.active_releases SET artifact_hash=$2,generation=generation+2 WHERE app_id=$1`, []any{app, spare}},
		{"pointer same hash", "55000", `UPDATE control.active_releases SET generation=generation+1 WHERE app_id=$1`, []any{app}},
		{"delete pointer", "P0001", `DELETE FROM control.active_releases WHERE app_id=$1`, []any{app}},
		{"truncate pointers", "P0001", `TRUNCATE control.active_releases`, nil},
	}
	for _, o := range operator {
		tx, err := p.f.operator.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(o.query, o.args...); err == nil {
			err = tx.Commit() // deferred pointer checks fire here
		} else {
			_ = tx.Rollback()
		}
		wantState(t, "operator "+o.name, err, o.state)
	}
	if h, g := p.pointer(t); h != first || g != 1 {
		t.Fatal("the pointer moved", h == first, g)
	}
	p.checkChain(t)

	// Forced RLS: another tenant's session sees none of tenant A's rows.
	tables := []string{"control.release_approvals", "control.release_rejections", "control.active_releases", "control.release_history"}
	for _, table := range tables {
		count := func(subject, tenantID string) int {
			var n int
			if err := p.call(subject, tenantID, &n, `SELECT count(*) FROM `+table+` WHERE app_id=$1`, app); err != nil {
				t.Fatal(table, err)
			}
			return n
		}
		if n := count(p.f.aliceSubject, tA); n == 0 {
			t.Errorf("%s: tenant member sees no rows", table)
		}
		if n := count(p.f.viewerSubject, tA); n == 0 {
			t.Errorf("%s: tenant viewer sees no rows", table)
		}
		if n := count(p.f.bobSubject, p.f.tenantB); n != 0 {
			t.Errorf("%s: other tenant sees %d rows", table, n)
		}
		var n int
		wantState(t, "unauthenticated read "+table, p.raw(nil, &n, `SELECT count(*) FROM `+table), "")
		if n != 0 {
			t.Errorf("%s: unauthenticated session sees %d rows", table, n)
		}
		wantState(t, "worker read "+table, worker.QueryRow(`SELECT count(*) FROM `+table).Scan(&n), "42501")
	}

	// SECURITY DEFINER hygiene of the ports and the internal helpers.
	for _, f := range []struct {
		sig     string
		runtime bool
		definer bool
	}{
		{"control.approve_release(text,text)", true, true},
		{"control.reject_release(text,text,text,text)", true, true},
		{"control.activate_release(text,text,text,bigint)", true, true},
		{"control.rollback_release(text,text,text,bigint,text,boolean)", true, true},
		{"control.release_approval_count(uuid,text,text)", false, true},
		{"control.release_actor_ok(uuid)", false, true},
		{"control.release_pointer_recorded()", false, true},
		{"control.release_port_write()", false, false},
		{"control.release_history_append()", false, false},
		{"control.release_pointer_step()", false, false},
	} {
		var definer, public, worker, dispatcher, runtime bool
		var owner, config string
		if err := p.f.operator.QueryRow(`SELECT p.prosecdef,p.proowner::regrole::text,coalesce(array_to_string(p.proconfig,';'),''),
		 has_function_privilege('public',p.oid,'EXECUTE'),has_function_privilege('framework_worker',p.oid,'EXECUTE'),
		 has_function_privilege('framework_dispatcher',p.oid,'EXECUTE'),has_function_privilege('framework_app',p.oid,'EXECUTE')
		 FROM pg_proc p WHERE p.oid=$1::text::regprocedure`, f.sig).Scan(&definer, &owner, &config, &public, &worker, &dispatcher, &runtime); err != nil {
			t.Fatal(f.sig, err)
		}
		if definer != f.definer || owner != "framework_schema_owner" || config != "search_path=pg_catalog, pg_temp" || public || worker || dispatcher || runtime != f.runtime {
			t.Errorf("%s hygiene: definer=%v owner=%s config=%q public=%v worker=%v dispatcher=%v runtime=%v", f.sig, definer, owner, config, public, worker, dispatcher, runtime)
		}
	}

	// The worker drift guard covers every release authority table.
	var safe bool
	if err := worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe); err != nil || !safe {
		t.Fatal("worker grants must start safe", safe, err)
	}
	for _, table := range tables {
		if _, err := p.f.operator.Exec(`GRANT SELECT(app_id) ON ` + table + ` TO framework_worker`); err != nil {
			t.Fatal(err)
		}
		scanErr := worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe)
		if _, err := p.f.operator.Exec(`REVOKE SELECT(app_id) ON ` + table + ` FROM framework_worker`); err != nil {
			t.Fatal(err)
		}
		if scanErr != nil || safe {
			t.Error("worker drift guard missed a column grant on", table, scanErr)
		}
	}
	// No runtime role holds a direct write privilege on any release authority table.
	for _, table := range tables {
		for _, role := range []string{"public", "framework_app", "framework_worker", "framework_dispatcher"} {
			var write bool
			if err := p.f.operator.QueryRow(`SELECT has_table_privilege($1,$2,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege($1,$2,'INSERT,UPDATE,REFERENCES')`, role, table).Scan(&write); err != nil || write {
				t.Errorf("%s can write %s (%v)", role, table, err)
			}
		}
	}
}

// portsDatabaseAt creates a disposable database migrated to version, points
// every binding at it (like m8Database) and returns the migration provider.
func portsDatabaseAt(t *testing.T, version int64) *goose.Provider {
	t.Helper()
	if environment := os.Getenv("APP_ENV"); environment != "development" && environment != "test" {
		t.Fatal("disposable fixtures require a development/test environment")
	}
	ctx := context.Background()
	adminURL, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || (adminURL.Hostname() != "127.0.0.1" && adminURL.Hostname() != "localhost") {
		t.Fatal("local admin binding required")
	}
	adminURL.User = url.UserPassword("postgres", os.Getenv("POSTGRES_PASSWORD"))
	admin, err := postgres.Open(adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "m13_ports_" + strings.ReplaceAll(uuid(t), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	for _, statement := range []string{`CREATE DATABASE ` + quoted + ` OWNER framework_migrator`, `REVOKE ALL ON DATABASE ` + quoted + ` FROM PUBLIC`,
		`GRANT CONNECT ON DATABASE ` + quoted + ` TO framework_app,framework_worker,framework_dispatcher`} {
		if _, err = admin.ExecContext(ctx, statement); err != nil {
			t.Fatal("disposable database", err)
		}
	}
	t.Cleanup(func() {
		db, err := postgres.Open(adminURL.String())
		if err != nil {
			t.Error(err)
			return
		}
		defer db.Close()
		if _, err = db.Exec(`DROP DATABASE ` + quoted + ` WITH (FORCE)`); err != nil {
			t.Error("disposable database cleanup")
		}
	})
	for _, key := range []string{"DATABASE_URL", "MIGRATION_DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		u, err := url.Parse(os.Getenv(key))
		if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
			t.Fatal("local binding required")
		}
		u.Path = "/" + name
		query := u.Query()
		query.Del("dbname")
		query.Del("database")
		query.Del("user")
		u.RawQuery = query.Encode()
		probe, err := postgres.Open(u.String())
		if err != nil {
			t.Fatal(err)
		}
		var actual string
		err = probe.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual)
		probe.Close()
		if err != nil || actual != name {
			t.Fatal("connected disposable binding mismatch")
		}
		t.Setenv(key, u.String())
	}
	t.Setenv("M9R_DISPOSABLE_WORKER_BINDING", "true")
	m, err := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, m, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, version); err != nil {
		t.Fatal("migrate to", version, err)
	}
	return provider
}

func TestM13PortsCloseTheM6RWriteGap(t *testing.T) {
	provider := portsDatabaseAt(t, 48)
	f := newIAM(t)
	p := &portsEnv{m12Env: &m12Env{t: t, f: f, app: "app.legacy_" + strings.ReplaceAll(uuid(t), "-", "")}}
	legacy := p.legacyBundleRow(t, p.app)
	writes := []struct{ name, query string }{
		{"insert approval", `INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash) VALUES($1,$2,$3)`},
		{"insert pointer", `INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES($1,$2,$3,1)`},
		{"move pointer", `UPDATE control.active_releases SET generation=generation+1 WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`},
	}
	exec := func(query string) error {
		return p.port(f.aliceSubject, f.tenantA, func(ctx context.Context, q postgres.Executor) error {
			r, err := q.ExecContext(ctx, query, f.tenantA, p.app, legacy)
			if err == nil {
				if n, _ := r.RowsAffected(); n != 1 {
					return fmt.Errorf("%d rows", n)
				}
			}
			return err
		})
	}
	// Before migration 49 the runtime role writes approvals and moves the pointer directly.
	for _, w := range writes {
		if err := exec(w.query); err != nil {
			t.Fatalf("M12 schema refused %s: %v (the gap this milestone closes is gone?)", w.name, err)
		}
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal("migration 49 over populated M6R rows", err)
	}
	if applied, err := provider.Up(context.Background()); err != nil || len(applied) != 0 {
		t.Fatal("migration repeatability", err)
	}
	for _, w := range writes {
		wantState(t, "after migration 49: "+w.name, exec(w.query), "42501")
	}
	// M6R rows are untouched; the legacy hash is not a candidate and not a rollback target.
	var approvals int
	var generation int64
	if err := f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.release_approvals WHERE app_id=$1),(SELECT generation FROM control.active_releases WHERE app_id=$1)`, p.app).Scan(&approvals, &generation); err != nil || approvals != 1 || generation != 2 {
		t.Fatal("M6R rows changed", approvals, generation, err)
	}
	_, err := p.approve(f.aliceSubject, legacy)
	wantState(t, "approve a 1.0.0 bundle", err, "P0002")
	_, err = p.activate(f.aliceSubject, legacy, legacy, 2)
	wantState(t, "activate a 1.0.0 bundle", err, "P0002")
	other := p.legacyBundleRow(t, p.app)
	_, err = p.rollback(f.aliceSubject, other, legacy, 2, strings.Repeat("c", 64), true)
	wantState(t, "rollback from an M6R pointer to a hash without history", err, "55000")
	_, err = f.operator.Exec(`UPDATE control.active_releases SET artifact_hash=$2,generation=3 WHERE app_id=$1`, p.app, other)
	wantState(t, "operator moves an M6R pointer by hand", err, "55000")
}
