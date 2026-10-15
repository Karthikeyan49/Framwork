// Package tools declares delivery guarantees. No external tool executes in M6R.
package tools

import "errors"

type Delivery struct {
	SideEffectClass, IdempotencyMode, RetryMode, ReconciliationMode string
	Compensation                                                    bool
}
type Outcome string

const Success Outcome = "SUCCESS"
const Failed Outcome = "FAILED"
const Unknown Outcome = "UNKNOWN_REQUIRES_RECONCILIATION"

func (d Delivery) Validate() error {
	switch d.SideEffectClass {
	case "NONE", "READ", "WRITE", "FINANCIAL", "DESTRUCTIVE":
	default:
		return errors.New("unknown side effect")
	}
	switch d.IdempotencyMode {
	case "NATIVE_KEY", "LOOKUP_BY_REFERENCE", "DEDUPE_WINDOW", "NONE":
	default:
		return errors.New("unknown idempotency")
	}
	switch d.RetryMode {
	case "NEVER", "SAFE_RETRY", "RECONCILE_FIRST":
	default:
		return errors.New("unknown retry")
	}
	if d.IdempotencyMode == "NONE" && d.SideEffectClass != "NONE" && d.SideEffectClass != "READ" && d.RetryMode == "SAFE_RETRY" {
		return errors.New("unsafe non-idempotent retry")
	}
	if d.SideEffectClass != "NONE" && d.SideEffectClass != "READ" && d.ReconciliationMode != "MANUAL" && d.ReconciliationMode != "REMOTE_LOOKUP" {
		return errors.New("effect requires reconciliation")
	}
	return nil
}
func (d Delivery) MayRetry(out Outcome, windowValid bool, reconciledAbsent bool) bool {
	if d.Validate() != nil || d.RetryMode == "NEVER" || out != Failed && out != Unknown {
		return false
	}
	if d.RetryMode == "RECONCILE_FIRST" && !reconciledAbsent {
		return false
	}
	if out == Unknown && d.IdempotencyMode != "NATIVE_KEY" && !reconciledAbsent {
		return false
	}
	switch d.IdempotencyMode {
	case "NATIVE_KEY":
		return true
	case "LOOKUP_BY_REFERENCE":
		return reconciledAbsent
	case "DEDUPE_WINDOW":
		return windowValid
	case "NONE":
		return d.SideEffectClass == "NONE" || d.SideEffectClass == "READ"
	}
	return false
}
