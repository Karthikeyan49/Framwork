package promotion

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
)

// Memoisation and admission of the pure recomputation (independent review M12-API-F9).
//
// Restore re-derives risk, verification and the dependency hashes of a stored
// candidate, and VerifyWithBase re-derives its diff from the base release (ADR102
// amendment 2). Both are pure functions of immutable, content-addressed bytes and of
// the code of this binary. Every read of a candidate, every approve and activate and
// every build of a successor repeated them inside the request transaction, so a few
// parallel reads of one large candidate could occupy every pool connection for longer
// than the 3 s transaction budget.
//
// A Recompute therefore (1) remembers a SUCCESSFUL outcome under the SHA-256 of the
// exact bytes that were verified, so a hit proves "these very bytes verified" and
// nothing else; (2) lets concurrent callers of one key share a single computation;
// and (3) bounds how many cold computations run at once, per process and per tenant,
// refusing the rest at once with a typed BusyError instead of queueing them on a pool
// connection. What a refused request already computed stays in the memo, so its retry
// resumes where it stopped. It never remembers anything that depends on live state:
// staleness, approvals, the active pointer, role snapshots and tool status are read
// again on every request, and a failure is never remembered.

// ErrBusy is matched (errors.Is) by every *BusyError.
var ErrBusy = errors.New("release recomputation is at capacity")

// BusyError refuses a request that would start a cold recomputation while the
// bound for its tenant or for the process is reached. Nothing was computed or written.
type BusyError struct {
	Scope      string // "tenant" or "process"
	RetryAfter time.Duration
}

func (e *BusyError) Error() string {
	return "release recomputation is at capacity (" + e.Scope + ")"
}
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

var errRecomputeAborted = errors.New("release recomputation did not complete")

// Limits configures a Recompute. A zero field takes its default.
type Limits struct {
	// Process and Tenant bound the cold recomputations that run at the same time, in
	// total and per tenant. Requests served from the memo, or waiting for a computation
	// another request already started, take no token. Defaults: max(2, GOMAXPROCS/2) and
	// half of that (at least 1).
	Process, Tenant int
	// MemoBytes and MemoEntries bound the memory the memo may hold (default 96 MiB, 256).
	MemoBytes, MemoEntries int
	// RetryAfter is the delay a refused request is told to wait (default 2 s).
	RetryAfter time.Duration
}

func (l Limits) withDefaults() Limits {
	if l.Process < 1 {
		l.Process = max(2, runtime.GOMAXPROCS(0)/2)
	}
	if l.Tenant < 1 {
		l.Tenant = max(1, l.Process/2)
	}
	if l.MemoBytes < 1 {
		l.MemoBytes = 96 << 20
	}
	if l.MemoEntries < 1 {
		l.MemoEntries = 256
	}
	if l.RetryAfter < 1 {
		l.RetryAfter = 2 * time.Second
	}
	return l
}

// RecomputeStats is a snapshot of the counters, for tests and operators.
type RecomputeStats struct {
	Hits     int64 // served from the memo
	Waits    int64 // joined a computation another caller had started
	Computes int64 // computations started (each ran once, whatever the number of waiters)
	Refused  int64 // cold computations refused with a BusyError
	Entries  int
	Bytes    int
	Active   int // admission tokens currently held
}

const (
	kindRestore byte = iota + 1
	kindVerify
)

// memoKey names one pure computation: what it computes, the digest of the exact stored
// bytes, for a verification the program hash of the base, and the supported IR set.
// The code identity (runtimeIdentity) is part of the key: the outcome is only valid for
// the components that produced it.
type memoKey struct {
	kind      byte
	digest    [sha256.Size]byte
	other     string
	supported string
	identity  string
}

var runtimeIdentity = sync.OnceValue(func() string { _, hash := RuntimeComponents(); return hash })

func restoreKey(sum [sha256.Size]byte, supported []string) memoKey {
	return memoKey{kind: kindRestore, digest: sum, supported: strings.Join(supported, ","), identity: runtimeIdentity()}
}

// restored is the outcome of a successful Restore: the candidate and the executable
// program of its IR. Both are immutable values.
type restored struct {
	candidate Candidate
	program   compiler.Program
}

func (r restored) size() int { return len(r.candidate.raw) + len(r.program.JSON()) }

type memoEntry struct {
	key  memoKey
	val  any
	size int
}

type flight struct {
	done chan struct{}
	val  any
	err  error
}

// Recompute is safe for concurrent use. The zero value is not usable; use
// NewRecompute. A nil *Recompute stands for the process default.
type Recompute struct {
	limits Limits

	mu      sync.Mutex
	lru     *list.List // *memoEntry, most recently used first
	entries map[memoKey]*list.Element
	bytes   int
	flights map[memoKey]*flight
	active  int
	tenants map[string]int
	stats   RecomputeStats
}

func NewRecompute(l Limits) *Recompute {
	return &Recompute{limits: l.withDefaults(), lru: list.New(), entries: map[memoKey]*list.Element{}, flights: map[memoKey]*flight{}, tenants: map[string]int{}}
}

var defaultRecompute = NewRecompute(Limits{})

func (r *Recompute) orDefault() *Recompute {
	if r == nil {
		return defaultRecompute
	}
	return r
}

// Stats returns a snapshot of the counters.
func (r *Recompute) Stats() RecomputeStats {
	r = r.orDefault()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats
	s.Entries, s.Bytes, s.Active = r.lru.Len(), r.bytes, r.active
	return s
}

// ------------------------------------------------------------------- admission

type tenantKey struct{}

// WithTenant returns a context whose cold recomputations count against tenantID's
// bound. Without a tenant (library use, the CLI, unit tests) nothing is bounded.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

