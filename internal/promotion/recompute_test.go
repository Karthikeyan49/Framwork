package promotion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// The tests of the memo and of the admission bound of the pure recomputation
// (independent review M12-API-F9, ADR038 amendment 10).

func tenantCtx(id string) context.Context { return WithTenant(context.Background(), id) }

func TestRecomputeRemembersASuccessUnderTheExactBytes(t *testing.T) {
	in, _ := firstRelease(t, orderOptions{})
	c := buildOK(t, in)
	supported := compiler.SupportedIRVersions()
	rc := NewRecompute(Limits{})
	ctx := tenantCtx(tenantA)

	first, err := rc.restore(ctx, c.JSON(), c.Hash(), supported)
	if err != nil {
		t.Fatal(err)
	}
	again, err := rc.restore(ctx, c.JSON(), c.Hash(), supported)
	if err != nil || again.candidate.Hash() != first.candidate.Hash() || again.program.Hash() != first.program.Hash() {
		t.Fatal("the remembered outcome differs from the computed one", err)
	}
	if s := rc.Stats(); s.Computes != 1 || s.Hits != 1 || s.Entries != 1 || s.Active != 0 {
		t.Fatalf("second restore must be a hit and release its token: %+v", s)
	}

	// The key is the digest of the bytes handed in, never the caller's claim: a hash
	// that does not belong to the bytes is refused before the memo is consulted.
	if _, err = rc.restore(ctx, c.JSON(), Hash([]byte("x")), supported); !errors.Is(err, ErrGate) {
		t.Fatalf("a hash that is not the digest of the bytes: %v", err)
	}
	if _, err = rc.restore(ctx, append(append([]byte{}, c.JSON()...), ' '), c.Hash(), supported); !errors.Is(err, ErrGate) {
		t.Fatalf("different bytes under the remembered hash: %v", err)
	}
	if s := rc.Stats(); s.Hits != 1 {
		t.Fatalf("a refused lookup must not count as a hit: %+v", s)
	}

	// The set of supported IR versions is part of the key: another set recomputes.
	_, _ = rc.restore(ctx, c.JSON(), c.Hash(), supported[:1])
	if s := rc.Stats(); s.Computes < 2 {
		t.Fatalf("another supported set must not be served from the memo: %+v", s)
	}
}

