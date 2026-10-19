package tools

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Regression tests for the independent gateway review (findings F1-F8; F9 is a
// documentation alignment, see ADR036 decision 6 and the README outcome table) and
// for M11-R07. Each one fails on the code before its fix.

// F1: encoding/json folds U+017F ("long s", already lower case) onto "s", so a second
// key "hoſt" overrode "host" while every other JSON tool saw an unrelated key.
func TestLoadCatalogRejectsUnicodeFoldedKeys(t *testing.T) {
	raw := string(catalogJSON(t, []Definition{simulateDefinition("geo.example.com", 443)}))
	for name, smuggled := range map[string]string{
		"host":       `"host":"geo.example.com"`,
		"tenants":    `"tenants":["*"]`,
		"secret_ref": `"secret_ref":"geo_live"`,
	} {
		folded := strings.Replace(smuggled, "s", "ſ", 1)
		if folded == smuggled || !strings.Contains(raw, smuggled) {
			t.Fatalf("%s: fixture does not contain %s", name, smuggled)
		}
		mutated := strings.Replace(raw, smuggled, smuggled+","+folded, 1)
		if _, err := LoadCatalog([]byte(mutated)); !errors.Is(err, ErrCatalog) {
			t.Errorf("%s: a Unicode-folded alias key was accepted (%v)", name, err)
		}
	}
	if _, err := LoadCatalog([]byte(raw)); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// F2: another port of the production host is still the production system.
func TestSandboxEndpointMayNotBeTheProductionHostOnAnotherPort(t *testing.T) {
	d := readDefinition("api.example.com", 443, "api.example.com", 8443)
	if _, err := NewCatalog([]Definition{d}); !errors.Is(err, ErrCatalog) {
		t.Fatalf("sandbox on the production host accepted: %v", err)
	}
	if _, ok := effectiveEndpoint(d, ModeReadOnly); ok {
		t.Fatal("the gateway would send a sandbox request to the production host")
	}
}

// F3: separation holds across the whole catalog, not only inside one definition.
func TestSandboxMayNotUseAnotherToolsProductionHostOrCredential(t *testing.T) {
	crm := writeDefinition("api.crm.example.com", 443, "sandbox.crm.example.com", 443)
	pay := lookupDefinition("pay.example.com", 443, "pay-sandbox.example.com", 443)
	if _, err := NewCatalog([]Definition{crm, pay}); err != nil {
		t.Fatalf("control: %v", err)
	}
	stealCredential := pay
	sb := *pay.Sandbox.Endpoint
	sb.SecretRef = crm.Connector.SecretRef
	stealCredential.Sandbox.Endpoint = &sb
	if _, err := NewCatalog([]Definition{crm, stealCredential}); !errors.Is(err, ErrCatalog) {
		t.Fatalf("a sandbox that uses another tool's production credential was accepted: %v", err)
	}
	reachProduction := pay
	sb2 := *pay.Sandbox.Endpoint
	sb2.Host = crm.Connector.Host
	reachProduction.Sandbox.Endpoint = &sb2
	if _, err := NewCatalog([]Definition{crm, reachProduction}); !errors.Is(err, ErrCatalog) {
		t.Fatalf("a sandbox that points at another tool's production host was accepted: %v", err)
	}
}

// F4: a ceiling that narrows TEST_ACCOUNT to REROUTE or READ_ONLY must withhold the
// test-account credential; REROUTE/READ_ONLY declarations may not carry one.
func TestOnlyTestAccountSendsTheSandboxCredential(t *testing.T) {
	h := newHarness(t)
	psrv, ssrv := h.pair(reply(200, `{"invoice_id":"inv-9","state":"open"}`))
	pay := lookupDefinition("pay.example.com", psrv.port, "pay-sandbox.example.com", ssrv.port)
	if mode := EffectiveMode(pay, ModeReroute); mode != ModeReroute {
		t.Fatalf("precondition: %s", mode)
	}
	got := h.run(pay, ModeReroute, KindSend, testKey, map[string]any{"amount": "5.00"})
	assertResult(t, got, Success, ClassOK, false, 200)
	if auth := ssrv.last().header.Get("Authorization"); auth != "" {
		t.Fatalf("a REROUTE request carried the test-account credential: %q", auth)
	}
	if len(h.secrets.refs()) != 0 {
		t.Fatalf("a credential was resolved for REROUTE: %v", h.secrets.refs())
	}
	got = h.run(pay, ModeTestAccount, KindSend, testKey, map[string]any{"amount": "5.00"})
	assertResult(t, got, Success, ClassOK, false, 200)
	if ssrv.last().header.Get("Authorization") != "Bearer "+paySandbox {
		t.Fatalf("TEST_ACCOUNT must still authenticate: %q", ssrv.last().header.Get("Authorization"))
	}
	for _, mode := range []string{"REROUTE", "READ_ONLY"} {
		d := readDefinition("api.example.com", 443, "ro.example.com", 443)
		d.Sandbox.Mode = mode
		sb := *d.Sandbox.Endpoint
		sb.SecretRef = "ro_secret"
		d.Sandbox.Endpoint = &sb
		if _, err := NewCatalog([]Definition{d}); !errors.Is(err, ErrCatalog) {
			t.Errorf("%s sandbox with a credential accepted: %v", mode, err)
		}
	}
}

// M11-R07 / F4 for a ceiling below REROUTE: a READ tool declared TEST_ACCOUNT that a
// READ_ONLY (or REROUTE) ceiling narrows must reach the sandbox endpoint without any
// credential, and the credential must never even be resolved.
func TestNarrowedTestAccountToolNeverResolvesItsSandboxCredential(t *testing.T) {
	for _, ceiling := range []string{ModeReadOnly, ModeReroute} {
		t.Run(ceiling, func(t *testing.T) {
			h := newHarness(t)
			psrv, ssrv := h.pair(reply(200, `{"balance":"10.25","currency":"USD"}`))
			d := readDefinition("api.example.com", psrv.port, "sandbox.example.com", ssrv.port)
			d.Sandbox.Mode = ModeTestAccount
			sb := *d.Sandbox.Endpoint
			sb.SecretRef = "crm_sandbox"
			d.Sandbox.Endpoint = &sb
			if _, err := NewCatalog([]Definition{d}); err != nil {
				t.Fatalf("precondition: %v", err)
			}
			mode := EffectiveMode(d, ceiling)
			if mode != ceiling {
				t.Fatalf("precondition: %s narrows to %s", ceiling, mode)
			}
			inputs := map[string]any{"account": "acc-1", "year": int64(2026), "active": true}
			got := h.run(d, mode, KindSend, testKey, inputs)
			assertResult(t, got, Success, ClassOK, false, 200)
			if auth := ssrv.last().header.Get("Authorization"); auth != "" {
				t.Fatalf("a %s request carried a credential: %q", ceiling, auth)
			}
			if len(h.secrets.refs()) != 0 {
				t.Fatalf("a credential was resolved under a %s ceiling: %v", ceiling, h.secrets.refs())
			}
			got = h.run(d, ModeTestAccount, KindSend, testKey, inputs)
			assertResult(t, got, Success, ClassOK, false, 200)
			if ssrv.last().header.Get("Authorization") != "Bearer "+sandboxToken {
				t.Fatalf("TEST_ACCOUNT must still authenticate: %q", ssrv.last().header.Get("Authorization"))
			}
		})
	}
}

// F5: the client never asks for compression and sends Accept: application/json, so a
// 2xx that carries a content coding or a media type other than JSON is not the typed
// answer that was requested, even when its body happens to parse.
func TestSuccessResponseNeedsIdentityEncodingAndJSONMediaType(t *testing.T) {
	cases := []struct {
		name   string
		set    func(http.Header)
		accept bool
	}{
		{"application/json", func(h http.Header) { h.Set("Content-Type", "application/json") }, true},
		{"charset utf-8 in any case", func(h http.Header) { h.Set("Content-Type", "Application/JSON; Charset=UTF-8") }, true},
		{"explicit identity coding", func(h http.Header) { h.Set("Content-Type", "application/json"); h.Set("Content-Encoding", "identity") }, true},
		{"gzip label on a plain body", func(h http.Header) { h.Set("Content-Type", "application/json"); h.Set("Content-Encoding", "gzip") }, false},
		{"br label", func(h http.Header) { h.Set("Content-Type", "application/json"); h.Set("Content-Encoding", "br") }, false},
		{"deflate label", func(h http.Header) { h.Set("Content-Type", "application/json"); h.Set("Content-Encoding", "deflate") }, false},
		{"stacked codings", func(h http.Header) {
			h.Set("Content-Type", "application/json")
			h.Set("Content-Encoding", "identity, gzip")
		}, false},
		{"text/html", func(h http.Header) { h.Set("Content-Type", "text/html") }, false},
		{"application/xml", func(h http.Header) { h.Set("Content-Type", "application/xml") }, false},
		{"text/plain", func(h http.Header) { h.Set("Content-Type", "text/plain; charset=utf-8") }, false},
		{"json in another charset", func(h http.Header) { h.Set("Content-Type", "application/json; charset=iso-8859-1") }, false},
		{"structured suffix", func(h http.Header) { h.Set("Content-Type", "application/problem+json") }, false},
		{"unparseable media type", func(h http.Header) { h.Set("Content-Type", "application/json; charset") }, false},
		{"no content type (server sniffs text/plain)", func(http.Header) {}, false},
		{"content type suppressed", func(h http.Header) { h["Content-Type"] = nil }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			def, prod, _ := h.writeFixture(func(w http.ResponseWriter, _ *http.Request) {
				c.set(w.Header())
				w.WriteHeader(200)
				_, _ = io.WriteString(w, okWrite)
			})
			got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
			if c.accept {
				assertResult(t, got, Success, ClassOK, false, 200)
			} else {
				assertResult(t, got, Unknown, ClassInvalidResponse, false, 200)
			}
			if prod.hits.Load() != 1 {
				t.Fatalf("request delivered %d times", prod.hits.Load())
			}
		})
	}
}

// F6: the pinned definition hash is canonical over the decoded fixture. Re-spelling a
// value (an escape the operator's formatter rewrites) must not re-pin the tool, while a
// changed value must.
func TestDefinitionHashCoversTheDecodedFixtureNotItsSpelling(t *testing.T) {
	spell := func(region, population string) Definition {
		d := simulateDefinition("geo.example.com", 443)
		d.Sandbox.Output = map[string]json.RawMessage{"region": json.RawMessage(region), "population": json.RawMessage(population)}
		return d
	}
	plain := spell(`"north"`, `42`)
	for name, other := range map[string]Definition{
		"escaped letter":        spell(`"nor\u0074h"`, `42`),
		"upper-case hex escape": spell(`"\u006Eorth"`, `42`),
		"escaped letters mixed": spell(`"n\u006fr\u0074h"`, `42`),
		"padded whitespace":     spell(` "north" `, ` 42 `),
	} {
		if other.Hash() != plain.Hash() {
			t.Errorf("%s: the same fixture hashes differently", name)
		}
	}
	if spell(`"south"`, `42`).Hash() == plain.Hash() || spell(`"north"`, `43`).Hash() == plain.Hash() {
		t.Error("a changed fixture value must change the hash")
	}
	// An optional member written as null decodes to "absent": same fixture, same hash.
	nullPop, absentPop := spell(`"north"`, `null`), plain
	absentPop.Sandbox.Output = map[string]json.RawMessage{"region": json.RawMessage(`"north"`)}
	if nullPop.Hash() != absentPop.Hash() {
		t.Error("null and absent optional fixture members must hash alike")
	}
	// Through LoadCatalog: an operator file with a re-spelled escape pins identically.
	raw := string(catalogJSON(t, []Definition{plain}))
	if !strings.Contains(raw, `"region":"north"`) {
		t.Fatalf("fixture does not contain the spelled value: %s", raw)
	}
	a, err := LoadCatalog([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadCatalog([]byte(strings.Replace(raw, `"region":"north"`, `"region":"nor\u0074h"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash() != b.Hash() {
		t.Error("a re-spelled escape changed the catalog hash")
	}
}

// F7: a fixture or input value is exactly one JSON scalar. Bytes after it (a stray
// closing bracket, a second value) used to pass because Decoder.More is false before
// ']' and '}', which let two different hand-built definitions hash alike.
func TestScalarsRejectTrailingBytesAndHashNeverCollapses(t *testing.T) {
	for _, raw := range []string{`"north"]`, `"north"}`, `"north" "south"`, `"north"x`, `"north",1`, `"north"` + "\x00"} {
		d := simulateDefinition("geo.example.com", 443)
		d.Sandbox.Output = map[string]json.RawMessage{"region": json.RawMessage(raw)}
		if _, err := NewCatalog([]Definition{d}); !errors.Is(err, ErrCatalog) {
			t.Errorf("fixture %q accepted: %v", raw, err)
		}
		if _, err := DecodeInput(Field{Name: "code", Type: "string", MaxLength: 8}, json.RawMessage(raw)); err == nil {
			t.Errorf("input %q accepted", raw)
		}
	}
	if _, err := DecodeInput(Field{Name: "code", Type: "string", MaxLength: 8}, json.RawMessage(" \"north\" \n")); err != nil {
		t.Errorf("surrounding whitespace is legal: %v", err)
	}
	// Hand-built definitions that cannot even be marshalled must not share the digest
	// of the empty string or any valid hash: no pin taken from them can ever match.
	a, b := simulateDefinition("geo.example.com", 443), simulateDefinition("geo.example.com", 443)
	a.Sandbox.Output = map[string]json.RawMessage{"region": json.RawMessage(`"north"]`)}
	b.Sandbox.Output = map[string]json.RawMessage{"region": json.RawMessage(`"south"}`)}
	valid := simulateDefinition("geo.example.com", 443)
	const emptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	hexDigest := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for name, d := range map[string]Definition{"north]": a, "south}": b} {
		if h := d.Hash(); h == emptyDigest || h == valid.Hash() || hexDigest.MatchString(h) {
			t.Errorf("%s: unmarshalable definition hashes to %q", name, h)
		}
	}
}
