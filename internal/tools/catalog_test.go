package tools

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
)

func validDefinitions() []Definition {
	return []Definition{
		writeDefinition("api.crm.example.com", 443, "sandbox.crm.example.com", 443),
		readDefinition("api.crm.example.com", 443, "ro.crm.example.com", 8443),
		lookupDefinition("pay.example.com", 443, "pay-sandbox.example.com", 9443),
		simulateDefinition("geo.example.com", 443),
	}
}

func catalogJSON(t *testing.T, defs []Definition) []byte {
	t.Helper()
	raw, err := json.Marshal(catalogDocument{SchemaVersion: CatalogVersion, Tools: defs})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLoadCatalogAcceptsValidDefinitions(t *testing.T) {
	defs := validDefinitions()
	c, err := LoadCatalog(catalogJSON(t, defs))
	if err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}
	if c.Len() != len(defs) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(c.Hash()) {
		t.Fatalf("len=%d hash=%q", c.Len(), c.Hash())
	}
	for _, d := range defs {
		got, ok := c.Lookup(d.Ref, d.Version, "")
		if !ok {
			t.Fatalf("lookup %s", d.Ref)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got.Hash()) {
			t.Fatal("definition hash")
		}
		if _, ok := c.Lookup(d.Ref, d.Version, got.Hash()); !ok {
			t.Fatalf("pinned lookup %s", d.Ref)
		}
		if _, ok := c.Lookup(d.Ref, d.Version, strings.Repeat("0", 64)); ok {
			t.Fatalf("pin mismatch accepted for %s", d.Ref)
		}
		if _, ok := c.Lookup(d.Ref, "9.9.9", ""); ok {
			t.Fatal("unknown version resolved")
		}
	}
	// Tenants are normalized (sorted) and the allowlist is exact.
	pay, _ := c.Lookup("tool.payments.create_invoice", "1.2.3", "")
	if pay.Tenants[0] != testTenant || !pay.AllowsTenant(testTenant) || pay.AllowsTenant("99999999-2222-4333-8444-555555555555") {
		t.Fatalf("tenants %v", pay.Tenants)
	}
	if write, _ := c.Lookup("tool.crm.create_note", "1.0.0", ""); !write.AllowsTenant(testTenant) {
		t.Fatal("wildcard tenant")
	}
	if _, ok := EmptyCatalog().Lookup("tool.crm.create_note", "1.0.0", ""); ok || EmptyCatalog().Len() != 0 {
		t.Fatal("empty catalog")
	}
	empty, err := LoadCatalog([]byte(`{"schema_version":"tool-catalog-v1","tools":[]}`))
	if err != nil || empty.Len() != 0 || empty.Hash() != EmptyCatalog().Hash() {
		t.Fatal("empty tools document")
	}
}

func TestCatalogHashStableAndOrderIndependent(t *testing.T) {
	defs := validDefinitions()
	a, err := NewCatalog(defs)
	if err != nil {
		t.Fatal(err)
	}
	reversed := make([]Definition, 0, len(defs))
	for i := len(defs) - 1; i >= 0; i-- {
		d := defs[i]
		d.Tenants = append([]string(nil), d.Tenants...)
		for l, r := 0, len(d.Tenants)-1; l < r; l, r = l+1, r-1 {
			d.Tenants[l], d.Tenants[r] = d.Tenants[r], d.Tenants[l]
		}
		reversed = append(reversed, d)
	}
	b, err := NewCatalog(reversed)
	if err != nil || a.Hash() != b.Hash() {
		t.Fatal("catalog hash depends on tool or tenant order")
	}
	// Formatting of the operator document does not change identity.
	indented, _ := json.MarshalIndent(catalogDocument{SchemaVersion: CatalogVersion, Tools: defs}, "", "   ")
	c, err := LoadCatalog(indented)
	if err != nil || c.Hash() != a.Hash() {
		t.Fatal("whitespace changed hash")
	}
	for _, d := range defs {
		x, _ := a.Lookup(d.Ref, d.Version, "")
		y, _ := c.Lookup(d.Ref, d.Version, "")
		if x.Hash() != y.Hash() {
			t.Fatal("definition hash unstable")
		}
	}
	// Golden value: the canonical definition hash is pinned by compiled plans,
	// so an encoding change must be a deliberate, reviewed decision.
	golden, _ := a.Lookup("tool.crm.create_note", "1.0.0", "")
	if got := golden.Hash(); got != goldenWriteHash {
		t.Fatalf("definition hash changed: %s", got)
	}
	// A changed definition never satisfies an existing pin.
	changed := validDefinitions()
	changed[0].Connector.TimeoutMS = 1999
	d, err := NewCatalog(changed)
	if err != nil || d.Hash() == a.Hash() {
		t.Fatal("changed definition kept catalog hash")
	}
	if _, ok := d.Lookup(golden.Ref, golden.Version, golden.Hash()); ok {
		t.Fatal("changed definition satisfied old pin")
	}
}

