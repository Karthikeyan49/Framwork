package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const okWrite = `{"id":"n-1","status":"created","total":7,"accepted":true,"balance":"10.25","ignored":"x"}`

var okWriteOutput = map[string]any{"id": "n-1", "status": "created", "total": int64(7), "accepted": true, "balance": "10.25"}

func (h *harness) pair(handler http.HandlerFunc) (prod, sandbox *testServer) {
	return h.server(handler), h.server(handler)
}

func TestLiveWriteSendsTypedGovernedRequest(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
	assertResult(t, got, Success, ClassOK, false, 201)
	if !reflect.DeepEqual(got.Output, okWriteOutput) {
		t.Fatalf("output %#v", got.Output)
	}
	if got.Simulated || sandbox.hits.Load() != 0 || prod.hits.Load() != 1 {
		t.Fatal("routing")
	}
	r := prod.last()
	if r.method != "POST" || r.uri != "/v1/customers/cust-42/notes" {
		t.Fatalf("request line %s %s", r.method, r.uri)
	}
	if r.host != "api.example.com:"+strconv.Itoa(prod.port) {
		t.Fatalf("host %q", r.host)
	}
	if r.header.Get("Idempotency-Key") != testKey {
		t.Fatalf("idempotency header %q", r.header.Get("Idempotency-Key"))
	}
	if r.header.Get("Authorization") != "Bearer "+liveToken || r.header.Get("Content-Type") != "application/json" || r.header.Get("Accept") != "application/json" {
		t.Fatalf("headers %v", r.header)
	}
	for _, name := range []string{"Accept-Encoding", "Cookie", "Proxy-Authorization", "X-Forwarded-For"} {
		if r.header.Get(name) != "" {
			t.Fatalf("unexpected header %s", name)
		}
	}
	if !reflect.DeepEqual(h.secrets.refs(), []string{"crm_live"}) {
		t.Fatalf("secret refs %v", h.secrets.refs())
	}
}

func TestBodyIsBuiltFromTypedBodyInputsOnly(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(200, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	inputs := writeInputs()
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, inputs), Success, ClassOK, false, 200)
	var body map[string]json.RawMessage
	if err := json.Unmarshal(prod.last().body, &body); err != nil {
		t.Fatalf("body %q: %v", prod.last().body, err)
	}
	if len(body) != 4 || string(body["count"]) != "3" || string(body["amount"]) != `"12.50"` || string(body["urgent"]) != "true" {
		t.Fatalf("typed body %s", prod.last().body)
	}
	var note string
	if json.Unmarshal(body["note"], &note) != nil || note != inputs["note"] {
		t.Fatalf("note %s", body["note"])
	}
	if _, leaked := body["customer_id"]; leaked {
		t.Fatal("path input was sent in the body")
	}
	// Optional inputs may be omitted or nil; both are absent from the body.
	delete(inputs, "urgent")
	inputs["note"] = nil
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, inputs), Success, ClassOK, false, 200)
	body = nil
	if err := json.Unmarshal(prod.last().body, &body); err != nil || len(body) != 2 {
		t.Fatalf("optional body %s", prod.last().body)
	}
	// Integer inputs accept the common Go and JSON number representations.
	for name, v := range map[string]any{"int": 3, "int32": int32(3), "int64": int64(3), "json.Number": json.Number("3")} {
		inputs := writeInputs()
		inputs["count"] = v
		assertResult(t, h.run(def, ModeLive, KindSend, testKey, inputs), Success, ClassOK, false, 200)
		if !bytes.Contains(prod.last().body, []byte(`"count":3`)) {
			t.Fatalf("%s body %s", name, prod.last().body)
		}
	}
}