func tenantOf(ctx context.Context) string {
	t, _ := ctx.Value(tenantKey{}).(string)
	return t
}

// admitLocked takes one token for the tenant and one for the process, or refuses at
// once. A token is held only while a computation runs: a request that is refused part
// way keeps what it already computed (the memo), so its retry resumes where it stopped.
func (r *Recompute) admitLocked(tenantID string) (func(), error) {
	if tenantID == "" {
		return func() {}, nil
	}
	if r.tenants[tenantID] >= r.limits.Tenant {
		r.stats.Refused++
		return nil, &BusyError{Scope: "tenant", RetryAfter: r.limits.RetryAfter}
	}
	if r.active >= r.limits.Process {
		r.stats.Refused++
		return nil, &BusyError{Scope: "process", RetryAfter: r.limits.RetryAfter}
	}
	r.active++
	r.tenants[tenantID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.active--
			if r.tenants[tenantID]--; r.tenants[tenantID] <= 0 {
				delete(r.tenants, tenantID)
			}
		})
	}, nil
}

// Admit takes the admission tokens of the tenant in ctx for CPU-bound work the caller is
// about to do outside the memo (building a candidate) and returns the function that gives
// them back. Without a tenant in ctx it admits everything.
func (r *Recompute) Admit(ctx context.Context) (func(), error) {
	r = r.orDefault()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.admitLocked(tenantOf(ctx))
}

// ------------------------------------------------------------------------ memo

func (r *Recompute) storeLocked(key memoKey, val any, size int) {
	if el, ok := r.entries[key]; ok {
		old := el.Value.(*memoEntry)
		r.bytes -= old.size
		r.lru.Remove(el)
		delete(r.entries, key)
	}
	if size > r.limits.MemoBytes {
		return
	}
	r.entries[key] = r.lru.PushFront(&memoEntry{key: key, val: val, size: size})
	r.bytes += size
	for r.bytes > r.limits.MemoBytes || r.lru.Len() > r.limits.MemoEntries {
		oldest := r.lru.Back()
		e := oldest.Value.(*memoEntry)
		r.lru.Remove(oldest)
		delete(r.entries, e.key)
		r.bytes -= e.size
	}
}

// do returns the remembered outcome of key, joins the computation another caller has
// started, or runs compute once (admitted by the request's scope). Only a successful
// outcome is remembered. compute runs to completion whatever happens to the request
// that started it, so a request that timed out still leaves the result for its retry.
func (r *Recompute) do(ctx context.Context, key memoKey, size func(any) int, compute func() (any, error)) (any, error) {
	r.mu.Lock()
	if el, ok := r.entries[key]; ok {
		r.lru.MoveToFront(el)
		r.stats.Hits++
		v := el.Value.(*memoEntry).val
		r.mu.Unlock()
		return v, nil
	}
	if f, ok := r.flights[key]; ok {
		r.stats.Waits++
		r.mu.Unlock()
		select {
		case <-f.done:
			return f.val, f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	giveBack, err := r.admitLocked(tenantOf(ctx))
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	f := &flight{done: make(chan struct{})}
	r.flights[key] = f
	r.stats.Computes++
	r.mu.Unlock()

	var val any
	err = errRecomputeAborted
	defer func() { // also runs while a panic unwinds: waiters fail closed
		r.mu.Lock()
		delete(r.flights, key)
		if err == nil {
			r.storeLocked(key, val, size(val))
		}
		r.mu.Unlock()
		giveBack()
		f.val, f.err = val, err
		close(f.done)
	}()
	val, err = compute()
	return val, err
}

// remember stores the outcome of a Restore that was just proven by a self-check.
func (r *Recompute) remember(c Candidate, program compiler.Program, supported []string) {
	sum := sha256.Sum256(c.raw)
	if hex.EncodeToString(sum[:]) != c.hash {
		return
	}
	v := restored{c, program}
	r.mu.Lock()
	r.storeLocked(restoreKey(sum, supported), v, v.size())
	r.mu.Unlock()
}

// restore is Restore, remembered. raw is hashed here, so a hit can only be produced by
// the exact bytes that were verified; the caller's hash must be their digest.
func (r *Recompute) restore(ctx context.Context, raw []byte, hash string, supported []string) (restored, error) {
	if len(raw) > MaxBundleBytes {
		return restored{}, ErrGate
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != hash {
		return restored{}, ErrGate
	}
	v, err := r.do(ctx, restoreKey(sum, supported), func(v any) int { return v.(restored).size() }, func() (any, error) {
		c, program, err := restoreProgram(raw, hash, supported)
		return restored{c, program}, err
	})
	if err != nil {
		return restored{}, err
	}
	return v.(restored), nil
}

// verifyWithBase is Candidate.VerifyWithBase, remembered under the digests of the
// candidate and of its base program: the diff, the storage class and the evolution
// plan are pure functions of those two immutable values.
func (r *Recompute) verifyWithBase(ctx context.Context, got restored, resolve BaseResolver, supported []string) error {
	c := got.candidate
	b, ok := c.V2()
	if !ok {
		return ErrGate
	}
	base := compiler.Program{}
	if b.BaseRelease != "" {
		if resolve == nil {
			return gateError("the base release cannot be resolved")
		}
		program, err := resolve(b.BaseRelease)
		if err != nil || !program.Valid() {
			return gateError("the base release is not available")
		}
		base = program
	}
	key := memoKey{kind: kindVerify, digest: sha256.Sum256(c.raw), other: base.Hash(), supported: strings.Join(supported, ","), identity: runtimeIdentity()}
	_, err := r.do(ctx, key, func(any) int { return 128 }, func() (any, error) {
		return struct{}{}, verifyAgainstBase(b, base, got.program)
	})
	return err
}
