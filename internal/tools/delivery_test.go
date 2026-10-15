package tools

import "testing"

func TestDeclaredGuaranteesForTimeoutDuplicateAndCrash(t *testing.T) {
	for _, mode := range []string{"NATIVE_KEY", "LOOKUP_BY_REFERENCE", "DEDUPE_WINDOW", "NONE"} {
		d := Delivery{SideEffectClass: "FINANCIAL", IdempotencyMode: mode, RetryMode: "RECONCILE_FIRST", ReconciliationMode: "REMOTE_LOOKUP"}
		if d.Validate() != nil {
			t.Fatal(mode)
		}
		// A timeout/crash after a remote response is UNKNOWN, not a confirmed failure.
		if d.MayRetry(Unknown, true, false) || d.MayRetry(Success, true, true) || d.MayRetry(Outcome("unrecognized"), true, true) {
			t.Fatal("unsafe retry", mode)
		}
		want := mode != "NONE"
		if d.MayRetry(Unknown, true, true) != want {
			t.Fatal("reconciliation capability", mode)
		}
		if mode == "DEDUPE_WINDOW" && d.MayRetry(Failed, false, true) {
			t.Fatal("expired dedupe")
		}
	}
	unsafe := Delivery{SideEffectClass: "WRITE", IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL"}
	if unsafe.Validate() == nil {
		t.Fatal("non-idempotent automatic retry")
	}
	read := Delivery{SideEffectClass: "READ", IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY"}
	if !read.MayRetry(Failed, false, false) {
		t.Fatal("pure read retry")
	}
}