func TestGetHasNoBodyAndFormatsTypedPathParameters(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(200, `{"balance":"-12.30","currency":"USD","ignored":null,"extra":1.5}`))
	def := readDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	got := h.run(def, ModeLive, KindSend, "", map[string]any{"account": "acct 1", "year": int64(-5), "active": true})
	assertResult(t, got, Success, ClassOK, false, 200)
	if !reflect.DeepEqual(got.Output, map[string]any{"balance": "-12.30", "currency": "USD"}) {
		t.Fatalf("output %#v", got.Output)
	}
	r := prod.last()
	if r.method != "GET" || r.uri != "/v2/accounts/acct%201/balance/-5/true" || len(r.body) != 0 {
		t.Fatalf("request %s %s body=%q", r.method, r.uri, r.body)
	}
	for _, name := range []string{"Content-Type", "Idempotency-Key", "Authorization", "Content-Length"} {
		if r.header.Get(name) != "" {
			t.Fatalf("GET carried %s", name)
		}
	}
	if r.header.Get("Accept") != "application/json" {
		t.Fatal("accept")
	}
	if len(h.secrets.refs()) != 0 {
		t.Fatal("secret resolved for an endpoint without secret_ref")
	}
}

func TestPathParametersAreEscapedPerSegmentAndTraversalRefused(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	escaped := map[string]string{
		"a b":      "a%20b",
		"é":        "%C3%A9",
		"a?b#c":    "a%3Fb%23c",
		"a%2Fb":    "a%252Fb",
		"a;b,c":    "a%3Bb%2Cc",
		"...":      "...",
		"..%2f":    "..%252f",
		"x&y=z":    "x&y=z",
		"a\tb\nc":  "a%09b%0Ac",
		"<script>": "%3Cscript%3E",
	}
	for value, want := range escaped {
		inputs := writeInputs()
		inputs["customer_id"] = value
		assertResult(t, h.run(def, ModeLive, KindSend, testKey, inputs), Success, ClassOK, false, 201)
		if got := prod.last().uri; got != "/v1/customers/"+want+"/notes" {
			t.Errorf("%q sent as %q", value, got)
		}
	}
	hits := prod.hits.Load()
	secretCalls := len(h.secrets.refs())
	for _, value := range []string{"", ".", "..", "a/b", "/", "../x", "x/..", `a\b`, `..\x`, "%2e%2e/x", "a/"} {
		inputs := writeInputs()
		inputs["customer_id"] = value
		assertResult(t, h.run(def, ModeLive, KindSend, testKey, inputs), Failed, ClassBlocked, false, 0)
	}
	if prod.hits.Load() != hits || len(h.secrets.refs()) != secretCalls {
		t.Fatal("rejected path value caused I/O")
	}
	// Lookup keys are escaped by the same builder and constrained to hex.
	out, ok := buildOutbound(lookupDefinition("pay.example.com", 443, "pay-sandbox.example.com", 443), Request{Kind: KindLookup, Key: testKey})
	if !ok || out.path != "/v1/invoices/by-reference/"+testKey || out.method != "GET" || out.body != nil {
		t.Fatalf("lookup outbound %+v", out)
	}
}

