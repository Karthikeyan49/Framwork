package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ErrScriptExhausted is returned (wrapped in ErrProviderRefused, so it is never
// retried) when a ScriptedProvider is called after its last step.
var ErrScriptExhausted = fmt.Errorf("%w: scripted provider has no more steps", ErrProviderRefused)

// ScriptStep is one scripted provider answer. Func, when set, computes the payload
// from the request and wins over Payload. A non-nil Err is returned instead of a
// response. Delay is slept (honouring the context) before answering; a Delay longer
// than the context deadline yields ErrProviderTimeout.
type ScriptStep struct {
	Payload []byte
	Err     error
	Delay   time.Duration
	Usage   Usage
	Model   string
	Func    func(ProviderRequest) ([]byte, error)
}

// ScriptedProvider is the deterministic in-memory provider of ADR040 section 3. It
// is safe for concurrent use and records every request it receives.
type ScriptedProvider struct {
	mu       sync.Mutex
	steps    []ScriptStep
	next     int
	requests []ProviderRequest
}

// NewScriptedProvider returns a provider that answers with the steps in order.
func NewScriptedProvider(steps ...ScriptStep) *ScriptedProvider {
	return &ScriptedProvider{steps: append([]ScriptStep(nil), steps...)}
}

// Push appends steps to the script. Tests use it to script an answer that depends on
// state that only exists after the provider was constructed.
func (s *ScriptedProvider) Push(steps ...ScriptStep) {
	s.mu.Lock()
	s.steps = append(s.steps, steps...)
	s.mu.Unlock()
}

// Calls is the number of requests received so far.
func (s *ScriptedProvider) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// Requests returns a copy of the requests received so far, oldest first.
func (s *ScriptedProvider) Requests() []ProviderRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProviderRequest, len(s.requests))
	for i, r := range s.requests {
		out[i] = cloneRequest(r)
	}
	return out
}

func cloneRequest(r ProviderRequest) ProviderRequest {
	r.Segments = append([]Segment(nil), r.Segments...)
	r.OutputSchema = append([]byte(nil), r.OutputSchema...)
	return r
}

func (s *ScriptedProvider) Complete(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, cloneRequest(req))
	if s.next >= len(s.steps) {
		s.mu.Unlock()
		return ProviderResponse{}, ErrScriptExhausted
	}
	step := s.steps[s.next]
	s.next++
	s.mu.Unlock()
	if step.Delay > 0 {
		timer := time.NewTimer(step.Delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ProviderResponse{}, ctxError(ctx)
		}
	}
	if err := ctx.Err(); err != nil {
		return ProviderResponse{}, ctxError(ctx)
	}
	if step.Err != nil {
		return ProviderResponse{}, step.Err
	}
	payload := step.Payload
	if step.Func != nil {
		var err error
		if payload, err = step.Func(cloneRequest(req)); err != nil {
			return ProviderResponse{}, err
		}
	}
	return ProviderResponse{Payload: append([]byte(nil), payload...), Provider: "scripted", Model: step.Model, Usage: step.Usage, Latency: step.Delay}, nil
}

// ctxError maps a finished context to the typed provider failures: an expired
// deadline is ErrProviderTimeout, a cancellation stays the context's own error.
func ctxError(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%w: %v", ErrProviderTimeout, ctx.Err())
	}
	return ctx.Err()
}

// scriptFile is the on-disk shape of a development script (kind scripted).
type scriptFile struct {
	Steps []scriptFileStep `json:"steps"`
}

type scriptFileStep struct {
	Payload json.RawMessage `json:"payload"`
	Usage   Usage           `json:"usage"`
	Model   string          `json:"model"`
	DelayMS int64           `json:"delay_ms"`
	Error   string          `json:"error"`
}

// LoadScript reads a development script: {"steps":[{"payload":<string or JSON
// value>,"usage":{...},"model":"...","delay_ms":0,"error":"unavailable|timeout|
// refused|malformed"}]}. A string payload is used byte for byte; any other JSON value
// is used as its compact encoding. At most MaxScriptSteps steps.
func LoadScript(path string) ([]ScriptStep, error) {
	raw, err := readBounded(path, MaxConfigBytes)
	if err != nil {
		return nil, err
	}
	var f scriptFile
	if err = decodeStrictObject(raw, MaxConfigBytes, &f, []string{"steps"}); err != nil {
		return nil, err
	}
	if len(f.Steps) == 0 || len(f.Steps) > MaxScriptSteps {
		return nil, fmt.Errorf("%w: a script has 1..%d steps", ErrInvalidInput, MaxScriptSteps)
	}
	steps := make([]ScriptStep, 0, len(f.Steps))
	for _, s := range f.Steps {
		step := ScriptStep{Usage: s.Usage, Model: s.Model}
		if s.DelayMS < 0 || s.DelayMS > DefaultWallClock.Milliseconds() {
			return nil, fmt.Errorf("%w: step delay out of range", ErrInvalidInput)
		}
		step.Delay = time.Duration(s.DelayMS) * time.Millisecond
		switch s.Error {
		case "":
			payload := []byte(s.Payload)
			var text string
			if json.Unmarshal(s.Payload, &text) == nil {
				payload = []byte(text)
			}
			step.Payload = payload
		case "unavailable":
			step.Err = ErrProviderUnavailable
		case "timeout":
			step.Err = ErrProviderTimeout
		case "refused":
			step.Err = ErrProviderRefused
		case "malformed":
			step.Err = ErrProviderMalformed
		default:
			return nil, fmt.Errorf("%w: unknown step error", ErrInvalidInput)
		}
		steps = append(steps, step)
	}
	return steps, nil
}