func TestRecomputeNeverRemembersAFailure(t *testing.T) {
	in, _ := firstRelease(t, orderOptions{})
	c := buildOK(t, in)
	bad := append(append([]byte{}, c.JSON()...), '\n') // not canonical: ErrGate
	rc := NewRecompute(Limits{})
	for i := 0; i < 3; i++ {
		if _, err := rc.restore(tenantCtx(tenantA), bad, Hash(bad), compiler.SupportedIRVersions()); !errors.Is(err, ErrGate) {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	if s := rc.Stats(); s.Computes != 3 || s.Hits != 0 || s.Entries != 0 || s.Active != 0 {
		t.Fatalf("a failure was remembered or leaked a token: %+v", s)
	}
}

func TestRecomputeVerifyWithBaseIsRememberedPerBaseProgram(t *testing.T) {
	first, firstProgram := firstRelease(t, orderOptions{})
	baseCandidate := buildOK(t, first)
	p2, s2 := compileDocument(t, orderDocument(orderOptions{threshold: "600"}), compiler.PlatformCatalog())
	requirements, acceptance := artifacts(t, "600", 2)
	second := buildOK(t, BuildInput{TenantID: tenantA, AppID: "app.orders", RequesterID: requester, Source: s2, SourceRevision: 2, Target: p2, Requirements: requirements, Acceptance: acceptance,
		Sandbox: evidenceFor(t, p2, 2), Tools: tools.EmptyCatalog(), Policy: policyInput(), Base: &BaseRelease{ArtifactHash: baseCandidate.Hash(), Generation: 1, Program: firstProgram}})
	supported := compiler.SupportedIRVersions()
	rc := NewRecompute(Limits{})
	ctx := tenantCtx(tenantA)

	got, err := rc.restore(ctx, second.JSON(), second.Hash(), supported)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(program compiler.Program) BaseResolver {
		return func(string) (compiler.Program, error) { return program, nil }
	}
	for i := 0; i < 2; i++ {
		if err = rc.verifyWithBase(ctx, got, resolve(firstProgram), supported); err != nil {
			t.Fatal(err)
		}
	}
	if s := rc.Stats(); s.Hits != 1 || s.Computes != 2 {
		t.Fatalf("the second verification against the same base must be a hit: %+v", s)
	}
	// Another base program is another computation, and its failure is not remembered.
	for i := 0; i < 2; i++ {
		if err = rc.verifyWithBase(ctx, got, resolve(p2), supported); err == nil {
			t.Fatal("verified against the wrong base")
		}
	}
	if s := rc.Stats(); s.Hits != 1 || s.Computes != 4 {
		t.Fatalf("a refused verification must be recomputed every time: %+v", s)
	}
	if err = rc.verifyWithBase(ctx, got, resolve(firstProgram), supported); err != nil {
		t.Fatal("the failures poisoned the memo", err)
	}
	if err = rc.verifyWithBase(ctx, got, nil, supported); err == nil {
		t.Fatal("a candidate with a base cannot verify without a resolver")
	}
}

func TestRecomputeConcurrentCallersOfOneKeyShareOneComputation(t *testing.T) {
	rc := NewRecompute(Limits{Process: 1, Tenant: 1})
	key := memoKey{kind: kindRestore, other: "shared"}
	release, started := make(chan struct{}), make(chan struct{})
	var runs int
	var mu sync.Mutex
	compute := func() (any, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		close(started)
		<-release
		return restored{}, nil
	}
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = rc.do(tenantCtx(tenantA), key, func(any) int { return 1 }, compute)
		}()
		if i == 0 {
			<-started // the first caller owns the computation before the others arrive
		}
	}
	for rc.Stats().Waits < int64(len(results)-1) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	for i, err := range results {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}
	if s := rc.Stats(); runs != 1 || s.Computes != 1 || s.Waits != 7 || s.Refused != 0 || s.Active != 0 || s.Entries != 1 {
		t.Fatalf("runs=%d %+v", runs, s)
	}
}

func TestRecomputeBoundsColdComputationsPerTenantAndPerProcess(t *testing.T) {
	rc := NewRecompute(Limits{Process: 2, Tenant: 1, RetryAfter: 3 * time.Second})
	hold := make(chan struct{})
	var wg sync.WaitGroup
	running := func(tenantID, name string) (cancel func()) {
		started := make(chan struct{})
		done := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rc.do(tenantCtx(tenantID), memoKey{kind: kindRestore, other: name}, func(any) int { return 1 }, func() (any, error) {
				close(started)
				<-hold
				return restored{}, nil
			})
			close(done)
		}()
		<-started
		return func() { <-done }
	}
	cold := func(tenantID, name string) error {
		_, err := rc.do(tenantCtx(tenantID), memoKey{kind: kindRestore, other: name}, func(any) int { return 1 }, func() (any, error) { return restored{}, nil })
		return err
	}

	running("tenant-a", "a1")
	var busy *BusyError
	if err := cold("tenant-a", "a2"); !errors.As(err, &busy) || busy.Scope != "tenant" || busy.RetryAfter != 3*time.Second || !errors.Is(err, ErrBusy) {
		t.Fatalf("a second cold computation of one tenant: %v", err)
	}
	running("tenant-b", "b1")
	if err := cold("tenant-c", "c1"); !errors.As(err, &busy) || busy.Scope != "process" {
		t.Fatalf("a third cold computation of the process: %v", err)
	}
	// Without a tenant (library use) nothing is bounded.
	if _, err := rc.do(context.Background(), memoKey{kind: kindRestore, other: "lib"}, func(any) int { return 1 }, func() (any, error) { return restored{}, nil }); err != nil {
		t.Fatalf("a call without a tenant was bounded: %v", err)
	}
	if s := rc.Stats(); s.Refused != 2 || s.Active != 2 {
		t.Fatalf("%+v", s)
	}
	close(hold)
	wg.Wait()
	if s := rc.Stats(); s.Active != 0 {
		t.Fatalf("tokens were not given back: %+v", s)
	}
	// What finished stays remembered: a request that was refused earlier is served.
	if err := cold("tenant-a", "a1"); err != nil {
		t.Fatal(err)
	}
	if s := rc.Stats(); s.Hits != 1 {
		t.Fatalf("%+v", s)
	}
	if err := cold("tenant-a", "a2"); err != nil {
		t.Fatalf("after the computations ended the tenant is admitted again: %v", err)
	}
}