func TestInputsAndKeyAreValidatedBeforeAnyIO(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	with := func(name string, value any) map[string]any {
		in := writeInputs()
		if value == deleteInput {
			delete(in, name)
		} else {
			in[name] = value
		}
		return in
	}
	bad := map[string]map[string]any{
		"missing required path":    with("customer_id", deleteInput),
		"missing required body":    with("amount", deleteInput),
		"nil required":             with("count", nil),
		"undeclared input":         with("extra", "x"),
		"string for integer":       with("count", "3"),
		"float for integer":        with("count", 3.0),
		"fractional json number":   with("count", json.Number("3.5")),
		"integer above 2^53":       with("count", int64(9007199254740992)),
		"integer for string":       with("note", 5),
		"string for bool":          with("urgent", "true"),
		"number for decimal":       with("amount", 12.5),
		"integer for decimal":      with("amount", int64(12)),
		"exponent decimal":         with("amount", "1e3"),
		"trailing dot decimal":     with("amount", "1."),
		"leading zero decimal":     with("amount", "01.5"),
		"empty decimal":            with("amount", ""),
		"over max length":          with("note", strings.Repeat("a", 201)),
		"path over max length":     with("customer_id", strings.Repeat("a", 65)),
		"invalid utf8":             with("note", "a\xffb"),
		"nul byte":                 with("note", "a\x00b"),
		"map value":                with("note", map[string]any{"a": 1}),
		"slice value":              with("note", []string{"a"}),
		"byte slice":               with("note", []byte("a")),
		"float32":                  with("count", float32(3)),
		"uint":                     with("count", uint(3)),
		"pointer":                  with("note", new(string)),
		"invalid json number text": with("count", json.Number("abc")),
	}
	for name, inputs := range bad {
		if got := h.run(def, ModeLive, KindSend, testKey, inputs); got.Outcome != Failed || got.Class != ClassBlocked || got.Retryable {
			t.Errorf("%s accepted: %+v", name, got)
		}
	}
	for name, key := range map[string]string{
		"empty": "", "short": testKey[:63], "long": testKey + "0", "uppercase": strings.ToUpper(testKey),
		"non hex": strings.Repeat("g", 64), "whitespace": " " + testKey[1:], "newline": testKey[:63] + "\n",
	} {
		r := Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: key, Inputs: writeInputs(), Timeout: 1}
		if got := h.gw.Execute(t.Context(), r); got.Outcome != Failed || got.Class != ClassBlocked || got.Retryable {
			t.Errorf("key %s accepted: %+v", name, got)
		}
	}
	for name, r := range map[string]Request{
		"unknown kind":                     {Definition: def, Mode: ModeLive, Kind: "DELETE", Key: testKey, Inputs: writeInputs()},
		"empty kind":                       {Definition: def, Mode: ModeLive, Key: testKey, Inputs: writeInputs()},
		"nil inputs":                       {Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey},
		"zero request":                     {},
		"lookup without lookup definition": {Definition: def, Mode: ModeLive, Kind: KindLookup, Key: testKey},
	} {
		r.Timeout = 1
		if got := h.gw.Execute(t.Context(), r); got.Outcome != Failed || got.Class != ClassBlocked || got.Retryable {
			t.Errorf("%s: %+v", name, got)
		}
	}
	h.assertNoIO(prod, sandbox)
	// A tool without an idempotency header does not require a key but never
	// accepts a malformed one.
	read := readDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	prod.set(reply(200, `{"balance":"1.00"}`))
	in := map[string]any{"account": "a", "year": 1, "active": false}
	assertResult(t, h.run(read, ModeLive, KindSend, "", in), Success, ClassOK, false, 200)
	assertResult(t, h.run(read, ModeLive, KindSend, "not-a-key", in), Failed, ClassBlocked, false, 0)
}

var deleteInput = new(int)

func TestHandBuiltUnsafeDefinitionsAreRefused(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	mutate := map[string]func(*Definition){
		"ip literal host":      func(d *Definition) { d.Connector.Host = "127.0.0.1" },
		"timeout beyond cap":   func(d *Definition) { d.Connector.TimeoutMS = 60000 },
		"response beyond cap":  func(d *Definition) { d.Connector.MaxResponseBytes = 1 << 20 },
		"write via GET":        func(d *Definition) { d.Connector.Method = "GET" },
		"unknown side effect":  func(d *Definition) { d.SideEffect = "FINANCIAL" },
		"traversal path":       func(d *Definition) { d.Connector.Path = "/v1/../{customer_id}" },
		"reserved header":      func(d *Definition) { d.Connector.IdempotencyHeader = "Authorization" },
		"sandbox same secret":  func(d *Definition) { d.Sandbox.Endpoint.SecretRef = d.Connector.SecretRef },
		"bad secret reference": func(d *Definition) { d.Connector.SecretRef = "../etc/passwd" },
	}
	for name, change := range mutate {
		def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
		change(&def)
		for _, mode := range []string{ModeLive, ModeReroute} {
			if got := h.run(def, mode, KindSend, testKey, writeInputs()); got.Outcome != Failed || got.Class != ClassBlocked {
				t.Errorf("%s/%s: %+v", name, mode, got)
			}
		}
	}
	h.assertNoIO(prod, sandbox)
}