const goldenWriteHash = "d6c1930790f0862bc494f8fe0b296373a3bdd3cd4869f18f74399f7866ac4ac5"

func TestLoadCatalogRejectsMalformedDocuments(t *testing.T) {
	valid := string(catalogJSON(t, validDefinitions()))
	if _, err := LoadCatalog([]byte(valid)); err != nil {
		t.Fatal("control document rejected")
	}
	if _, err := LoadCatalog([]byte(valid + " \n\t")); err != nil {
		t.Fatal("trailing whitespace rejected")
	}
	cases := map[string]string{
		"empty":                "",
		"not object":           `[]`,
		"duplicate top key":    `{"schema_version":"tool-catalog-v1","schema_version":"tool-catalog-v1","tools":[]}`,
		"duplicate nested key": strings.Replace(valid, `"side_effect":"WRITE"`, `"side_effect":"WRITE","side_effect":"WRITE"`, 1),
		"uppercase key":        strings.Replace(valid, `"schema_version"`, `"Schema_version"`, 1),
		"uppercase nested key": strings.Replace(valid, `"timeout_ms"`, `"Timeout_ms"`, 1),
		"unknown field":        strings.Replace(valid, `"side_effect":"WRITE"`, `"side_effect":"WRITE","extra":true`, 1),
		"unknown top field":    strings.Replace(valid, `"tools"`, `"extra":1,"tools"`, 1),
		"trailing document":    valid + valid,
		"trailing object":      valid + `{}`,
		"trailing bracket":     valid + `]`,
		"trailing brace":       valid + `}`,
		"trailing garbage":     valid + `x`,
		"wrong schema":         strings.Replace(valid, CatalogVersion, "tool-catalog-v0", 1),
		"missing tools":        `{"schema_version":"tool-catalog-v1"}`,
		"null tools":           `{"schema_version":"tool-catalog-v1","tools":null}`,
		"tools not array":      `{"schema_version":"tool-catalog-v1","tools":{}}`,
		"too deep":             `{"schema_version":"tool-catalog-v1","tools":[],"x":` + strings.Repeat("[", 20) + strings.Repeat("]", 20) + `}`,
		"too large":            `{"schema_version":"tool-catalog-v1","tools":[]` + strings.Repeat(" ", MaxCatalogBytes) + `}`,
	}
	for name, raw := range cases {
		if _, err := LoadCatalog([]byte(raw)); !errors.Is(err, ErrCatalog) {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestNewCatalogRejectsUnsafeDefinitions(t *testing.T) {
	base := func() Definition { return writeDefinition("api.crm.example.com", 443, "sandbox.crm.example.com", 443) }
	read := func() Definition { return readDefinition("api.crm.example.com", 443, "ro.crm.example.com", 443) }
	lookup := func() Definition { return lookupDefinition("pay.example.com", 443, "pay-sandbox.example.com", 443) }
	sim := func() Definition { return simulateDefinition("geo.example.com", 443) }
	// Controls: each mutation below starts from a valid definition.
	for _, d := range []Definition{base(), read(), lookup(), sim()} {
		if _, err := NewCatalog([]Definition{d}); err != nil {
			t.Fatalf("control %s rejected", d.Ref)
		}
	}
	// writeViaGET differs from a valid write only by its method.
	writeVia := func(method string) Definition {
		d := read()
		d.SideEffect, d.Approval = "WRITE", "REQUIRED"
		d.Delivery = DeliverySpec{IdempotencyMode: "NONE", RetryMode: "RECONCILE_FIRST", ReconciliationMode: "MANUAL"}
		d.Sandbox.Mode = "REROUTE"
		d.Connector.Method = method
		return d
	}
	if _, err := NewCatalog([]Definition{writeVia("POST")}); err != nil {
		t.Fatal("write-via-POST control rejected")
	}
	cases := map[string]func() Definition{
		"bad ref":                   func() Definition { d := base(); d.Ref = "crm.create_note"; return d },
		"uppercase ref":             func() Definition { d := base(); d.Ref = "tool.CRM.create"; return d },
		"bad version":               func() Definition { d := base(); d.Version = "01.0.0"; return d },
		"short version":             func() Definition { d := base(); d.Version = "1.0"; return d },
		"financial":                 func() Definition { d := base(); d.SideEffect = "FINANCIAL"; return d },
		"destructive":               func() Definition { d := base(); d.SideEffect = "DESTRUCTIVE"; return d },
		"unknown effect":            func() Definition { d := base(); d.SideEffect = "MAYBE"; return d },
		"write via GET":             func() Definition { return writeVia("GET") },
		"read via POST":             func() Definition { d := read(); d.Connector.Method = "POST"; return d },
		"method DELETE":             func() Definition { d := base(); d.Connector.Method = "DELETE"; return d },
		"write without approval":    func() Definition { d := base(); d.Approval = "NONE"; return d },
		"unknown approval":          func() Definition { d := base(); d.Approval = "MAYBE"; return d },
		"invalid delivery":          func() Definition { d := base(); d.Delivery.RetryMode = "ALWAYS"; return d },
		"unsafe write retry":        func() Definition { d := writeVia("POST"); d.Delivery.RetryMode = "SAFE_RETRY"; return d },
		"write without reconcile":   func() Definition { d := base(); d.Delivery.ReconciliationMode = "NONE"; return d },
		"native key without window": func() Definition { d := base(); d.Delivery.ReplayWindowMS = 0; return d },
		"native key without header": func() Definition { d := base(); d.Connector.IdempotencyHeader = ""; return d },
		"dedupe without window": func() Definition {
			d := base()
			d.Delivery.IdempotencyMode, d.Delivery.ReplayWindowMS = "DEDUPE_WINDOW", 0
			return d
		},
		"window too long":          func() Definition { d := base(); d.Delivery.ReplayWindowMS = 7*86400000 + 1; return d },
		"negative window":          func() Definition { d := base(); d.Delivery.ReplayWindowMS = -1; return d },
		"none mode with header":    func() Definition { d := writeVia("POST"); d.Connector.IdempotencyHeader = "Idempotency-Key"; return d },
		"lookup mode without path": func() Definition { d := lookup(); d.Connector.Lookup = nil; return d },
		"lookup with native key": func() Definition {
			d := base()
			d.Connector.Lookup = &Lookup{Method: "GET", Path: "/v1/{key}"}
			return d
		},
		"lookup manual reconcile": func() Definition { d := lookup(); d.Delivery.ReconciliationMode = "MANUAL"; return d },
		"lookup POST":             func() Definition { d := lookup(); d.Connector.Lookup.Method = "POST"; return d },
		"lookup without key":      func() Definition { d := lookup(); d.Connector.Lookup.Path = "/v1/invoices"; return d },
		"lookup key twice":        func() Definition { d := lookup(); d.Connector.Lookup.Path = "/v1/{key}/{key}"; return d },
		"lookup other param":      func() Definition { d := lookup(); d.Connector.Lookup.Path = "/v1/{key}/{amount}"; return d },
		"ipv4 literal host":       func() Definition { d := base(); d.Connector.Host = "127.0.0.1"; return d },
		"ipv6 literal host":       func() Definition { d := base(); d.Connector.Host = "::1"; return d },
		"numeric shorthand host":  func() Definition { d := base(); d.Connector.Host = "127.1"; return d },
		"hex shorthand host":      func() Definition { d := base(); d.Connector.Host = "0x7f.1"; return d },
		"hex last label host":     func() Definition { d := base(); d.Connector.Host = "a.0x7f"; return d },
		"single label host":       func() Definition { d := base(); d.Connector.Host = "localhost"; return d },
		"uppercase host":          func() Definition { d := base(); d.Connector.Host = "API.crm.example.com"; return d },
		"trailing dot host":       func() Definition { d := base(); d.Connector.Host = "api.crm.example.com."; return d },
		"host with port":          func() Definition { d := base(); d.Connector.Host = "api.crm.example.com:443"; return d },
		"host with userinfo":      func() Definition { d := base(); d.Connector.Host = "user@api.crm.example.com"; return d },
		"port zero":               func() Definition { d := base(); d.Connector.Port = 0; return d },
		"port too high":           func() Definition { d := base(); d.Connector.Port = 65536; return d },
		"unknown network":         func() Definition { d := base(); d.Connector.Network = "private"; return d },
		"bad secret ref":          func() Definition { d := base(); d.Connector.SecretRef = "../crm"; return d },
		"timeout zero":            func() Definition { d := base(); d.Connector.TimeoutMS = 0; return d },
		"timeout over 2000":       func() Definition { d := base(); d.Connector.TimeoutMS = 2001; return d },
		"response zero":           func() Definition { d := base(); d.Connector.MaxResponseBytes = 0; return d },
		"response over 16KiB":     func() Definition { d := base(); d.Connector.MaxResponseBytes = MaxResponseBytes + 1; return d },
		"rate zero":               func() Definition { d := base(); d.Connector.RatePerSecond = 0; return d },
		"rate over 100":           func() Definition { d := base(); d.Connector.RatePerSecond = 101; return d },
		"header authorization":    func() Definition { d := base(); d.Connector.IdempotencyHeader = "Authorization"; return d },
		"header host":             func() Definition { d := base(); d.Connector.IdempotencyHeader = "Host"; return d },
		"header content type":     func() Definition { d := base(); d.Connector.IdempotencyHeader = "Content-Type"; return d },
		"header accept":           func() Definition { d := base(); d.Connector.IdempotencyHeader = "Accept"; return d },
		"header expect":           func() Definition { d := base(); d.Connector.IdempotencyHeader = "Expect"; return d },
		"header content encoding": func() Definition { d := base(); d.Connector.IdempotencyHeader = "Content-Encoding"; return d },
		"header user agent":       func() Definition { d := base(); d.Connector.IdempotencyHeader = "User-Agent"; return d },
		"header proxy prefix":     func() Definition { d := base(); d.Connector.IdempotencyHeader = "Proxy-Key"; return d },
		"header sec prefix":       func() Definition { d := base(); d.Connector.IdempotencyHeader = "Sec-Key"; return d },
		"header lowercase":        func() Definition { d := base(); d.Connector.IdempotencyHeader = "idempotency-key"; return d },
		"header with space":       func() Definition { d := base(); d.Connector.IdempotencyHeader = "Idempotency Key"; return d },
		"path not absolute":       func() Definition { d := base(); d.Connector.Path = "v1/customers/{customer_id}/notes"; return d },
		"path traversal":          func() Definition { d := base(); d.Connector.Path = "/v1/../customers/{customer_id}"; return d },
		"path dot segment":        func() Definition { d := base(); d.Connector.Path = "/v1/./{customer_id}"; return d },
		"path empty segment":      func() Definition { d := base(); d.Connector.Path = "/v1//{customer_id}"; return d },
		"path trailing slash":     func() Definition { d := base(); d.Connector.Path = "/v1/{customer_id}/"; return d },
		"path percent":            func() Definition { d := base(); d.Connector.Path = "/v1/%2e%2e/{customer_id}"; return d },
		"path query":              func() Definition { d := base(); d.Connector.Path = "/v1/{customer_id}?x=1"; return d },
		"path param undeclared":   func() Definition { d := base(); d.Connector.Path = "/v1/{customer_id}/{other}"; return d },
		"path param body field":   func() Definition { d := base(); d.Connector.Path = "/v1/{customer_id}/{amount}"; return d },
		"path param twice":        func() Definition { d := base(); d.Connector.Path = "/v1/{customer_id}/{customer_id}"; return d },
		"path param unused":       func() Definition { d := base(); d.Connector.Path = "/v1/customers"; return d },
		"path param partial":      func() Definition { d := base(); d.Connector.Path = "/v1/x{customer_id}"; return d },
		"optional path field":     func() Definition { d := base(); d.Input[0].Required = false; return d },
		"body field on GET":       func() Definition { d := read(); d.Input[1].In = "body"; return d },
		"input without placement": func() Definition { d := base(); d.Input[1].In = ""; return d },
		"input placed in query":   func() Definition { d := base(); d.Input[1].In = "query"; return d },
		"input named key":         func() Definition { d := base(); d.Input[1].Name = "key"; return d },
		"duplicate input":         func() Definition { d := base(); d.Input[2].Name = "amount"; return d },
		"uppercase input":         func() Definition { d := base(); d.Input[1].Name = "Amount"; return d },
		"string without length":   func() Definition { d := base(); d.Input[4].MaxLength = 0; return d },
		"string too long":         func() Definition { d := base(); d.Input[4].MaxLength = 4097; return d },
		"integer with length":     func() Definition { d := base(); d.Input[2].MaxLength = 4; return d },
		"float type":              func() Definition { d := base(); d.Input[2].Type = "float"; return d },
		"object type":             func() Definition { d := base(); d.Output[0].Type = "object"; return d },
		"no outputs":              func() Definition { d := base(); d.Output = nil; return d },
		"duplicate output":        func() Definition { d := base(); d.Output[1].Name = "id"; return d },
		"output with placement":   func() Definition { d := base(); d.Output[1].In = "body"; return d },
		"too many inputs": func() Definition {
			d := base()
			for i := 0; i <= MaxFields; i++ {
				d.Input = append(d.Input, Field{Name: "extra_" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Type: "bool", In: "body"})
			}
			return d
		},
		"sandbox equals production": func() Definition {
			d := base()
			d.Sandbox.Endpoint.Host, d.Sandbox.Endpoint.Port = d.Connector.Host, d.Connector.Port
			return d
		},
		"reroute without endpoint": func() Definition { d := base(); d.Sandbox.Endpoint = nil; return d },
		"sandbox bad endpoint":     func() Definition { d := base(); d.Sandbox.Endpoint.Host = "10.0.0.1"; return d },
		"sandbox secret equals production": func() Definition {
			d := base()
			d.Sandbox.Endpoint.SecretRef = d.Connector.SecretRef
			return d
		},
		"test account without secret": func() Definition { d := lookup(); d.Sandbox.Endpoint.SecretRef = ""; return d },
		"read only with write":        func() Definition { d := base(); d.Sandbox.Mode = "READ_ONLY"; return d },
		"block with endpoint":         func() Definition { d := base(); d.Sandbox.Mode = "BLOCK"; return d },
		"reroute with fixture": func() Definition {
			d := base()
			d.Sandbox.Output = map[string]json.RawMessage{"id": json.RawMessage(`"x"`)}
			return d
		},
		"simulate with endpoint": func() Definition {
			d := sim()
			d.Sandbox.Endpoint = &Endpoint{Host: "geo-sandbox.example.com", Port: 443, Network: "public"}
			return d
		},
		"simulate missing required": func() Definition { d := sim(); delete(d.Sandbox.Output, "region"); return d },
		"simulate unknown member": func() Definition {
			d := sim()
			d.Sandbox.Output["secret"] = json.RawMessage(`"x"`)
			return d
		},
		"simulate wrong type":     func() Definition { d := sim(); d.Sandbox.Output["population"] = json.RawMessage(`"42"`); return d },
		"simulate fractional int": func() Definition { d := sim(); d.Sandbox.Output["population"] = json.RawMessage(`4.2`); return d },
		"unknown sandbox mode":    func() Definition { d := base(); d.Sandbox.Mode = "LIVE"; return d },
		"no tenants":              func() Definition { d := base(); d.Tenants = nil; return d },
		"invalid tenant":          func() Definition { d := base(); d.Tenants = []string{"tenant-1"}; return d },
		"uppercase tenant":        func() Definition { d := base(); d.Tenants = []string{"AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE"}; return d },
		"duplicate tenant":        func() Definition { d := base(); d.Tenants = []string{testTenant, testTenant}; return d },
		"wildcard mixed":          func() Definition { d := base(); d.Tenants = []string{"*", testTenant}; return d },
		"double wildcard":         func() Definition { d := base(); d.Tenants = []string{"*", "*"}; return d },
	}
	for name, build := range cases {
		if _, err := NewCatalog([]Definition{build()}); !errors.Is(err, ErrCatalog) {
			t.Errorf("%s accepted", name)
		}
	}
	// Duplicate ref@version and the tool count bound.
	if _, err := NewCatalog([]Definition{base(), base()}); !errors.Is(err, ErrCatalog) {
		t.Error("duplicate definition accepted")
	}
	many := make([]Definition, MaxTools+1)
	for i := range many {
		many[i] = base()
	}
	if _, err := NewCatalog(many); !errors.Is(err, ErrCatalog) {
		t.Error("tool count bound")
	}
	// A different version of the same ref is a distinct definition.
	next := base()
	next.Version = "1.1.0"
	if c, err := NewCatalog([]Definition{base(), next}); err != nil || c.Len() != 2 {
		t.Error("versions of one ref")
	}
}