func TestRecomputeAHitTakesNoTokenAndAPanicFailsClosed(t *testing.T) {
	rc := NewRecompute(Limits{Process: 1, Tenant: 1})
	key := memoKey{kind: kindRestore, other: "k"}
	size := func(any) int { return 1 }
	if _, err := rc.do(tenantCtx("t"), key, size, func() (any, error) { return restored{}, nil }); err != nil {
		t.Fatal(err)
	}
	// Occupy the only token with a different key; the remembered key is still served.
	hold, started := make(chan struct{}), make(chan struct{})
	go func() {
		_, _ = rc.do(tenantCtx("t"), memoKey{kind: kindRestore, other: "busy"}, size, func() (any, error) { close(started); <-hold; return restored{}, nil })
	}()
	<-started
	if _, err := rc.do(tenantCtx("t"), key, size, func() (any, error) { t.Fatal("recomputed a remembered key"); return nil, nil }); err != nil {
		t.Fatalf("a hit needs no token: %v", err)
	}
	close(hold)
	for rc.Stats().Active != 0 {
		time.Sleep(time.Millisecond)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panic was swallowed")
			}
		}()
		_, _ = rc.do(tenantCtx("t"), memoKey{kind: kindRestore, other: "boom"}, size, func() (any, error) { panic("boom") })
	}()
	if s := rc.Stats(); s.Active != 0 || s.Entries != 2 {
		t.Fatalf("a panic leaked a token or remembered a result: %+v", s)
	}
	if _, err := rc.do(tenantCtx("t"), memoKey{kind: kindRestore, other: "boom"}, size, func() (any, error) { return restored{}, nil }); err != nil {
		t.Fatalf("a later caller of the key that panicked: %v", err)
	}
}

func TestRecomputeMemoIsBoundedAndLeastRecentlyUsedFirst(t *testing.T) {
	rc := NewRecompute(Limits{MemoEntries: 3, MemoBytes: 100})
	put := func(name string, size int) {
		t.Helper()
		if _, err := rc.do(tenantCtx("t"), memoKey{kind: kindRestore, other: name}, func(any) int { return size }, func() (any, error) { return restored{}, nil }); err != nil {
			t.Fatal(err)
		}
	}
	has := func(name string) bool { _, ok := rc.entries[memoKey{kind: kindRestore, other: name}]; return ok }
	put("a", 10)
	put("b", 10)
	put("c", 10)
	put("a", 10) // touches a
	put("d", 10) // evicts b, the least recently used
	rc.mu.Lock()
	ok := has("a") && !has("b") && has("c") && has("d") && rc.lru.Len() == 3
	rc.mu.Unlock()
	if !ok {
		t.Fatal("entry bound or LRU order violated")
	}
	put("big", 80) // 10+10+10+80 > 100: the oldest go until the bytes fit
	put("huge", 101)
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.bytes > 100 || has("huge") || !has("big") {
		t.Fatalf("byte bound violated: %d bytes, huge=%v big=%v", rc.bytes, has("huge"), has("big"))
	}
}