func TestSecretIsInjectedOnlyAsBearerAndNeverReturned(t *testing.T) {
	h := newHarness(t)
	// The provider reflects its credentials in an error body; nothing from a
	// body, header or resolver error may reach the Result.
	echo := func(status int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Echo", r.Header.Get("Authorization"))
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":"bad token %s","id":"%s"}`, r.Header.Get("Authorization"), liveToken)
		}
	}
	prod, sandbox := h.pair(echo(400))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
	assertResult(t, got, Failed, ClassRejected, false, 400)
	if contains(fmt.Sprintf("%#v %+v", got, got), liveToken, "Bearer") {
		t.Fatalf("secret leaked into result: %+v", got)
	}
	r := prod.last()
	if r.header.Get("Authorization") != "Bearer "+liveToken || contains(string(r.body)+r.uri, liveToken) {
		t.Fatal("credential must travel only in Authorization")
	}
	for name, values := range r.header {
		if name != "Authorization" && contains(strings.Join(values, ","), liveToken) {
			t.Fatalf("credential in header %s", name)
		}
	}
	// Unresolvable, failing and panicking resolvers: retryable, no I/O, no leak.
	// Each scenario starts from a healthy resolver, so order does not matter.
	healthy := func() {
		h.secrets.mu.Lock()
		h.secrets.fail, h.secrets.panics = false, false
		h.secrets.values["crm_live"] = liveToken
		h.secrets.mu.Unlock()
	}
	scenarios := map[string]func(){
		"resolver error": func() { h.secrets.fail = true },
		"unknown ref":    func() { delete(h.secrets.values, "crm_live") },
		"resolver panic": func() { h.secrets.panics = true },
		"header injection": func() {
			h.secrets.values["crm_live"] = "line1\r\nX-Injected: 1"
		},
		"empty value":      func() { h.secrets.values["crm_live"] = "" },
		"whitespace value": func() { h.secrets.values["crm_live"] = "two words" },
	}
	for name, change := range scenarios {
		healthy()
		change()
		before := prod.hits.Load()
		got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
		assertResult(t, got, Failed, ClassNotSent, true, 0)
		if prod.hits.Load() != before || contains(fmt.Sprintf("%#v", got), liveToken, "line1") {
			t.Fatalf("%s: io or leak", name)
		}
	}
	healthy()
	// No resolver configured: a secret-bearing endpoint cannot be used.
	h.rebuild(func(o *GatewayOptions) { o.Secrets = nil })
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)
}

func TestFileSecretsFeedTheBearerHeader(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	writeSecret(t, dir, "crm_live", "file-token-123\n", 0o600)
	h.rebuild(func(o *GatewayOptions) { o.Secrets = FileSecrets{Dir: dir} })
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Success, ClassOK, false, 201)
	if prod.last().header.Get("Authorization") != "Bearer file-token-123" {
		t.Fatal("file secret")
	}
	if runtime.GOOS == "windows" {
		return // no owner-only mode bits to enforce
	}
	writeSecret(t, dir, "crm_live", "file-token-123\n", 0o644)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)
}

