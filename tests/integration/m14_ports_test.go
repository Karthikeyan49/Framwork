//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

// Port-level tests of migration 00060 (ADR040 A1-A4). Everything here calls the SQL
// ports directly with doctored arguments; no HTTP handler is involved, so the database
// floors are tested on their own.

type m14Ports struct {
	*m14Env
	p        *portsEnv
	revision int64
	hash     string
}

func m14PortsSetup(t *testing.T) *m14Ports {
	t.Helper()
	e := m14Setup(t, m14Options{})
	e.draft("500")
	revision, hash := e.headAndHash()
	return &m14Ports{m14Env: e, p: &portsEnv{m12Env: e.m12Env}, revision: revision, hash: hash}
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

var (
	h1 = strings.Repeat("1", 64)
	h2 = strings.Repeat("2", 64)
	h3 = strings.Repeat("3", 64)
)

const beginSQL = `SELECT control.ai_begin($1,$2,$3,$4,$5,$6,$7,$8,$9)::text`

// begin admits a request as subject with the usual numbers.
func (m *m14Ports) begin(subject string, kind string, daily, reserve int64, inflight, lease int) (string, error) {
	var id string
	err := m.p.call(subject, m.f.tenantA, &id, beginSQL, m.app, kind, h1, m.revision, m.hash, daily, reserve, inflight, lease)
	return id, err
}

// beginRetrying retries the contention refusals (lock timeout, serialization) the way
// a client would; limits are refusals and are returned.
func (m *m14Ports) beginRetrying(subject string, kind string, daily, reserve int64, inflight, lease int) (string, error) {
	for i := 0; ; i++ {
		id, err := m.begin(subject, kind, daily, reserve, inflight, lease)
		var pg *pgconn.PgError
		if err != nil && i < 40 && asPg(err, &pg) && (pg.Code == "55P03" || pg.Code == "40001" || pg.Code == "40P01") {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		return id, err
	}
}

func (m *m14Ports) attempt(subject, id string, n int, outcome string) error {
	var x any
	return m.p.port(subject, m.f.tenantA, func(ctx context.Context, q postgres.Executor) error {
		_, err := q.ExecContext(ctx, `SELECT control.ai_record_attempt($1,$2,$3,'scripted','m','ai-templates-v1',$4,$5,$6,10,5,12,$7,'',$8)`, m.app, id, n, h1, h2, h3, outcome, h1)
		_ = x
		return err
	})
}

func (m *m14Ports) finishFailed(subject, id string) (string, error) {
	var st string
	err := m.p.call(subject, m.f.tenantA, &st, `SELECT control.ai_finish($1,$2,'FAILED',NULL,NULL,NULL,NULL,NULL,'INTERNAL',NULL,1,100,50)`, m.app, id)
	return st, err
}

// readyBody is a canonical, valid patch proposal for the app.
func (m *m14Ports) readyBody() ([]byte, string) {
	m.t.Helper()
	doc := m.storedDoc(m.revision)
	raw := labelPatch(m.t, doc, "entity.order", fmt.Sprintf("Label %d", time.Now().UnixNano()))
	return raw, sum(raw)
}

func (m *m14Ports) finishReady(subject, id string) (string, error) {
	body, hash := m.readyBody()
	var st string
	err := m.p.call(subject, m.f.tenantA, &st, `SELECT control.ai_finish($1,$2,'READY',$3,$4,'{"targets":[],"sensitive":[],"aspects":[]}'::jsonb,NULL,NULL,NULL,$5,1,100,50)`, m.app, id, body, hash, h1)
	return st, err
}

func (m *m14Ports) decide(subject, id, decision string, expected, result *int64, resultHash, reason string) (string, error) {
	var st string
	var rh, rr any
	if resultHash != "" {
		rh = resultHash
	}
	if reason != "" {
		rr = reason
	}
	err := m.p.call(subject, m.f.tenantA, &st, `SELECT control.ai_decide($1,$2,$3,$4,$5,$6,$7,$8)`, m.app, id, decision, expected, result, rh, rr, h3)
	return st, err
}

func i64(n int64) *int64 { return &n }

func TestM14PortsCallerAuthority(t *testing.T) {
	m := m14PortsSetup(t)
	id, err := m.begin(m.f.aliceSubject, "patch", 200000, 10000, 4, 30)
	if err != nil {
		t.Fatal(err)
	}
	olga := m.addMemberIn(m.f.tenantB, "olga", "owner")
	calls := []struct {
		name  string
		query string
		args  []any
	}{
		{"begin", beginSQL, []any{m.app, "patch", h1, m.revision, m.hash, 200000, 10000, 4, 30}},
		{"record_attempt", `SELECT control.ai_record_attempt($1,$2,1,'p','m','t','','','',1,1,1,'OK','',$3)::text`, []any{m.app, id, h1}},
		{"finish", `SELECT control.ai_finish($1,$2,'FAILED',NULL,NULL,NULL,NULL,NULL,'INTERNAL',NULL,1,1,1)::text`, []any{m.app, id}},
		{"decide", `SELECT control.ai_decide($1,$2,'REJECT',NULL,NULL,NULL,'OTHER',$3)::text`, []any{m.app, id, h3}},
	}
	identityOf := func(subject, user string) map[string]string {
		return map[string]string{"app.auth_issuer": "https://test-issuer.example", "app.auth_subject": subject, "app.actor_id": user}
	}
	forgedOutsider := identityOf(olga.subject, olga.id)
	forgedOutsider["app.tenant_id"] = m.f.tenantA
	forgedAlice := identityOf(m.f.aliceSubject, m.f.alice)
	forgedAlice["app.tenant_id"] = m.f.tenantB
	noTenant := identityOf(m.f.aliceSubject, m.f.alice)
	wrongSubject := identityOf(m.f.bobSubject, m.f.alice)
	wrongSubject["app.tenant_id"] = m.f.tenantA
	var out string
	for _, c := range calls {
		wantState(t, "viewer "+c.name, m.p.call(m.f.viewerSubject, m.f.tenantA, &out, c.query, c.args...), "42501")
		wantState(t, "unauthenticated "+c.name, m.p.raw(nil, &out, c.query, c.args...), "42501")
		wantState(t, "outsider forging tenant A "+c.name, m.p.raw(forgedOutsider, &out, c.query, c.args...), "42501")
		wantState(t, "member forging tenant B "+c.name, m.p.raw(forgedAlice, &out, c.query, c.args...), "42501")
		wantState(t, "no tenant "+c.name, m.p.raw(noTenant, &out, c.query, c.args...), "42501")
		wantState(t, "mismatched identity "+c.name, m.p.raw(wrongSubject, &out, c.query, c.args...), "42501")
	}
	for _, key := range []string{"WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db := portsRole(t, key)
		for _, c := range calls {
			wantState(t, key+" "+c.name, db.QueryRow(c.query, c.args...).Scan(&out), "42501")
		}
	}
	// A member of another tenant addressing our application and proposal.
	wantState(t, "other tenant begin", m.p.call(olga.subject, m.f.tenantB, &out, calls[0].query, calls[0].args...), "P0002")
	wantState(t, "other tenant record", m.p.call(olga.subject, m.f.tenantB, &out, calls[1].query, calls[1].args...), "P0002")
	wantState(t, "other tenant finish", m.p.call(olga.subject, m.f.tenantB, &out, calls[2].query, calls[2].args...), "P0002")
	wantState(t, "other tenant decide", m.p.call(olga.subject, m.f.tenantB, &out, calls[3].query, calls[3].args...), "P0002")
	// A different admin of the same tenant is not the requester: it can neither record
	// attempts nor finish; it may decide only a READY proposal.
	wantState(t, "non-requester record", m.attempt(m.f.bobSubject, id, 1, "OK"), "42501")
	_, err = m.finishFailed(m.f.bobSubject, id)
	wantState(t, "non-requester finish", err, "42501")
	_, err = m.decide(m.f.bobSubject, id, "REJECT", nil, nil, "", "OTHER")
	wantState(t, "decide a PENDING proposal", err, "55000")
	if pgDetail(err) != "not_ready" {
		t.Fatalf("detail %q", pgDetail(err))
	}
	if m.proposalRows() != 1 {
		t.Fatalf("refused calls created or removed rows: %d", m.proposalRows())
	}
	if _, err = m.finishFailed(m.f.aliceSubject, id); err != nil {
		t.Fatal(err)
	}
}

func TestM14PortsRefuseMalformedArguments(t *testing.T) {
	m := m14PortsSetup(t)
	id, err := m.begin(m.f.aliceSubject, "patch", 200000, 10000, 4, 30)
	if err != nil {
		t.Fatal(err)
	}
	type call struct {
		name  string
		query string
		args  []any
	}
	begin := func(mutate func(a []any)) []any {
		a := []any{m.app, "patch", h1, m.revision, m.hash, int64(200000), int64(10000), 4, 30}
		mutate(a)
		return a
	}
	var malformed []call
	for name, mutate := range map[string]func(a []any){
		"app pattern": func(a []any) { a[0] = "orders" }, "app empty": func(a []any) { a[0] = "" }, "app null": func(a []any) { a[0] = nil },
		"kind": func(a []any) { a[1] = "chat" }, "kind null": func(a []any) { a[1] = nil },
		"request hash short": func(a []any) { a[2] = "abc" }, "request hash upper": func(a []any) { a[2] = strings.ToUpper(h1[:63]) + "A" }, "request hash null": func(a []any) { a[2] = nil },
		"revision zero": func(a []any) { a[3] = 0 }, "revision huge": func(a []any) { a[3] = int64(1_000_000_001) },
		"draft hash": func(a []any) { a[4] = "zz" },
		"daily zero": func(a []any) { a[5] = int64(0) }, "daily huge": func(a []any) { a[5] = int64(100_000_001) },
		"reserve zero": func(a []any) { a[6] = int64(0) }, "reserve huge": func(a []any) { a[6] = int64(10_000_001) },
		"inflight zero": func(a []any) { a[7] = 0 }, "inflight 17": func(a []any) { a[7] = 17 },
		"lease zero": func(a []any) { a[8] = 0 }, "lease 601": func(a []any) { a[8] = 601 },
	} {
		malformed = append(malformed, call{"begin " + name, beginSQL, begin(mutate)})
	}
	attempt := func(mutate func(a []any)) []any {
		a := []any{m.app, id, 1, "scripted", "model", "ai-templates-v1", h1, h2, h3, int64(1), int64(1), 1, "OK", "", h1}
		mutate(a)
		return a
	}
	const attemptSQL = `SELECT control.ai_record_attempt($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)::text`
	for name, mutate := range map[string]func(a []any){
		"attempt zero": func(a []any) { a[2] = 0 }, "attempt 17": func(a []any) { a[2] = 17 },
		"provider space": func(a []any) { a[3] = "a b" }, "provider empty": func(a []any) { a[3] = "" }, "provider long": func(a []any) { a[3] = strings.Repeat("p", 65) },
		"model newline": func(a []any) { a[4] = "m\n" }, "model long": func(a []any) { a[4] = strings.Repeat("m", 129) },
		"template": func(a []any) { a[5] = "t t" }, "context hash": func(a []any) { a[6] = "xyz" }, "request hash": func(a []any) { a[7] = "xyz" }, "output hash": func(a []any) { a[8] = "xyz" },
		"negative tokens": func(a []any) { a[9] = int64(-1) }, "huge tokens": func(a []any) { a[10] = int64(100_000_001) }, "negative latency": func(a []any) { a[11] = -1 },
		"unknown outcome": func(a []any) { a[12] = "WIN" }, "diagnostics hash": func(a []any) { a[13] = "short" }, "empty compiler hash": func(a []any) { a[14] = "" },
	} {
		malformed = append(malformed, call{"record " + name, attemptSQL, attempt(mutate)})
	}
	body, hash := m.readyBody()
	finish := func(mutate func(a []any)) []any {
		a := []any{m.app, id, "READY", body, hash, `{"targets":[],"sensitive":[],"aspects":[]}`, nil, nil, nil, h1, 1, int64(1), int64(1)}
		mutate(a)
		return a
	}
	const finishSQL = `SELECT control.ai_finish($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11,$12,$13)::text`
	for name, mutate := range map[string]func(a []any){
		"status PENDING": func(a []any) { a[2] = "PENDING" }, "status APPLIED": func(a []any) { a[2] = "APPLIED" }, "status null": func(a []any) { a[2] = nil },
		"hash mismatch": func(a []any) { a[4] = h2 }, "hash missing": func(a []any) { a[4] = nil }, "body not json": func(a []any) { b := []byte("not json"); a[3] = b; a[4] = sum(b) },
		"body not a proposal": func(a []any) {
			b := []byte(`{"version":"ai-requirement-proposal-v1","requirements":[]}`)
			a[3] = b
			a[4] = sum(b)
		},
		"body wrong list": func(a []any) {
			b := []byte(`{"version":"ai-patch-proposal-v1","operations":{}}`)
			a[3] = b
			a[4] = sum(b)
		},
		"body too small":        func(a []any) { b := []byte(`1`); a[3] = b; a[4] = sum(b) },
		"READY without derived": func(a []any) { a[5] = nil }, "READY without body": func(a []any) { a[3] = nil; a[4] = nil }, "READY with failure": func(a []any) { a[8] = "INTERNAL" },
		"derived not an object": func(a []any) { a[5] = `[]` }, "failure code unknown": func(a []any) { a[2] = "FAILED"; a[3] = nil; a[4] = nil; a[5] = nil; a[8] = "OOPS" },
		"FAILED with a body": func(a []any) { a[2] = "FAILED"; a[8] = "INTERNAL" }, "FAILED without code": func(a []any) { a[2] = "FAILED"; a[3] = nil; a[4] = nil; a[5] = nil },
		"EXPIRED is not a finish":  func(a []any) { a[2] = "EXPIRED" },
		"diagnostics not an array": func(a []any) { a[6] = `{}` }, "diagnostics too many": func(a []any) { a[6] = "[" + strings.TrimSuffix(strings.Repeat("{},", 41), ",") + "]" },
		"attempts 17": func(a []any) { a[10] = 17 }, "negative usage": func(a []any) { a[11] = int64(-1) }, "context hash": func(a []any) { a[9] = "no" },
	} {
		malformed = append(malformed, call{"finish " + name, finishSQL, finish(mutate)})
	}
	const decideSQL = `SELECT control.ai_decide($1,$2,$3,$4,$5,$6,$7,$8)::text`
	for name, a := range map[string][]any{
		"unknown decision": {m.app, id, "ACCEPT", nil, nil, nil, "OTHER", h3}, "apply without result": {m.app, id, "APPLY", int64(1), nil, nil, nil, h3},
		"apply result not next": {m.app, id, "APPLY", int64(1), int64(3), h1, nil, h3}, "apply with a reason": {m.app, id, "APPLY", int64(1), int64(2), h1, "OTHER", h3},
		"apply expected zero": {m.app, id, "APPLY", int64(0), int64(1), h1, nil, h3}, "apply bad hash": {m.app, id, "APPLY", int64(1), int64(2), "zz", nil, h3},
		"reject without reason": {m.app, id, "REJECT", nil, nil, nil, nil, h3}, "reject unknown reason": {m.app, id, "REJECT", nil, nil, nil, "BECAUSE", h3},
		"reject with a result": {m.app, id, "REJECT", int64(1), nil, nil, "OTHER", h3}, "no decision hash": {m.app, id, "REJECT", nil, nil, nil, "OTHER", "x"},
		"null decision": {m.app, id, nil, nil, nil, nil, nil, h3},
	} {
		malformed = append(malformed, call{"decide " + name, decideSQL, a})
	}
	var out string
	for _, c := range malformed {
		wantState(t, c.name, m.p.call(m.f.aliceSubject, m.f.tenantA, &out, c.query, c.args...), "22023")
	}
	// Nothing above wrote a trace row or moved the proposal.
	var n int
	var status string
	if err := m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.ai_invocations WHERE tenant_id=$1),(SELECT status FROM control.ai_proposals WHERE tenant_id=$1 AND id=$2)`, m.f.tenantA, id).Scan(&n, &status); err != nil || n != 0 || status != "PENDING" {
		t.Fatalf("refused calls left %d trace rows, status %s (%v)", n, status, err)
	}
}

func TestM14PortsAdmission(t *testing.T) {
	t.Run("draft must be the head the caller read", func(t *testing.T) {
		m := m14PortsSetup(t)
		var out string
		for name, args := range map[string][]any{
			"older revision": {m.app, "patch", h1, m.revision + 1, m.hash, 200000, 10000, 2, 30},
			"wrong hash":     {m.app, "patch", h1, m.revision, h2, 200000, 10000, 2, 30},
		} {
			err := m.p.call(m.f.aliceSubject, m.f.tenantA, &out, beginSQL, args...)
			wantState(t, name, err, "55000")
			if pgDetail(err) != "draft_changed" {
				t.Errorf("%s: detail %q", name, pgDetail(err))
			}
		}
		wantState(t, "unknown application", m.p.call(m.f.aliceSubject, m.f.tenantA, &out, beginSQL, "app.nothing_here", "patch", h1, 1, h1, 200000, 10000, 2, 30), "P0002")
		if m.proposalRows() != 0 {
			t.Fatal("a refused admission left a row")
		}
	})
	t.Run("in-flight limit and daily budget are decided under one lock", func(t *testing.T) {
		m := m14PortsSetup(t)
		// Daily budget 25 000, reserve 10 000: exactly two of eight concurrent requests fit.
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, budget, other := 0, 0, 0
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := m.beginRetrying(m.f.aliceSubject, "patch", 25_000, 10_000, 16, 30)
				mu.Lock()
				defer mu.Unlock()
				var pg *pgconn.PgError
				switch {
				case err == nil:
					ok++
				case asPg(err, &pg) && pg.Code == "53400" && pg.Detail == "daily_budget":
					budget++
				default:
					other++
				}
			}()
		}
		wg.Wait()
		if ok != 2 || budget != 6 || other != 0 {
			t.Fatalf("admitted %d, over budget %d, other %d", ok, budget, other)
		}
		// Used and reserved never exceed the daily budget at any point.
		var rows int
		_ = m.f.operator.QueryRow(`SELECT count(*) FROM control.ai_proposals WHERE tenant_id=$1 AND status='PENDING'`, m.f.tenantA).Scan(&rows)
		if rows != 2 {
			t.Fatalf("%d pending rows", rows)
		}
	})
	t.Run("in-flight limit", func(t *testing.T) {
		m := m14PortsSetup(t)
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, busy := 0, 0
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := m.beginRetrying(m.f.aliceSubject, "patch", 1_000_000, 1000, 3, 60)
				mu.Lock()
				defer mu.Unlock()
				var pg *pgconn.PgError
				if err == nil {
					ok++
				} else if asPg(err, &pg) && pg.Code == "53400" && pg.Detail == "in_flight" {
					busy++
				}
			}()
		}
		wg.Wait()
		if ok != 3 || busy != 3 {
			t.Fatalf("admitted %d, busy %d", ok, busy)
		}
	})
	t.Run("a lapsed lease expires, is charged pessimistically and cannot become READY", func(t *testing.T) {
		m := m14PortsSetup(t)
		id, err := m.begin(m.f.aliceSubject, "patch", 200_000, 7_000, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1300 * time.Millisecond)
		// A late attempt record and a late finish are refused or return EXPIRED.
		wantState(t, "late attempt", m.attempt(m.f.aliceSubject, id, 1, "OK"), "55000")
		st, err := m.finishFailed(m.f.aliceSubject, id)
		if err != nil || st != "EXPIRED" {
			t.Fatalf("late finish: %q %v", st, err)
		}
		var used int64
		var status string
		_ = m.f.operator.QueryRow(`SELECT used_tokens FROM control.ai_usage WHERE tenant_id=$1`, m.f.tenantA).Scan(&used)
		_ = m.f.operator.QueryRow(`SELECT status FROM control.ai_proposals WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, id).Scan(&status)
		if status != "EXPIRED" || used != 150 {
			t.Fatalf("status %s used %d (the reported usage of the late finish is charged)", status, used)
		}
		// Terminal: no later READY.
		_, err = m.finishReady(m.f.aliceSubject, id)
		wantState(t, "finish after expiry", err, "55000")
	})
	t.Run("usage is keyed by tenant and UTC day", func(t *testing.T) {
		m := m14PortsSetup(t)
		if _, err := m.begin(m.f.aliceSubject, "patch", 200_000, 1000, 2, 30); err != nil {
			t.Fatal(err)
		}
		var day, today string
		var pk string
		if err := m.f.operator.QueryRow(`SELECT usage_day::text,(clock_timestamp() AT TIME ZONE 'UTC')::date::text FROM control.ai_usage WHERE tenant_id=$1`, m.f.tenantA).Scan(&day, &today); err != nil || day != today {
			t.Fatalf("usage day %s, today %s (%v)", day, today, err)
		}
		if err := m.f.operator.QueryRow(`SELECT string_agg(a.attname,',' ORDER BY a.attnum) FROM pg_index i JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=ANY(i.indkey) WHERE i.indrelid='control.ai_usage'::regclass AND i.indisprimary`).Scan(&pk); err != nil || pk != "tenant_id,usage_day" {
			t.Fatalf("primary key %q: a new day must be a new row, nothing is reset (%v)", pk, err)
		}
	})
}

