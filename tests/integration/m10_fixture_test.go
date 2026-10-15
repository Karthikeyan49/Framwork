//go:build integration

package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

const (
	m10ProdHost    = "provider.example.test"
	m10SandboxHost = "sandbox.example.test"
)

// m10Call is one request observed by the fake provider.
type m10Call struct {
	Host, Method, Path, Key, Auth, Body string
}

// m10Provider is a deterministic HTTPS provider with real idempotency
// semantics per route and injectable failures. Effects are counted separately
// from requests so duplicate delivery is observable.
type m10Provider struct {
	mu       sync.Mutex
	srv      *httptest.Server
	calls    []m10Call
	effects  map[string]string
	unkeyed  int
	next     int
	behavior func(c m10Call, n int) string // "", "drop", "500", "slow:<ms>"
	port     int
	pool     *x509.CertPool
}

func newM10Provider(t *testing.T) *m10Provider {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "m10 provider"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, DNSNames: []string{m10ProdHost, m10SandboxHost},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), mustPKCS8(t, key))
	if err != nil {
		t.Fatal(err)
	}
	p := &m10Provider{effects: map[string]string{}, pool: x509.NewCertPool()}
	parsed, _ := x509.ParseCertificate(der)
	p.pool.AddCert(parsed)
	p.srv = httptest.NewUnstartedServer(http.HandlerFunc(p.serve))
	p.srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	p.srv.StartTLS()
	t.Cleanup(p.srv.Close)
	p.port = p.srv.Listener.Addr().(*net.TCPAddr).Port
	return p
}
func mustPKCS8(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func (p *m10Provider) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	c := m10Call{Host: strings.Split(r.Host, ":")[0], Method: r.Method, Path: r.URL.Path, Key: r.Header.Get("Idempotency-Key"), Auth: r.Header.Get("Authorization"), Body: string(raw)}
	p.mu.Lock()
	p.calls = append(p.calls, c)
	n := len(p.calls)
	behavior := p.behavior
	p.mu.Unlock()
	action := ""
	if behavior != nil {
		action = behavior(c, n)
	}
	reply := func(status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
	switch {
	case action == "reject500":
		reply(500, `{}`)
	case action == "400":
		reply(400, `{}`)
	case action == "429":
		reply(429, `{}`)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/contacts/"):
		reply(200, `{"risk_score":7}`)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/notify-l/"):
		p.mu.Lock()
		id, ok := p.effects[strings.TrimPrefix(r.URL.Path, "/notify-l/")]
		p.mu.Unlock()
		if !ok {
			reply(404, `{}`)
			return
		}
		reply(200, fmt.Sprintf(`{"notification_id":%q}`, id))
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/notify"):
		p.mu.Lock()
		var id string
		if c.Key != "" {
			if existing, ok := p.effects[c.Key]; ok {
				id = existing
			}
		}
		if id == "" {
			p.next++
			id = "n-" + strconv.Itoa(p.next)
			if c.Key != "" {
				p.effects[c.Key] = id
			} else {
				p.unkeyed++
			}
		}
		p.mu.Unlock()
		switch {
		case action == "drop":
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
		case action == "500":
			reply(500, `{}`)
		case strings.HasPrefix(action, "slow:"):
			ms, _ := strconv.Atoi(strings.TrimPrefix(action, "slow:"))
			time.Sleep(time.Duration(ms) * time.Millisecond)
			reply(200, fmt.Sprintf(`{"notification_id":%q}`, id))
		default:
			reply(200, fmt.Sprintf(`{"notification_id":%q}`, id))
		}
	default:
		reply(404, `{}`)
	}
}

func (p *m10Provider) setBehavior(f func(c m10Call, n int) string) {
	p.mu.Lock()
	p.behavior = f
	p.mu.Unlock()
}

// requests counts calls by host and path prefix ("" matches all).
func (p *m10Provider) requests(host, prefix string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		if (host == "" || c.Host == host) && strings.HasPrefix(c.Path, prefix) {
			n++
		}
	}
	return n
}

// effectCount is the number of distinct real-world effects created.
func (p *m10Provider) effectCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.effects) + p.unkeyed
}
func (p *m10Provider) lastCall() m10Call {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[len(p.calls)-1]
}

type m10Resolver struct{}