func TestSandboxModesNeverResolveOrUseProductionSecretOrEndpoint(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	write := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	assertResult(t, h.run(write, ModeReroute, KindSend, testKey, writeInputs()), Success, ClassOK, false, 201)
	r := sandbox.last()
	if r.host != "sandbox.example.com:"+strconv.Itoa(sandbox.port) || r.header.Get("Authorization") != "Bearer "+sandboxToken || r.header.Get("Idempotency-Key") != testKey {
		t.Fatalf("reroute request %+v", r)
	}
	if prod.hits.Load() != 0 || prod.conns.Load() != 0 || !reflect.DeepEqual(h.secrets.refs(), []string{"crm_sandbox"}) {
		t.Fatalf("production touched: conns=%d refs=%v", prod.conns.Load(), h.secrets.refs())
	}

	// TEST_ACCOUNT uses the mandatory sandbox credential of the sandbox endpoint.
	psrv, ssrv := h.pair(reply(200, `{"invoice_id":"inv-9","state":"open"}`))
	pay := lookupDefinition("pay.example.com", psrv.port, "pay-sandbox.example.com", ssrv.port)
	got := h.run(pay, ModeTestAccount, KindSend, testKey, map[string]any{"amount": "5.00"})
	assertResult(t, got, Success, ClassOK, false, 200)
	if ssrv.last().header.Get("Authorization") != "Bearer "+paySandbox || ssrv.last().method != "PUT" || ssrv.last().header.Get("X-Reference-Key") != testKey {
		t.Fatalf("test account request %+v", ssrv.last())
	}
	got = h.run(pay, ModeTestAccount, KindLookup, testKey, nil)
	assertResult(t, got, Success, ClassOK, false, 200)
	if ssrv.last().uri != "/v1/invoices/by-reference/"+testKey || psrv.hits.Load() != 0 || psrv.conns.Load() != 0 {
		t.Fatal("test account lookup routing")
	}

	// READ_ONLY reaches the credential-free sandbox of a read tool.
	rsrv, rsbx := h.pair(reply(200, `{"balance":"1.00"}`))
	read := readDefinition("api.example.com", rsrv.port, "sandbox.example.com", rsbx.port)
	before := len(h.secrets.refs())
	assertResult(t, h.run(read, ModeReadOnly, KindSend, "", map[string]any{"account": "a", "year": 2026, "active": true}), Success, ClassOK, false, 200)
	if rsrv.hits.Load() != 0 || rsbx.hits.Load() != 1 || len(h.secrets.refs()) != before || rsbx.last().header.Get("Authorization") != "" {
		t.Fatal("read only routing")
	}

	for _, ref := range h.secrets.refs() {
		if ref == "crm_live" || ref == "pay_live" {
			t.Fatalf("production secret %s resolved in a sandbox mode", ref)
		}
	}
}

