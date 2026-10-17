package tools

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// Regression tests for the independent gateway review (findings F1-F4). Each one
// fails on the code before the fix.

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
	_ = strconv.Itoa
}