func (m10Resolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	switch host {
	case m10ProdHost, m10SandboxHost:
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return nil, errors.New("no such host")
}

// m10Secrets records every resolution; production references must never be
// resolved by a sandbox mode.
type m10Secrets struct {
	mu   sync.Mutex
	refs []string
}

func (s *m10Secrets) Resolve(_ context.Context, ref string) (string, error) {
	s.mu.Lock()
	s.refs = append(s.refs, ref)
	s.mu.Unlock()
	switch ref {
	case "prod_token", "sandbox_token":
		return "tok-" + ref, nil
	}
	return "", errors.New("unknown secret")
}
func (s *m10Secrets) resolved(ref string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.refs {
		if r == ref {
			n++
		}
	}
	return n
}

// m10Exec observes the real gateway so a test can inject a crash between the
// remote response and the fenced outcome write.
type m10Exec struct {
	inner tools.Executor
	after func(tools.Request, tools.Result)
	mu    sync.Mutex
	reqs  []tools.Request
}

func (e *m10Exec) Execute(ctx context.Context, r tools.Request) tools.Result {
	res := e.inner.Execute(ctx, r)
	e.mu.Lock()
	e.reqs = append(e.reqs, r)
	hook := e.after
	e.mu.Unlock()
	if hook != nil {
		hook(r, res)
	}
	return res
}
func (e *m10Exec) setAfter(f func(tools.Request, tools.Result)) {
	e.mu.Lock()
	e.after = f
	e.mu.Unlock()
}
func (e *m10Exec) calls() []tools.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tools.Request(nil), e.reqs...)
}

func m10Endpoint(host string, port int, secret string) tools.Endpoint {
	return tools.Endpoint{Host: host, Port: port, Network: "internal", SecretRef: secret}
}
func m10Retry() map[string]any {
	return map[string]any{"max_attempts": 3, "initial_backoff_ms": 100, "max_backoff_ms": 400}
}

