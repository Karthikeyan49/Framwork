//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func m9rReceiptPath(f *iamFixture, app, key string) string {
	return "/v1/tenants/" + f.tenantA + "/preview/" + app + "/commands/" + key
}
func m9rReceipt(t *testing.T, h http.Handler, path, token string) data.Receipt {
	t.Helper()
	w := m3Call(h, "GET", path, token, 0, nil)
	var receipt data.Receipt
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil {
		t.Fatal("receipt", w.Code, w.Body.String())
	}
	return receipt
}
func m9rCommandCounts(t *testing.T, f *iamFixture, app string, records, receipts, events int) {
	t.Helper()
	for _, check := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, records},
		{`SELECT count(*) FROM runtime.command_receipts WHERE tenant_id=$1 AND app_id=$2`, receipts},
		{`SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1 AND app_id=$2`, events},
		{`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND state->>'metadata_app'=$2 AND action IN('record.created','record.updated','record.deleted')`, events},
	} {
		var n int
		if err := f.operator.QueryRow(check.query, f.tenantA, app).Scan(&n); err != nil || n != check.want {
			t.Fatalf("committed fact count got=%d want=%d error=%v", n, check.want, err)
		}
	}
}

func TestM9RCommandReplayConflictIsolationAndImmutability(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	key := uuid(t)
	c := data.Command{Key: key, Operation: "create", Values: m5Values("Receipt values must not leak")}
	w := m5Call(h, base+"/commands", token, c)
	var original data.Record
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal("first command", w.Code, w.Body.String())
	}
	w = m5Call(h, base+"/commands", token, c)
	var replay data.Record
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || !replay.Replayed || replay.ID != original.ID || replay.Version != 1 || len(replay.Values) != 0 {
		t.Fatal("replay contract", w.Code, w.Body.String())
	}
	path := m9rReceiptPath(f, app, key)
	r := m9rReceipt(t, h, path, token)
	if r.State != "COMMITTED" || r.RecordID != original.ID || r.Version != 1 || strings.Contains(m3Call(h, "GET", path, token, 0, nil).Body.String(), "Receipt values") {
		t.Fatal("owned minimal receipt")
	}
	c.Values = m5Values("Changed under same key")
	if w = m5Call(h, base+"/commands", token, c); w.Code != 409 {
		t.Fatal("command key conflict", w.Code)
	}
	if r = m9rReceipt(t, h, m9rReceiptPath(f, app, uuid(t)), token); r.State != "UNKNOWN" || r.RecordID != "" {
		t.Fatal("absence is not an abort receipt")
	}
	if w = m3Call(h, "GET", path, f.token(t, f.bobSubject, nil), 0, nil); w.Code != 403 {
		t.Fatal("cross-tenant receipt")
	}
	if w = m3Call(h, "GET", path, f.token(t, f.viewerSubject, nil), 0, nil); w.Code != 403 {
		t.Fatal("viewer receipt")
	}
	for _, statement := range []string{`UPDATE runtime.command_receipts SET record_version=99 WHERE tenant_id=$1 AND app_id=$2`, `DELETE FROM runtime.command_receipts WHERE tenant_id=$1 AND app_id=$2`, `TRUNCATE runtime.command_receipts`} {
		tx, err := f.operator.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(statement, "TRUNCATE") {
			_, err = tx.Exec(statement)
		} else {
			_, err = tx.Exec(statement, f.tenantA, app)
		}
		tx.Rollback()
		if err == nil {
			t.Fatal("receipt history mutable")
		}
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
	// The SQL actor restriction holds even when product policy is bypassed.
	p, err := f.verifier.Verify(f.token(t, f.viewerSubject, nil))
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		a, err := m3Scope(f, ctx, q, p, f.tenantA)
		if err != nil {
			return err
		}
		r, err := data.CommandReceipt(ctx, q, a, app, key)
		if err == nil && (r.State != "UNKNOWN" || r.RecordID != "") {
			return errors.New("other actor receipt visible")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestM9RConcurrentSameKeyCreatesOneCommandFact(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	c := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Same user intent")}
	start := make(chan struct{})
	results := make(chan *httpResult, 8)
	for range 8 {
		go func() {
			<-start
			w := m5Call(h, base+"/commands", token, c)
			results <- &httpResult{w.Code, w.Body.Bytes()}
		}()
	}
	close(start)
	var id string
	for range 8 {
		w := <-results
		var r data.Record
		if w.status != 200 || json.Unmarshal(w.body, &r) != nil {
			t.Fatal("same-key command", w.status, string(w.body))
		}
		if id == "" {
			id = r.ID
		} else if id != r.ID {
			t.Fatal("duplicated command identity")
		}
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
}

type httpResult struct {
	status int
	body   []byte
}

// Both normal API transactions update their own row, then request the other's
// row lock. The server detects a real deadlock; the losing whole transaction is
// rolled back and retried. Only this owned test disables the 500 ms lock timeout
// so PostgreSQL's default 1 s deadlock detector fires first.
type m9rDeadlockAudit struct {
	deadlocks atomic.Int64
	a, b      string
	mu        sync.Mutex
	seen      map[string]bool
	entered   chan struct{}
	release   chan struct{}
}

func (w *m9rDeadlockAudit) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if e.Action == "record.updated" {
		w.mu.Lock()
		first := !w.seen[e.State.RecordID]
		w.seen[e.State.RecordID] = true
		w.mu.Unlock()
		if first {
			if _, err := q.ExecContext(ctx, `SELECT set_config('lock_timeout','0',true)`); err != nil {
				return err
			}
			w.entered <- struct{}{}
			select {
			case <-w.release:
			case <-ctx.Done():
				return ctx.Err()
			}
			other := w.a
			if e.State.RecordID == w.a {
				other = w.b
			}
			var id string
			if err := q.QueryRowContext(ctx, `SELECT id::text FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND entity_id='entity.contact' AND id=$3 FOR UPDATE`, e.TenantID, e.State.MetadataApp, other).Scan(&id); err != nil {
				var databaseError *pgconn.PgError
				if errors.As(err, &databaseError) && databaseError.Code == "40P01" {
					w.deadlocks.Add(1)
				}
				return err
			}
		}
	}
	return (audit.Postgres{}).Append(ctx, q, e)
}
func TestM9RActualDeadlockRetriesWithoutDuplicateFacts(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	a, b := m5Create(t, h, base, token, "A"), m5Create(t, h, base, token, "B")
	w := &m9rDeadlockAudit{a: a.ID, b: b.ID, seen: map[string]bool{}, entered: make(chan struct{}, 2), release: make(chan struct{})}
	h = m5Handler(t, f, policy.Native{}, w, true)
	results := make(chan int, 2)
	for _, r := range []data.Record{a, b} {
		c := data.Command{Key: uuid(t), Operation: "update", ID: r.ID, ExpectedVersion: 1, Values: m5Values("Deadlock recovered")}
		go func() { results <- m5Call(h, base+"/commands", token, c).Code }()
	}
	for range 2 {
		select {
		case <-w.entered:
		case <-time.After(2 * time.Second):
			close(w.release)
			t.Fatal("deadlock barrier not reached")
		}
	}
	close(w.release)
	for range 2 {
		if code := <-results; code != 200 {
			t.Fatal("deadlock recovery", code)
		}
	}
	if w.deadlocks.Load() != 1 {
		t.Fatal("server deadlock abort was not observed", w.deadlocks.Load())
	}
	m9rCommandCounts(t, f, app, 2, 2, 4)
}

func TestM9RConcurrentReferenceInsertionAndParentDeletion(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	for range 12 {
		parent := m5Create(t, h, base, token, "Parent")
		v := m5Values("Child")
		v["field.contact.parent"] = parent.ID
		create := data.Command{Key: uuid(t), Operation: "create", Values: v}
		remove := data.Command{Key: uuid(t), Operation: "delete", ID: parent.ID, ExpectedVersion: 1}
		start := make(chan struct{})
		results := make(chan int, 2)
		for _, c := range []data.Command{create, remove} {
			go func() { <-start; results <- m5Call(h, base+"/commands", token, c).Code }()
		}
		close(start)
		codes := []int{<-results, <-results}
		for _, code := range codes {
			if code != 200 && code != 400 && code != 409 {
				t.Fatal("uncontrolled reference race", codes)
			}
		}
		if codes[0] == 200 && codes[1] == 200 {
			t.Fatal("parent deletion and dependent insertion both committed")
		}
	}
	var dangling int
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.records source WHERE tenant_id=$1 AND app_id=$2 AND custom_data->>'field.contact.parent' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM runtime.records target WHERE target.tenant_id=source.tenant_id AND target.app_id=source.app_id AND target.metadata_revision=source.metadata_revision AND target.ir_hash=source.ir_hash AND target.entity_id='entity.contact' AND target.id::text=source.custom_data->>'field.contact.parent')`, f.tenantA, app).Scan(&dangling); err != nil || dangling != 0 {
		t.Fatal("dangling JSON reference", dangling, err)
	}
}

type m9rWriteGate struct {
	entered chan string
	release chan struct{}
}

func (g *m9rWriteGate) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if e.Action == "record.updated" {
		select {
		case g.entered <- e.State.RecordID:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-g.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return (audit.Postgres{}).Append(ctx, q, e)
}
func TestM9RIndependentWritersOverlapAndCASIsDefined(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	a, b := m5Create(t, h, base, token, "A"), m5Create(t, h, base, token, "B")
	gate := &m9rWriteGate{entered: make(chan string, 2), release: make(chan struct{})}
	h = m5Handler(t, f, policy.Native{}, gate, true)
	results := make(chan int, 2)
	for _, r := range []data.Record{a, b} {
		c := data.Command{Key: uuid(t), Operation: "update", ID: r.ID, ExpectedVersion: 1, Values: m5Values("Overlapped")}
		go func() { results <- m5Call(h, base+"/commands", token, c).Code }()
	}
	for range 2 {
		select {
		case <-gate.entered:
		case <-time.After(2 * time.Second):
			close(gate.release)
			t.Fatal("independent transactions did not overlap before COMMIT")
		}
	}
	close(gate.release)
	for range 2 {
		if code := <-results; code != 200 {
			t.Fatal("independent writer", code)
		}
	}
	// Both same-version writers start together. One advances the version and the
	// other gets the defined conflict after fresh-snapshot retry, never a 503.
	start := make(chan struct{})
	for range 2 {
		c := data.Command{Key: uuid(t), Operation: "update", ID: a.ID, ExpectedVersion: 2, Values: m5Values("CAS winner")}
		go func() { <-start; results <- m5Call(h, base+"/commands", token, c).Code }()
	}
	close(start)
	x, y := <-results, <-results
	if !(x == 200 && y == 409 || y == 200 && x == 409) {
		t.Fatalf("CAS outcomes %d/%d", x, y)
	}
	m9rCommandCounts(t, f, app, 2, 3, 5)
}

type m9rAbortAudit struct{ once atomic.Bool }

func (w *m9rAbortAudit) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if e.Action == "record.created" && w.once.CompareAndSwap(false, true) {
		_, err := q.ExecContext(ctx, `DO $$ BEGIN RAISE EXCEPTION 'injected serialization abort' USING ERRCODE='40001';END $$`)
		return err
	}
	return (audit.Postgres{}).Append(ctx, q, e)
}
func TestM9RKnownSerializationRollbackRetriesWholeCommand(t *testing.T) {
	m8Database(t)
	f, _, base, token, app := m5Fixture(t)
	w := &m9rAbortAudit{}
	h := m5Handler(t, f, policy.Native{}, w, true)
	c := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Exactly one local commit")}
	if result := m5Call(h, base+"/commands", token, c); result.Code != 200 || !w.once.Load() {
		t.Fatal("known abort retry", result.Code)
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
}

func TestM9RUnknownCommandCommitIsReconciledWithoutRepeatingEffects(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
	db, err := postgres.Open(proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Prime the proxy's first COMMIT; the next actual command loses its reply.
	if err = postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }); err != nil {
		t.Fatal(err)
	}
	a := access.API{DB: db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true}
	faulted := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: a.Register, AllowPOST: a.AcceptsPOST})
	c := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Lost COMMIT reply")}
	if w := m5Call(faulted, base+"/commands", token, c); w.Code != 503 || !observed.Load() {
		t.Fatal("unknown commit not reproduced", w.Code, observed.Load())
	}
	r := m9rReceipt(t, h, m9rReceiptPath(f, app, c.Key), token)
	if r.State != "COMMITTED" || r.Version != 1 {
		t.Fatal("committed receipt missing")
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
	if w := m5Call(h, base+"/commands", token, c); w.Code != 200 {
		t.Fatal("confirmed replay", w.Code)
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
}

type m9rRollbackAudit struct{}

func (m9rRollbackAudit) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if e.Action == "record.created" {
		return errors.New("injected audit unavailable")
	}
	return (audit.Postgres{}).Append(ctx, q, e)
}
func TestM9RReceiptMutationAuditAndOutboxRollbackTogether(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	c := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Rolled back")}
	faulted := m5Handler(t, f, policy.Native{}, m9rRollbackAudit{}, true)
	if w := m5Call(faulted, base+"/commands", token, c); w.Code != 503 {
		t.Fatal("audit fault", w.Code)
	}
	m9rCommandCounts(t, f, app, 0, 0, 0)
	if r := m9rReceipt(t, h, m9rReceiptPath(f, app, c.Key), token); r.State != "UNKNOWN" {
		t.Fatal("rolled back receipt")
	}
	if w := m5Call(h, base+"/commands", token, c); w.Code != 200 {
		t.Fatal("explicit retry after confirmed test rollback", w.Code)
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
}