func TestSandboxModeEdgeCasesAreBlocked(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	write := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	read := readDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	readIn := map[string]any{"account": "a", "year": 1, "active": true}

	// READ_ONLY never sends a WRITE definition, even with a usable sandbox.
	assertResult(t, h.run(write, ModeReadOnly, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	// TEST_ACCOUNT requires a sandbox credential.
	assertResult(t, h.run(read, ModeTestAccount, KindSend, "", readIn), Failed, ClassBlocked, false, 0)
	// Sandbox modes need a sandbox endpoint; BLOCK and SIMULATE tools have none.
	blocked := write
	blocked.Sandbox = SandboxSpec{Mode: "BLOCK"}
	for _, mode := range []string{ModeReroute, ModeTestAccount, ModeReadOnly} {
		assertResult(t, h.run(blocked, mode, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	}
	// BLOCK performs no I/O, whatever the definition.
	assertResult(t, h.run(write, ModeBlock, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	assertResult(t, h.run(blocked, ModeBlock, KindLookup, testKey, nil), Failed, ClassBlocked, false, 0)
	// Unknown, empty and differently cased modes fail closed.
	for _, mode := range []string{"", "live", "PRODUCTION", "LIVE ", "SANDBOX", "DRY_RUN"} {
		assertResult(t, h.run(write, mode, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	}
	h.assertNoIO(prod, sandbox)
}

func TestSimulateReturnsFixtureWithoutAnyIO(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(200, `{"region":"LIVE"}`))
	def := simulateDefinition("geo.example.com", prod.port)
	got := h.run(def, ModeSimulate, KindSend, "", map[string]any{"code": "NO"})
	assertResult(t, got, Success, ClassOK, false, 0)
	if !got.Simulated || !reflect.DeepEqual(got.Output, map[string]any{"region": "north", "population": int64(42)}) {
		t.Fatalf("simulated result %+v", got)
	}
	// Mutating the returned map must not alter the definition's fixture.
	got.Output["region"] = "tampered"
	again := h.run(def, ModeSimulate, KindSend, "", map[string]any{"code": "NO"})
	if again.Output["region"] != "north" {
		t.Fatal("fixture aliased")
	}
	// A lookup of a tool with a reference-lookup capability is simulated too.
	lookup := lookupDefinition("pay.example.com", prod.port, "pay-sandbox.example.com", sandbox.port)
	lookup.Sandbox = SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{"invoice_id": json.RawMessage(`"inv-sim"`)}}
	got = h.run(lookup, ModeSimulate, KindLookup, testKey, nil)
	assertResult(t, got, Success, ClassOK, false, 0)
	if !got.Simulated || got.Output["invoice_id"] != "inv-sim" {
		t.Fatalf("simulated lookup %+v", got)
	}
	got = h.run(lookup, ModeSimulate, KindSend, testKey, map[string]any{"amount": "1.00"})
	if !got.Simulated || got.Outcome != Success {
		t.Fatalf("simulated send %+v", got)
	}
	// Invalid input is rejected even though nothing would be sent; so is a
	// definition whose sandbox is not a fixture.
	assertResult(t, h.run(def, ModeSimulate, KindSend, "", map[string]any{"code": strings.Repeat("x", 9)}), Failed, ClassBlocked, false, 0)
	write := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	assertResult(t, h.run(write, ModeSimulate, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	h.assertNoIO(prod, sandbox)
}

func TestMissingFixtureFieldsAreSimplyAbsent(t *testing.T) {
	def := simulateDefinition("geo.example.com", 443)
	got, err := DecodeOutput(def.Output, def.Sandbox.Output)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["coastal"]; present || len(got) != 2 {
		t.Fatalf("fixture output %#v", got)
	}
}

func TestLookupFoundAndNotFound(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/invoices/by-reference/"+testKey {
			w.WriteHeader(200)
			fmt.Fprint(w, `{"invoice_id":"inv-1","state":"paid"}`)
			return
		}
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":"not found"}`)
	})
	def := lookupDefinition("pay.example.com", prod.port, "pay-sandbox.example.com", sandbox.port)
	got := h.run(def, ModeLive, KindLookup, testKey, map[string]any{"amount": "9.99"})
	assertResult(t, got, Success, ClassOK, false, 200)
	if !reflect.DeepEqual(got.Output, map[string]any{"invoice_id": "inv-1", "state": "paid"}) {
		t.Fatalf("lookup output %#v", got.Output)
	}
	r := prod.last()
	if r.method != "GET" || r.uri != "/v1/invoices/by-reference/"+testKey || len(r.body) != 0 || r.header.Get("Authorization") != "Bearer "+payToken {
		t.Fatalf("lookup request %+v", r)
	}
	if r.header.Get("X-Reference-Key") != "" || r.header.Get("Content-Type") != "" {
		t.Fatal("lookup must not carry send headers")
	}
	other := strings.Repeat("a", 64)
	got = h.run(def, ModeLive, KindLookup, other, nil)
	assertResult(t, got, Failed, ClassNotFound, false, 404)
	// 404 means "provably absent" only for lookups.
	assertResult(t, h.run(def, ModeLive, KindSend, other, map[string]any{"amount": "1.00"}), Failed, ClassRejected, false, 404)
	// A malformed key never reaches the provider.
	hits := prod.hits.Load()
	for _, key := range []string{"", "../x", strings.ToUpper(testKey), testKey + "0"} {
		assertResult(t, h.run(def, ModeLive, KindLookup, key, nil), Failed, ClassBlocked, false, 0)
	}
	if prod.hits.Load() != hits {
		t.Fatal("malformed key reached provider")
	}
}
