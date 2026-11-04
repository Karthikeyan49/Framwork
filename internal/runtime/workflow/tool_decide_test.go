package workflow

import (
	"errors"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// decideTool regression table for M10R-01/03/04 (and C6).
func TestDecideToolRoutesFreshOutcomesAndRefusesBlindResends(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Second)
	routed := stored{Instance: Instance{Status: "RETRY_WAIT", DueAt: due}}
	freshX := stored{Instance: Instance{Status: "READY", DueAt: due}}
	finishedAfter, finishedBefore := due.Add(500*time.Millisecond), due.Add(-500*time.Millisecond)
	read := ToolContract{SideEffect: "READ", Approval: "NONE", IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", TimeoutMS: 1000}
	native := ToolContract{SideEffect: "WRITE", Approval: "REQUIRED", IdempotencyMode: "NATIVE_KEY", RetryMode: "SAFE_RETRY", ReplayWindowMS: 60000, TimeoutMS: 1000}
	lookup := ToolContract{SideEffect: "WRITE", Approval: "REQUIRED", IdempotencyMode: "LOOKUP_BY_REFERENCE", RetryMode: "SAFE_RETRY", SettleMS: 1000, TimeoutMS: 1000}
	none := ToolContract{SideEffect: "WRITE", Approval: "REQUIRED", IdempotencyMode: "NONE", RetryMode: "RECONCILE_FIRST", TimeoutMS: 1000}
	failed := func(finished time.Time) toolLedger {
		return toolLedger{state: "FAILED", retryable: true, attempts: 1, firstSent: now.Add(-2 * time.Second), lastFinished: finished, lastKind: tools.KindSend}
	}
	unknown := func(finished time.Time, kind string) toolLedger {
		return toolLedger{state: "UNKNOWN", outstanding: true, attempts: 1, firstSent: now.Add(-2 * time.Second), lastFinished: finished, lastKind: kind}
	}
	retryable, permanent, unknownErr := Retryable(), Permanent(), Unknown()
	cases := []struct {
		name  string
		c     ToolContract
		l     toolLedger
		found bool
		x     stored
		kind  string
		err   error
	}{
		{"first send", read, toolLedger{}, false, freshX, tools.KindSend, nil},
		{"fresh 429 goes through the failure path", read, failed(finishedAfter), true, freshX, "", retryable},
		{"fresh 429 while RETRY_WAIT still counts (M10R-03)", read, failed(finishedAfter), true, routed, "", retryable},
		{"routed 429 resends after backoff", read, failed(finishedBefore), true, routed, tools.KindSend, nil},
		{"rejected is permanent", read, toolLedger{state: "FAILED", attempts: 1, lastFinished: finishedBefore}, true, routed, "", permanent},
		{"ledger budget settles a retryable failure", read, func() toolLedger { l := failed(finishedBefore); l.attempts = MaxLedgerAttempts; return l }(), true, routed, "", permanent},
		{"fresh unknown keyed write backs off", native, unknown(finishedAfter, tools.KindSend), true, freshX, "", retryable},
		{"routed unknown keyed write resends in window", native, unknown(finishedBefore, tools.KindSend), true, routed, tools.KindSend, nil},
		{"keyed resend whose deadline leaves the window parks (C6)", ToolContract{SideEffect: "WRITE", Approval: "REQUIRED", IdempotencyMode: "DEDUPE_WINDOW", RetryMode: "SAFE_RETRY", ReplayWindowMS: 2500, TimeoutMS: 1000}, unknown(finishedBefore, tools.KindSend), true, routed, "", unknownErr},
		{"keyed resend on a failed retry after unknown stays bounded", native, func() toolLedger {
			l := unknown(finishedBefore, tools.KindSend)
			l.state, l.class = "UNKNOWN", "rate_limited"
			return l
		}(), true, routed, tools.KindSend, nil},
		{"unknown lookup write looks up, never sends (M10R-01)", lookup, unknown(finishedBefore, tools.KindLookup), true, routed, tools.KindLookup, nil},
		{"unknown lookup write after a failed lookup looks up again", lookup, unknown(finishedBefore, tools.KindLookup), true, freshX, tools.KindLookup, nil},
		{"unknown write without idempotency reconciles", none, unknown(finishedAfter, tools.KindSend), true, freshX, "", unknownErr},
		{"unknown at the ledger budget reconciles", native, func() toolLedger {
			l := unknown(finishedBefore, tools.KindSend)
			l.attempts = MaxLedgerAttempts
			return l
		}(), true, routed, "", unknownErr},
		{"proved absence permits one send", lookup, toolLedger{state: "NOT_APPLIED", attempts: 2, lastFinished: finishedAfter}, true, freshX, tools.KindSend, nil},
		{"legacy failed row with an unknown attempt never resends", none, toolLedger{state: "FAILED", retryable: true, outstanding: true, attempts: 2, lastFinished: finishedBefore}, true, routed, "", unknownErr},
	}
	for _, c := range cases {
		kind, err := decideTool(c.c, c.l, c.found, c.x, now)
		var want Failure
		if kind != c.kind || (c.err == nil) != (err == nil) || c.err != nil && (!errors.As(err, &want) || failureKind(err) != failureKind(c.err)) {
			t.Errorf("%s: got %q %v, want %q %v", c.name, kind, err, c.kind, c.err)
		}
	}
}