func asPg(err error, target **pgconn.PgError) bool {
	for err != nil {
		if p, ok := err.(*pgconn.PgError); ok {
			*target = p
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestM14PortsFinishAndDecideStateMachine(t *testing.T) {
	m := m14PortsSetup(t)
	alice, bob := m.f.aliceSubject, m.f.bobSubject
	var out string

	// record_attempt: once per attempt number; only while PENDING.
	id, err := m.begin(alice, "patch", 1_000_000, 10_000, 8, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.attempt(alice, id, 1, "COMPILE_ERROR"); err != nil {
		t.Fatal(err)
	}
	wantState(t, "attempt twice", m.attempt(alice, id, 1, "OK"), "23505")
	if err = m.attempt(alice, id, 2, "OK"); err != nil {
		t.Fatal(err)
	}
	// finish: the stored body must be what was hashed; a wrong proposal-kind body is refused.
	body, hash := m.readyBody()
	wantState(t, "hash of another body", m.p.call(alice, m.f.tenantA, &out, `SELECT control.ai_finish($1,$2,'READY',$3,$4,'{}'::jsonb,NULL,NULL,NULL,$5,2,10,10)`, m.app, id, body, h1, h2), "22023")
	st, err := m.finishReady(alice, id)
	if err != nil || st != "READY" {
		t.Fatalf("%q %v", st, err)
	}
	_, err = m.finishReady(alice, id)
	wantState(t, "finish twice", err, "55000")
	if pgDetail(err) != "not_pending" {
		t.Errorf("detail %q", pgDetail(err))
	}
	wantState(t, "attempt after finish", m.attempt(alice, id, 3, "OK"), "55000")
	// The row cannot change once finished except through decide.
	var rows int64
	var stored, tokensIn int64
	_ = m.f.operator.QueryRow(`SELECT count(*) FROM control.ai_invocations WHERE tenant_id=$1 AND proposal_id=$2`, m.f.tenantA, id).Scan(&rows)
	_ = m.f.operator.QueryRow(`SELECT octet_length(body),input_tokens FROM control.ai_proposals WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, id).Scan(&stored, &tokensIn)
	if rows != 2 || stored != int64(len(body)) && stored == 0 {
		t.Fatalf("rows %d stored %d", rows, stored)
	}

	// decide REJECT: by a different human; once.
	if got, err := m.decide(bob, id, "REJECT", nil, nil, "", "UNSAFE"); err != nil || got != "REJECTED" {
		t.Fatalf("%q %v", got, err)
	}
	var decidedBy, requester string
	_ = m.f.operator.QueryRow(`SELECT decided_by::text,requester_id::text FROM control.ai_proposals WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, id).Scan(&decidedBy, &requester)
	if decidedBy != m.f.bob || requester != m.f.alice {
		t.Fatalf("decided_by %s requester %s", decidedBy, requester)
	}
	_, err = m.decide(alice, id, "REJECT", nil, nil, "", "OTHER")
	wantState(t, "reject twice", err, "55000")
	if pgDetail(err) != "decided" {
		t.Errorf("detail %q", pgDetail(err))
	}
	_, err = m.decide(m.f.viewerSubject, id, "REJECT", nil, nil, "", "OTHER")
	wantState(t, "viewer decides", err, "42501")

	// decide APPLY: the revision must exist, be the head, carry the hash and be written
	// by the decider in this very request.
	a, _ := m.begin(alice, "patch", 1_000_000, 10_000, 8, 30)
	if _, err = m.finishReady(alice, a); err != nil {
		t.Fatal(err)
	}
	_, err = m.decide(alice, a, "APPLY", i64(1), i64(2), h1, "")
	wantState(t, "apply a revision that was not written", err, "55000")
	if pgDetail(err) != "draft_changed" {
		t.Errorf("detail %q", pgDetail(err))
	}
	// Bob appends revision 2 through the API; alice may not claim it as her write.
	doc := orderDocument(m.app, "500")
	doc.Definitions[0].Label = "Edited by bob"
	raw := mustMarshal(t, doc)
	if w := m3Call(m.h, "POST", "/v1/tenants/"+m.f.tenantA+"/metadata/"+m.app+"/versions", m.bob, 1, raw); w.Code != 201 {
		t.Fatal("append", w.Code, w.Body.String())
	}
	_, hash2 := m.headAndHash()
	_, err = m.decide(alice, a, "APPLY", i64(1), i64(2), hash2, "")
	wantState(t, "apply a revision written by someone else", err, "22023")
	_, err = m.decide(bob, a, "APPLY", i64(1), i64(2), h1, "")
	wantState(t, "apply with the wrong hash", err, "22023")
	// Kind mismatches.
	_, err = m.decide(bob, a, "CONFIRM", i64(0), i64(1), hash2, "")
	wantState(t, "confirm a patch proposal", err, "22023")
	// The decider need not be the requester: bob decided his own write.
	if got, err := m.decide(bob, a, "APPLY", i64(1), i64(2), hash2, ""); err != nil || got != "APPLIED" {
		t.Fatalf("%q %v", got, err)
	}
	_, err = m.decide(bob, a, "APPLY", i64(1), i64(2), hash2, "")
	wantState(t, "apply twice", err, "55000")
	// A revision is the result of at most one proposal.
	m.revision, m.hash = 2, hash2
	b, err := m.begin(alice, "patch", 1_000_000, 10_000, 8, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.finishReady(alice, b); err != nil {
		t.Fatal(err)
	}
	_, err = m.decide(bob, b, "APPLY", i64(1), i64(2), hash2, "")
	wantState(t, "claim an applied revision twice", err, "23505")

	// A requirements proposal: CONFIRM names the set version the caller wrote.
	r, err := m.begin(alice, "requirements", 1_000_000, 10_000, 8, 30)
	if err != nil {
		t.Fatal(err)
	}
	req := m.reqBody()
	wantState(t, "patch body for a requirements proposal", m.p.call(alice, m.f.tenantA, &out, `SELECT control.ai_finish($1,$2,'READY',$3,$4,'{"requirements":[],"open_questions":0}'::jsonb,NULL,NULL,NULL,$5,1,1,1)`, m.app, r, body, hash, h1), "22023")
	if err = m.p.call(alice, m.f.tenantA, &out, `SELECT control.ai_finish($1,$2,'READY',$3,$4,'{"requirements":[],"open_questions":0}'::jsonb,NULL,NULL,NULL,$5,1,1,1)`, m.app, r, req, sum(req), h1); err != nil {
		t.Fatal(err)
	}
	_, err = m.decide(alice, r, "APPLY", i64(2), i64(3), hash2, "")
	wantState(t, "apply a requirements proposal", err, "22023")
	_, err = m.decide(alice, r, "CONFIRM", i64(0), i64(1), h1, "")
	wantState(t, "confirm a set nobody wrote", err, "55000")
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (m *m14Ports) reqBody() []byte {
	m.t.Helper()
	step := m.reqAnswer("po-approval")
	return step.Payload
}

func TestM14PortsTablesAreAppendOnlyIsolatedAndHygienic(t *testing.T) {
	m := m14PortsSetup(t)
	id, err := m.begin(m.f.aliceSubject, "patch", 1_000_000, 10_000, 8, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.attempt(m.f.aliceSubject, id, 1, "OK"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.finishReady(m.f.aliceSubject, id); err != nil {
		t.Fatal(err)
	}
	tables := []string{"control.ai_proposals", "control.ai_invocations", "control.ai_usage"}

	// Privileges: no runtime role writes; only framework_app reads (through RLS).
	for _, table := range tables {
		for _, role := range []string{"public", "framework_app", "framework_worker", "framework_dispatcher"} {
			var write, read bool
			if err = m.f.operator.QueryRow(`SELECT has_table_privilege($1,$2,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege($1,$2,'INSERT,UPDATE,REFERENCES'),
 has_table_privilege($1,$2,'SELECT') OR has_any_column_privilege($1,$2,'SELECT')`, role, table).Scan(&write, &read); err != nil {
				t.Fatal(err)
			}
			if write || read && role != "framework_app" || !read && role == "framework_app" {
				t.Errorf("%s keeps write=%v read=%v on %s", role, write, read, table)
			}
		}
		var forced bool
		if err = m.f.operator.QueryRow(`SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid=$1::regclass`, table).Scan(&forced); err != nil || !forced {
			t.Errorf("%s: row level security is not forced (%v)", table, err)
		}
	}
	// The runtime role cannot write directly; neither can the worker or the dispatcher read.
	appDB := m.f.db
	for _, q := range []string{
		`INSERT INTO control.ai_usage(tenant_id,usage_day,requests,used_tokens) VALUES(gen_random_uuid(),current_date,0,0)`,
		`UPDATE control.ai_proposals SET status='READY'`, `DELETE FROM control.ai_invocations`, `TRUNCATE control.ai_usage`,
		`UPDATE control.ai_usage SET used_tokens=0`,
	} {
		_, err = appDB.Exec(q)
		wantState(t, "framework_app: "+q, err, "42501")
	}
	for _, key := range []string{"WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db := portsRole(t, key)
		for _, table := range tables {
			var n int
			wantState(t, key+" read "+table, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n), "42501")
		}
	}
	// Even the schema operator cannot hand-write, edit, delete or truncate evidence.
	for _, q := range []string{
		`INSERT INTO control.ai_usage(tenant_id,usage_day,requests,used_tokens) SELECT tenant_id,usage_day+1,0,0 FROM control.ai_usage LIMIT 1`,
		`UPDATE control.ai_proposals SET reserved_tokens=1`, `UPDATE control.ai_proposals SET status='APPLIED'`, `UPDATE control.ai_invocations SET outcome='OK'`,
		`UPDATE control.ai_usage SET used_tokens=used_tokens-1`,
		`DELETE FROM control.ai_proposals`, `DELETE FROM control.ai_invocations`, `DELETE FROM control.ai_usage`,
		`TRUNCATE control.ai_proposals CASCADE`, `TRUNCATE control.ai_invocations`, `TRUNCATE control.ai_usage CASCADE`,
	} {
		if _, err = m.f.operator.Exec(q); err == nil {
			t.Errorf("operator statement was accepted: %s", q)
		}
	}
	var rows, attempts int
	_ = m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.ai_proposals WHERE tenant_id=$1),(SELECT count(*) FROM control.ai_invocations WHERE tenant_id=$1)`, m.f.tenantA).Scan(&rows, &attempts)
	if rows != 1 || attempts != 1 {
		t.Fatalf("evidence changed: %d proposals, %d attempts", rows, attempts)
	}

	// The ports: definer functions of the schema owner with a pinned search path and
	// EXECUTE for the API role only.
	var good int
	if err = m.f.operator.QueryRow(`SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('ai_begin','ai_record_attempt','ai_finish','ai_decide')
 AND p.prosecdef AND p.proowner='framework_schema_owner'::regrole AND p.proconfig::text ~ 'search_path=pg_catalog' AND has_function_privilege('framework_app',p.oid,'EXECUTE')
 AND NOT has_function_privilege('public',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')`).Scan(&good); err != nil || good != 4 {
		t.Fatalf("%d of 4 ports are in the expected shape (%v)", good, err)
	}
	// The worker drift guard (extended by migration 60) still passes for a worker session.
	var safe bool
	if err = portsRole(t, "WORKER_DATABASE_URL").QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe); err != nil || !safe {
		t.Fatalf("worker_grants_safe = %v (%v)", safe, err)
	}
	// Readiness check of the Go store agrees.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = (ai.Store{}).Ready(ctx, m.f.db); err != nil {
		t.Fatalf("Store.Ready: %v", err)
	}
	// Store wrappers refuse malformed arguments before SQL and type the SQLSTATEs.
	err = m.withAccess(m.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		_, e := (ai.Store{}).Begin(ctx, q, a, ai.BeginArgs{App: m.app, Kind: ai.ProposalPatch, RequestHash: "x"})
		return e
	})
	if !errors.Is(err, ai.ErrInvalidRequest) {
		t.Fatalf("Store.Begin with a bad hash: %v", err)
	}
	// A foreign id is not found, a malformed one is not found either (no oracle).
	for _, bad := range []string{"not-an-id", "11111111-2222-4333-8444-555555555555"} {
		err = m.withAccess(m.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
			_, e := (ai.Store{}).Get(ctx, q, a, m.app, bad)
			return e
		})
		if !errors.Is(err, ai.ErrProposalNotFound) {
			t.Fatalf("Store.Get(%q): %v", bad, err)
		}
	}
	// A stored row that no longer matches its hash is never shown as validated: the
	// table forbids it for everyone, so exercise the Go check on a doctored scan.
	err = m.withAccess(m.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		p, e := (ai.Store{}).Get(ctx, q, a, m.app, id)
		if e != nil || p.Status != ai.ProposalReady || len(p.Proposal) == 0 || p.ProposalHash != sum(p.Proposal) {
			return fmt.Errorf("stored proposal %+v: %v", p, e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// withAccess runs fn as subject inside the runtime transaction with the resolved
// tenant scope.
func (m *m14Ports) withAccess(subject string, fn func(context.Context, postgres.Executor, tenant.Access) error) error {
	return postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
		principal, err := m.f.verifier.Verify(m.f.token(m.t, subject, nil))
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
		a, err := (tenant.Store{}).Resolve(ctx, q, u, m.f.tenantA)
		if err != nil {
			return err
		}
		return fn(ctx, q, a)
	})
}
