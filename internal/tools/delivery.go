// Package tools declares delivery guarantees (Delivery.Validate), the operator catalog and the governed gateway
// that performs a tool call (M10, ADR036). Which attempt may follow which outcome is decided by the workflow
// runtime over the durable ledger (internal/runtime/workflow.decideTool), never by a rule declared here.
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
