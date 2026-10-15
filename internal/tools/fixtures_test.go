package tools

import "encoding/json"

const testTenant = "11111111-2222-4333-8444-555555555555"

// testKey is a deterministic 64-hex idempotency/reference key.
const testKey = "4f2b8c0d1e3a5b7c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e"

// writeDefinition is a WRITE POST tool with a native idempotency key, path and
// typed body inputs, and a REROUTE sandbox with its own secret.
func writeDefinition(host string, port int, sandboxHost string, sandboxPort int) Definition {
	return Definition{
		Ref: "tool.crm.create_note", Version: "1.0.0", SideEffect: "WRITE", Approval: "REQUIRED",
		Delivery: DeliverySpec{IdempotencyMode: "NATIVE_KEY", RetryMode: "SAFE_RETRY", ReconciliationMode: "REMOTE_LOOKUP", ReplayWindowMS: 86400000},
		Connector: Connector{
			Endpoint: Endpoint{Host: host, Port: port, Network: "internal", SecretRef: "crm_live"},
			Method:   "POST", Path: "/v1/customers/{customer_id}/notes", IdempotencyHeader: "Idempotency-Key",
			TimeoutMS: 2000, MaxResponseBytes: 4096, RatePerSecond: 50,
		},
		Input: []Field{
			{Name: "customer_id", Type: "string", Required: true, MaxLength: 64, In: "path"},
			{Name: "amount", Type: "decimal", Required: true, In: "body"},
			{Name: "count", Type: "integer", Required: true, In: "body"},
			{Name: "urgent", Type: "bool", In: "body"},
			{Name: "note", Type: "string", MaxLength: 200, In: "body"},
		},
		Output: []Field{
			{Name: "id", Type: "string", Required: true, MaxLength: 64},
			{Name: "status", Type: "string", MaxLength: 32},
			{Name: "total", Type: "integer"},
			{Name: "accepted", Type: "bool"},
			{Name: "balance", Type: "decimal"},
		},
		Sandbox: SandboxSpec{Mode: "REROUTE", Endpoint: &Endpoint{Host: sandboxHost, Port: sandboxPort, Network: "internal", SecretRef: "crm_sandbox"}},
		Tenants: []string{"*"},
	}
}

// readDefinition is a READ GET tool with typed path parameters, no key and a
// READ_ONLY sandbox without a credential.
func readDefinition(host string, port int, sandboxHost string, sandboxPort int) Definition {
	return Definition{
		Ref: "tool.crm.get_balance", Version: "2.1.0", SideEffect: "READ", Approval: "NONE",
		Delivery: DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: Connector{
			Endpoint: Endpoint{Host: host, Port: port, Network: "internal"},
			Method:   "GET", Path: "/v2/accounts/{account}/balance/{year}/{active}",
			TimeoutMS: 1500, MaxResponseBytes: 1024, RatePerSecond: 10,
		},
		Input: []Field{
			{Name: "account", Type: "string", Required: true, MaxLength: 128, In: "path"},
			{Name: "year", Type: "integer", Required: true, In: "path"},
			{Name: "active", Type: "bool", Required: true, In: "path"},
		},
		Output:  []Field{{Name: "balance", Type: "decimal", Required: true}, {Name: "currency", Type: "string", MaxLength: 3}},
		Sandbox: SandboxSpec{Mode: "READ_ONLY", Endpoint: &Endpoint{Host: sandboxHost, Port: sandboxPort, Network: "internal"}},
		Tenants: []string{testTenant},
	}
}

// lookupDefinition is a WRITE tool reconciled by reference lookup with a
// TEST_ACCOUNT sandbox.
func lookupDefinition(host string, port int, sandboxHost string, sandboxPort int) Definition {
	return Definition{
		Ref: "tool.payments.create_invoice", Version: "1.2.3", SideEffect: "WRITE", Approval: "REQUIRED",
		Delivery: DeliverySpec{IdempotencyMode: "LOOKUP_BY_REFERENCE", RetryMode: "RECONCILE_FIRST", ReconciliationMode: "REMOTE_LOOKUP"},
		Connector: Connector{
			Endpoint: Endpoint{Host: host, Port: port, Network: "internal", SecretRef: "pay_live"},
			Method:   "PUT", Path: "/v1/invoices", IdempotencyHeader: "X-Reference-Key",
			Lookup:    &Lookup{Method: "GET", Path: "/v1/invoices/by-reference/{key}"},
			TimeoutMS: 2000, MaxResponseBytes: 2048, RatePerSecond: 5,
		},
		Input:   []Field{{Name: "amount", Type: "decimal", Required: true, In: "body"}},
		Output:  []Field{{Name: "invoice_id", Type: "string", Required: true, MaxLength: 64}, {Name: "state", Type: "string", MaxLength: 16}},
		Sandbox: SandboxSpec{Mode: "TEST_ACCOUNT", Endpoint: &Endpoint{Host: sandboxHost, Port: sandboxPort, Network: "internal", SecretRef: "pay_sandbox"}},
		Tenants: []string{testTenant, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
	}
}

// simulateDefinition is a NONE-effect tool whose sandbox is a fixture.
func simulateDefinition(host string, port int) Definition {
	return Definition{
		Ref: "tool.geo.lookup_region", Version: "1.0.0", SideEffect: "NONE", Approval: "NONE",
		Delivery: DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: Connector{
			Endpoint: Endpoint{Host: host, Port: port, Network: "public", SecretRef: "geo_live"},
			Method:   "GET", Path: "/v1/regions/{code}",
			TimeoutMS: 1000, MaxResponseBytes: 512, RatePerSecond: 100,
		},
		Input:  []Field{{Name: "code", Type: "string", Required: true, MaxLength: 8, In: "path"}},
		Output: []Field{{Name: "region", Type: "string", Required: true, MaxLength: 32}, {Name: "population", Type: "integer"}, {Name: "coastal", Type: "bool"}},
		Sandbox: SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{
			"region": json.RawMessage(`"north"`), "population": json.RawMessage(`42`),
		}},
		Tenants: []string{"*"},
	}
}

func writeInputs() map[string]any {
	return map[string]any{"customer_id": "cust-42", "amount": "12.50", "count": int64(3), "urgent": true, "note": "hello <world> & \"friends\""}
}