// m10Tools builds one governed tool per delivery capability.
func m10Tools(port int) []tools.Definition {
	prod, sandbox := m10Endpoint(m10ProdHost, port, "prod_token"), m10Endpoint(m10SandboxHost, port, "sandbox_token")
	input := []tools.Field{{Name: "contact_id", Type: "string", Required: true, MaxLength: 64, In: "body"}, {Name: "name", Type: "string", Required: true, MaxLength: 200, In: "body"}}
	output := []tools.Field{{Name: "notification_id", Type: "string", Required: true, MaxLength: 64}}
	write := func(ref, path string, d tools.DeliverySpec, header string) tools.Definition {
		return tools.Definition{
			Ref: ref, Version: "1.0.0", SideEffect: "WRITE", Approval: "REQUIRED", Delivery: d,
			Connector: tools.Connector{Endpoint: prod, Method: "POST", Path: path, IdempotencyHeader: header, TimeoutMS: 1000, MaxResponseBytes: 2048, RatePerSecond: 50},
			Input:     input, Output: output,
			Sandbox: tools.SandboxSpec{Mode: "TEST_ACCOUNT", Endpoint: &sandbox},
			Tenants: []string{"*"},
		}
	}
	native := write("tool.notify_native", "/notify", tools.DeliverySpec{IdempotencyMode: "NATIVE_KEY", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL", ReplayWindowMS: 3600000}, "Idempotency-Key")
	lookup := write("tool.notify_lookup", "/notify-l", tools.DeliverySpec{IdempotencyMode: "LOOKUP_BY_REFERENCE", RetryMode: "SAFE_RETRY", ReconciliationMode: "REMOTE_LOOKUP"}, "Idempotency-Key")
	lookup.Connector.Lookup = &tools.Lookup{Method: "GET", Path: "/notify-l/{key}"}
	dedupe := write("tool.notify_dedupe", "/notify-d", tools.DeliverySpec{IdempotencyMode: "DEDUPE_WINDOW", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL", ReplayWindowMS: 3600000}, "Idempotency-Key")
	expired := write("tool.notify_expired", "/notify-d", tools.DeliverySpec{IdempotencyMode: "DEDUPE_WINDOW", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL", ReplayWindowMS: 1}, "Idempotency-Key")
	none := write("tool.notify_none", "/notify-n", tools.DeliverySpec{IdempotencyMode: "NONE", RetryMode: "RECONCILE_FIRST", ReconciliationMode: "MANUAL"}, "")
	read := tools.Definition{
		Ref: "tool.contact_lookup", Version: "1.0.0", SideEffect: "READ", Approval: "NONE",
		Delivery:  tools.DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: tools.Connector{Endpoint: prod, Method: "GET", Path: "/contacts/{contact_id}", TimeoutMS: 1000, MaxResponseBytes: 2048, RatePerSecond: 50},
		Input:     []tools.Field{{Name: "contact_id", Type: "string", Required: true, MaxLength: 64, In: "path"}},
		Output:    []tools.Field{{Name: "risk_score", Type: "integer", Required: true}},
		Sandbox:   tools.SandboxSpec{Mode: "TEST_ACCOUNT", Endpoint: &sandbox}, Tenants: []string{"*"},
	}
	simulated := read
	simulated.Ref = "tool.contact_simulated"
	simulated.Sandbox = tools.SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{"risk_score": json.RawMessage(`7`)}}
	blocked := read
	blocked.Ref = "tool.contact_blocked"
	blocked.Sandbox = tools.SandboxSpec{Mode: "BLOCK"}
	return []tools.Definition{native, lookup, dedupe, expired, none, read, simulated, blocked}
}

// m10Flow is a bounded workflow: start -> [approval] -> tool -> complete.
func m10Flow(tool string, inputs map[string]string, approval bool) map[string]any {
	cond := map[string]any{"op": "eq", "args": []any{map[string]any{"op": "field", "field_ref": "field.contact.status"}, map[string]any{"op": "literal", "value": "active"}}}
	first := "state.tool"
	states := []any{
		map[string]any{"id": "state.complete", "type": "complete", "transitions": []any{}},
		map[string]any{"id": "state.failed", "type": "failed", "transitions": []any{}},
		map[string]any{"id": "state.tool", "type": "activity", "activity": map[string]any{"tool_ref": tool, "version": "1.0.0", "inputs": inputs, "retry": m10Retry()}, "transitions": []any{map[string]any{"to": "state.complete"}}},
	}
	if approval {
		first = "state.approval"
		states = append(states, map[string]any{"id": "state.approval", "type": "approval", "approval_roles": []any{"admin", "owner"}, "approval_timeout_ms": 300000, "transitions": []any{map[string]any{"to": "state.tool", "on": "approved"}, map[string]any{"to": "state.failed", "on": "rejected"}}})
	}
	states = append(states, map[string]any{"id": "state.start", "type": "start", "transitions": []any{map[string]any{"to": first, "condition": cond}, map[string]any{"to": "state.failed"}}})
	return map[string]any{"dsl_version": "2.0.0", "version": "1.0.0", "timeout_ms": 600000, "entity_ref": "entity.contact", "trigger": "manual", "initial_state": "state.start", "states": states}
}

type m10Options struct {
	workerCatalog  *tools.Catalog // nil = same as the API catalog
	allowInternal  bool
	tenantOnly     string // restrict every tool to one tenant ("" = all)
	omitFromWorker bool
}

type m10Fixture struct {
	*m9Fixture
	provider *m10Provider
	secrets  *m10Secrets
	exec     *m10Exec
	gateway  *tools.Gateway
	catalog  tools.Catalog
	root     string
	token    string
}

func (m *m10Fixture) flow(name string) string { return m.root + "/workflows/workflow.contact." + name }

var m10Flows = map[string]struct {
	tool     string
	approval bool
	inputs   map[string]string
}{
	"native":    {"tool.notify_native", true, map[string]string{"contact_id": "field.contact.id", "name": "field.contact.name"}},
	"lookup":    {"tool.notify_lookup", true, map[string]string{"contact_id": "field.contact.id", "name": "field.contact.name"}},
	"dedupe":    {"tool.notify_dedupe", true, map[string]string{"contact_id": "field.contact.id", "name": "field.contact.name"}},
	"expired":   {"tool.notify_expired", true, map[string]string{"contact_id": "field.contact.id", "name": "field.contact.name"}},
	"none":      {"tool.notify_none", true, map[string]string{"contact_id": "field.contact.id", "name": "field.contact.name"}},
	"read":      {"tool.contact_lookup", false, map[string]string{"contact_id": "field.contact.id"}},
	"simulated": {"tool.contact_simulated", false, map[string]string{"contact_id": "field.contact.id"}},
	"blocked":   {"tool.contact_blocked", false, map[string]string{"contact_id": "field.contact.id"}},
}

func m10Setup(t *testing.T, o m10Options) *m10Fixture {
	t.Helper()
	m8Database(t)
	f := newIAM(t)
	provider := newM10Provider(t)
	definitions := m10Tools(provider.port)
	if o.tenantOnly != "" {
		for i := range definitions {
			definitions[i].Tenants = []string{o.tenantOnly}
		}
	}
	catalog, err := tools.NewCatalog(definitions)
	if err != nil {
		t.Fatal(err)
	}
	workerCatalog := catalog
	if o.workerCatalog != nil {
		workerCatalog = *o.workerCatalog
	}
	raw, err := os.ReadFile("../../contracts/examples/workflow-draft-v1.2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	app := "app.tools_" + strings.ReplaceAll(uuid(t), "-", "")
	doc["schema_version"], doc["app_id"] = "1.3.0", app
	var kept []any
	var template map[string]any
	for _, d := range doc["definitions"].([]any) {
		def := d.(map[string]any)
		def["schema_version"] = "1.3.0"
		if def["kind"] == "workflow" {
			template = def
			continue
		}
		kept = append(kept, def)
	}
	for name, flow := range m10Flows {
		def := map[string]any{}
		for k, v := range template {
			def[k] = v
		}
		def["id"], def["label"], def["workflow"] = "workflow.contact."+name, "Tool "+name, m10Flow(flow.tool, flow.inputs, flow.approval)
		kept = append(kept, def)
	}
	doc["definitions"] = kept
	raw, _ = json.Marshal(doc)

	compilerCatalog := compiler.PlatformCatalog()
	compilerCatalog.ToolCatalog = catalog
	api := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true, CompilerCatalog: &compilerCatalog, ToolCatalog: catalog}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	token := f.token(t, f.aliceSubject, nil)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/versions/1"
	if r := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 0, raw); r.Code != 201 {
		t.Fatal("schema 1.3 registry", r.Code, r.Body.String())
	}
	rec := m5Create(t, h, root+"/entities/entity.contact", token, "Tool snapshot")
	dispatcher, db := m8Roles(t)
	store := access.WorkerWorkflowStore(policy.Native{}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var job string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&job); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, job)
	})
	tx, e := f.operator.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = (tenant.Store{}).Provision(context.Background(), tx, f.tenantA, "Tools workspace", f.bob, "admin", true); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	secrets := &m10Secrets{}
	gateway, err := tools.NewGateway(tools.GatewayOptions{Secrets: secrets, AllowInternal: o.allowInternal, RootCAs: provider.pool, Resolver: m10Resolver{}})
	if err != nil {
		t.Fatal(err)
	}
	exec := &m10Exec{inner: gateway}
	store.Tools, store.ToolCatalog = exec, workerCatalog
	return &m10Fixture{
		m9Fixture: &m9Fixture{f: f, handler: h, worker: m9TestWorker{Worker: workflow.Worker{DB: db, Store: store}, dispatcher: dispatcher}, db: db, token: token, approver: f.token(t, f.bobSubject, nil), base: root + "/workflows/workflow.contact.read", entity: root + "/entities/entity.contact", record: rec, app: app, raw: raw},
		provider:  provider, secrets: secrets, exec: exec, gateway: gateway, catalog: catalog, root: root, token: token,
	}
}

// drive runs the worker until the instance reaches status, tolerating the
// stale-lease errors that an injected crash legitimately produces.
func (m *m10Fixture) drive(t *testing.T, id, status string) workflow.Instance {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		x := m.get(t, id)
		if x.Status == status {
			return x
		}
		began := time.Now()
		found, e := m.worker.Once(context.Background())
		if found && os.Getenv("M10_TIMING") != "" {
			t.Logf("step %s status=%s state=%s err=%v", time.Since(began).Round(time.Millisecond), x.Status, x.StateID, e)
		}
		if e != nil && !errors.Is(e, workflow.ErrLease) {
			t.Fatal("worker", e)
		}
		if !found || e != nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	x := m.get(t, id)
	t.Fatal("workflow status timeout", status, "last", x.Status, x.StateID, x.Failure, x.Tools)
	return x
}

type m10Ledger struct {
	State, Class string
	Attempts     int
	Mode         string
	Retryable    bool
}

func (m *m10Fixture) ledger(t *testing.T, id string) m10Ledger {
	t.Helper()
	var l m10Ledger
	if e := m.f.operator.QueryRow(`SELECT state,outcome_class,attempts,mode,retryable FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, id).Scan(&l.State, &l.Class, &l.Attempts, &l.Mode, &l.Retryable); e != nil {
		t.Fatal("ledger", e)
	}
	return l
}
func (m *m10Fixture) ledgerRows(t *testing.T, id string) int {
	t.Helper()
	var n int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, id).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

// startFlow starts a named flow and waits for its first stable state.
func (m *m10Fixture) startFlow(t *testing.T, name string) workflow.Instance {
	t.Helper()
	m.base = m.flow(name)
	return m.start(t, "start-"+name+"-"+uuid(t))
}

// approve waits for the approval gate and approves as a different actor.
func (m *m10Fixture) approve(t *testing.T, id string) workflow.Instance {
	t.Helper()
	x := m.drive(t, id, "WAITING_APPROVAL")
	return m.signal(t, x, "approve", m.approver)
}

// expireLease makes the job's lease immediately stale, as a worker crash would.
func (m *m10Fixture) expireLease(t *testing.T, id string) {
	t.Helper()
	if _, e := m.f.operator.Exec(`UPDATE control.workflow_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, id); e != nil {
		t.Fatal(e)
	}
}

// crashAfterFirstSend simulates a worker dying after the remote response but
// before the fenced outcome commit: the lease is lost and the result discarded.
func (m *m10Fixture) crashAfterFirstSend(id string, t *testing.T) {
	var once sync.Once
	m.exec.setAfter(func(_ tools.Request, _ tools.Result) {
		once.Do(func() { m.expireLease(t, id) })
	})
}

var _ = data.ErrNotFound
