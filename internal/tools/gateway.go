package tools

import (
	"context"
	"time"
)

// Modes are resolved by the trusted runtime from the server-derived execution
// environment (ADR036/ADR037), never from requests, metadata or AI output.
const (
	ModeLive        = "LIVE"
	ModeReroute     = "REROUTE"
	ModeTestAccount = "TEST_ACCOUNT"
	ModeReadOnly    = "READ_ONLY"
	ModeSimulate    = "SIMULATE"
	ModeBlock       = "BLOCK"
)

const (
	KindSend   = "SEND"
	KindLookup = "LOOKUP"
)

// Result classes are bounded, secret-free evidence labels.
const (
	ClassOK              = "ok"
	ClassNotSent         = "not_sent"         // FAILED retryable: provably never transmitted
	ClassBlocked         = "blocked"          // FAILED permanent: mode or policy forbids I/O
	ClassRejected        = "rejected"         // FAILED permanent: provider 4xx
	ClassRateLimited     = "rate_limited"     // FAILED retryable: provider 429 or local limiter
	ClassNotFound        = "not_found"        // FAILED (lookup): reference provably absent
	ClassTimeout         = "timeout"          // UNKNOWN
	ClassServerError     = "server_error"     // UNKNOWN: 5xx/408/409 after transmission
	ClassTransport       = "transport"        // UNKNOWN: connection failed after write began
	ClassInvalidResponse = "invalid_response" // UNKNOWN: effect may exist; response unusable
)

// Request is one bounded attempt. Inputs are already typed and validated
// against Definition.Input. Key is the deterministic idempotency/reference key.
type Request struct {
	Definition Definition
	Mode       string
	Kind       string
	Key        string
	Inputs     map[string]any
	Timeout    time.Duration
}
type Result struct {
	Outcome   Outcome
	Class     string
	Retryable bool
	Status    int
	Output    map[string]any
	Simulated bool
}

// SecretResolver returns a credential for a reference at send time. Values are
// never persisted, logged, traced or returned in a Result or error.
type SecretResolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

// Executor is the gateway port consumed by the workflow runtime.
type Executor interface {
	Execute(ctx context.Context, r Request) Result
}
