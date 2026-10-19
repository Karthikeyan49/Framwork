package tools

import "testing"

// Delivery.Validate admits only combinations that can be reconciled. Retry and reconciliation decisions over a
// ledger are tested where they are made: TestDecideToolRoutesFreshOutcomesAndRefusesBlindResends.
func TestDeliveryDeclarationsAreValidatedForTheirRisk(t *testing.T) {
	for _, mode := range []string{"NATIVE_KEY", "LOOKUP_BY_REFERENCE", "DEDUPE_WINDOW", "NONE"} {
		d := Delivery{SideEffectClass: "FINANCIAL", IdempotencyMode: mode, RetryMode: "RECONCILE_FIRST", ReconciliationMode: "REMOTE_LOOKUP"}
		if d.Validate() != nil {
			t.Fatal(mode)
		}
		// An effect that cannot be reconciled is refused whatever the retry mode.
		d.ReconciliationMode = "NONE"
		if d.Validate() == nil {
			t.Fatal("effect without reconciliation", mode)
		}
	}
	unsafe := Delivery{SideEffectClass: "WRITE", IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL"}
	if unsafe.Validate() == nil {
		t.Fatal("non-idempotent automatic retry")
	}
	for _, bad := range []Delivery{
		{SideEffectClass: "SHELL", IdempotencyMode: "NONE", RetryMode: "NEVER"},
		{SideEffectClass: "READ", IdempotencyMode: "SOMETIMES", RetryMode: "NEVER"},
		{SideEffectClass: "READ", IdempotencyMode: "NONE", RetryMode: "ALWAYS"},
	} {
		if bad.Validate() == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
	read := Delivery{SideEffectClass: "READ", IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY"}
	if read.Validate() != nil {
		t.Fatal("a pure read may retry")
	}
}
