# Robustness audit: panics, leaks, context handling, unbounded growth, concurrency

Branch `audit/panic-leak-robustness`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`).
Scope: `cmd/` and `internal/` (187 non-test Go files, 37,840 lines). No database stack: every database-dependent claim is NOT RUN.
Report-only: no product code, test or document was changed. All probes are `zz_review_*` files that were run from a scratch
snapshot outside the worktree (tracked files copied byte for byte) and are not in this commit. The host was saturated (load
average 20 to 33 on 4 cores, about 12 waiters on the heavy lock), so every timing below is inflated by a factor of about 3 to 5.

## Verdict: PASS WITH FINDINGS (four LOW findings, no HIGH, no MEDIUM)

No panic, no goroutine leak, no handle leak and no super-linear behaviour was found in the code paths that could be exercised
without a database. The `-race -count=3` run is NOT RUN (see E-9). Below, the static reading comes first (with the answer for
each item of the brief), then what was executed (E-1 to E-9).

## Findings

| id | sev | where | summary |
|---|---|---|---|
| RB-01 | LOW | `internal/platform/httpserver/httpserver.go:50-94`, `internal/platform/app/app.go:288` | `/readyz` is an anonymous, unthrottled fan-out (12 goroutines, about 29 database statements and 5 Redis commands per request) and its probe goroutines have no `recover`: one probe panic ends the process. The fan-out is the documented residual of ADR061 (quantified here); the missing `recover` is new. |
| RB-02 | LOW | `internal/ai/repair.go:115`, `:141-143`, `:152` | `maxAbandonedCalls = 16` is a check-then-act bound: 41 abandoned provider calls were observed at once with 200 concurrent runs. Needs an adapter that ignores its context (neither shipped adapter does). |
| RB-03 | LOW | `internal/platform/app/app.go:134`, `:169`, `:211-213`; `internal/runtime/workflow/execution.go:485`; `internal/platform/jobs/dispatch.go:23` | No worker, dispatcher or consumer loop recovers a panic: a panic ends the whole process (fail-stop, never a silent death), so a panic that depends on one instance's stored data would crash-loop every tenant's work on that worker. No such panic was found; this is the missing containment, not a witnessed crash. |
| RB-04 | LOW | `cmd/migrate/main.go:117` | `migrate up` has one fixed 30 s wall clock for the lock wait and all pending migrations; the migration running when the 30 s end is cancelled and rolled back (each migration is its own transaction; none uses `NO TRANSACTION`), and a migration that alone needs longer fails on every retry, with no flag to extend the limit. NOT RUN against a large database. |

### RB-01 detail

* `Handler` registers `GET /readyz` on the same mux as the API (`httpserver.go:53`). The traffic gate and the FIFO admission live
  only in `access.API.serve` (`access.go:242-243`), so `/readyz` is outside both. `README.md:196` documents it among the API endpoints
  and `apps/web/src/api/client.ts:44` calls it from the browser.
* One request starts one goroutine per probe (`httpserver.go:66`); `app.go:288` registers 12: postgres, redis, workflows, signin,
  policy, events, identity, tenant, audit, data, metadata, ai. Counting the statements in each `Ready`: postgres 2 (ping plus the
  `checkRuntime` privilege-catalog scan, `postgres.go:31-77`), identity 1, tenant 3, data 6, audit 1, policy 3, events 7,
  workflow 4 (when enabled), ai 1 (when configured), metadata 2, plus Redis ping and 3 sign-in commands: about 29 database
  statements and 5 Redis commands. Up to ten of them run concurrently against a pool of 10 (`postgres.go:23`), outside the
  admission gate (default 4 concurrent, queue 128), so one request can hold every pool connection while real requests wait in `db.Conn`
  (3 s cap, `transaction.go:87` and `:101`).
* ADR061 says "readiness needs network/operator protection" (`docs/adr/061-m9r-pre-database-request-admission.md:51-52`), so the
  fan-out is a documented residual; the default listen address is loopback. What is new is the quantification above and the
  missing `recover`.
* Executed (E-2): 100 concurrent anonymous `GET /readyz` against a stub with 12 probes gave 100 x HTTP 200, 0 x 429, and 1200
  probe bodies running at once. Executed (E-8): a probe that panics ends the process (`exit status 2`, `panic: assignment to entry
  in nil map`) while a handler that panics is answered 500 by `recoverer` (`httpserver.go:190-200`) and the process survives.
* Database cost of one `/readyz`, pool starvation under flood: NOT RUN (no database).
* Repro: copy the RB-01 test file of the appendix into `internal/platform/httpserver/` and run
  `gt test -count=1 -run 'TestRepro' -v ./internal/platform/httpserver` (observed: `200=100 429=0 peak=1200`, then `exit status 2` for the
  probe and `ZZ-STATUS 500` for the handler).
* Fix direction (not applied): `defer recover` in the probe goroutine, a per-peer limiter or a separate listener for `/readyz`.

### RB-02 detail

`call` (`repair.go:114-149`) reads `g.abandoned.Load() >= maxAbandonedCalls` (line 115) and increments only later, when a caller
gives up (line 141-143). Any number of concurrent callers pass the check before the first increments. Executed (E-3): 200 concurrent
`RunRequirements` against an adapter family where one in six ignores its context gave `abandonedBeforeRelease=41` and 41 adapter
bodies still running, against the documented "at most 16" (`repair.go:112`). M14R-07 is recorded FIXED (`docs/verification/M14_INDEPENDENT_REVIEW.md:223`); the fix holds for sequential callers only. The
earlier review's test (`INTEGRATION_INDEPENDENT_REVIEW.md:2670`) issues its 20 requests sequentially, so it cannot see this. After the adapters returned,
the counter fell to 0 and the goroutine count to the baseline: no leak, only an unenforced bound. Consequence: goroutines and
buffered requests of every abandoned call stay until the adapter returns; concurrency is bounded only by the tenants'
`InFlightPerTenant` and the HTTP server. The shipped adapters (`provider_http.go:267`, `provider_scripted.go:85`) honour the context.

### RB-03 detail

* `app.go:134` and `:169` start the dispatcher and the worker with `wg.Go`; `app.go:211-213` run the event dispatcher and consumer
  on the main goroutine. None has a `recover`; only the local activity is contained (`invoke`, `execution.go:370-376`) and the tool
  gateway (`executor.go:153-162`). A panic anywhere else in `Worker.Step` is an unrecovered goroutine panic: the process exits
  with status 2 and the supervisor restarts it. I read `step`, `recordFailure`, `call`, `receipt`, the retry arithmetic
  (`execution.go:415-450`: `1 << (x.Attempt-1)` is reached only after `x.Attempt++`, so the shift is never negative) and the
  `Activity` and `ToolContract` dereferences (all nil-guarded) and found no reachable panic.
* So the answer to "process exit or silent death" is process exit for the worker and dispatcher loops, and process exit for a
  `/readyz` probe (RB-01). HTTP handlers are covered twice (`recoverer` and `net/http`). The four helper goroutines
  (`repair.go:124`, `provider_http.go:128`, `netpolicy.go:249`, `executor.go:276`) all recover.
* Not executed: no instance data that panics the worker was found, so the crash loop is a consequence of Go semantics, not an
  observation.

## What was read (static), with the answer for each item of the brief

| item | result |
|---|---|
| every `go` statement | 6 `go` plus 2 `wg.Go`: see RB-01, RB-03 and the recover list above; every channel they write to is buffered (1 or `len(Probes)`), so none blocks forever |
| `time.After` / `time.Tick` in loops | none in non-test code. Five `time.NewTimer` (`dispatch.go:30`, `execution.go:492`, `delivery.go:265`, `retry.go:60`, `provider_scripted.go:85`), each stopped or drained on every path |
| copied mutexes | `go vet ./...` (copylocks) clean (E-1); every mutex owner is used by pointer (`Gate`, `BodyBudget`, `Admission`, `Recompute`, `rateLimiter`, `rejectionLog`) |
| unbounded maps, slices, caches, channels | all bounded: traffic pools 8192 keys with idle eviction (`gate.go:21-22`, `get`), `BodyBudget.bySubject` deleted at zero (`bodybudget.go:93`), tool limiter 4096 buckets (`ratelimit.go:10`), `Recompute` LRU by bytes and entries (`recompute.go:236-253`) with per-tenant tokens deleted at zero, `patchSchemas` stored only for 6 known versions (`schema_text.go:36`, `:91-193`), `fieldCache` keyed by Go type, sign-in flows capped 120 global / 20 per remote with TTL (`cache/login.go:17-28`), event stream `MaxLen 10000` (`redis.go:68`), audit batch 8192 (`batch.go:15`), admission queue 128 |
| `rows.Close`, `tx.Rollback`, `Body.Close` | every `QueryContext` call that binds a `*sql.Rows` closes it (scan of the 36 call sites; four showed no `Close` within six lines and were read by hand: `shadow.go:117` defers it, `unique.go:176` closes on both paths, `dataset.go:344` closes at `:367`, `metadata.go:135` is not a rows site); `defer tx.Rollback()` at `transaction.go:132`, `cmd/iam`, `cmd/worker-admin`; `defer resp.Body.Close()` at `provider_http.go:283`, `executor.go:336`, and the reject path at `identity/login.go:111`; all 13 `io.ReadAll` calls sit behind `io.LimitReader` (the one unbounded read, `os.ReadFile(c.ToolCAFile)` at `app.go:46`, names an operator-owned file) |
| `context.Background()` on request paths | 15 occurrences: process roots in `cmd/*`, shutdown contexts (`app.go:88`, `:313`), the deliberate 1 s connection cleanup (`transaction.go:110`) and `verification/verifier.go:386`, a pure call bounded by `len(plan.States)` and `MaxWork`; no request path loses cancellation. `Prepare` (redaction and compile, `ai/service.go:290`, called inside the request transaction at `platform/access/ai.go:422`) is CPU-bound inside the 3 s transaction (`transaction.go:87`): the redaction alone measured 0.1-2.0 s for one 256 KiB hostile string on the loaded host (E-7), under the cap but with little margin on a loaded node |
| `recover()` coverage | 12 sites: `httpserver.go:193`, `repair.go:127`, `provider_http.go:130`, `service.go:327` (provider phase becomes `ErrGatewayFailed`), `execution.go:372`, `executor.go:154`, `:278`, `netpolicy.go:251`, `traffic/gate.go:164`, and three re-panics (`transaction.go:92`, `preview.go:109`, `aggregate.go:121`) that leave the rollback to `WithRuntime` as documented |
| integer conversions and size arithmetic | no `make` takes an untrusted number (every size is `len()` of a bounded collection; `corpus.go:550` is test-only data capped at 100000); the only narrowing conversions are `uint16(port)` at `netpolicy.go:224` and `provider_http.go:160`, with ports validated 1..65535 (`tools/catalog.go:410`, `ai/config.go:204`); `query.Limit+1` is reached only after `Limit <= MaxPage` (`query.go:212`); revision and cursor parses are range-checked (`metadata.go:135`) |
| reflection and type assertions | `reflect` is used for `DeepEqual` and `jsonFields` (called only with struct types, `schema.go:219`); the unchecked assertions `normalized[f.ID].(string)` (`store.go:278`, `shadow.go:257`, `dataset.go:440`, `patch.go:158`) are guarded by the reference normaliser that returns only strings (`values.go:105-110`); `http.DefaultTransport.(*http.Transport)` (`identity/login.go:136`) runs at start-up only and nothing in the repository replaces the default transport |
| `json.Unmarshal` into interfaces, huge depth | `metadata.DecodeJSON` caps depth 32, nodes 16000, numbers 64 characters with exponent +-18 (`validation.go:225-310`); `DecodeYAML` rejects aliases and anchors after building nodes (no expansion) and applies the same caps; `encoding/json` itself stops at depth 10000. Executed in E-4 and E-5 |
| super-linear behaviour | the redactor (RE2 regexps plus 4 settling passes plus a fold pass) scales 2.0-2.5x per doubling of input, 32 KiB to 256 KiB, for five hostile shapes (E-7); ReDoS is excluded by Go's regexp engine |
| map iteration order in hashed or canonical output | scripted scan of every `range` over a map-typed name that appends or writes: 5 hits, all in `compiler/diff.go` (`:525`, `:529`, `:711`, `:731`) and `classify.go:143`, all sorted afterwards (`diff.go:443-468`, `classify.go:52`) |
| shutdown ordering, `WaitGroup` | `wg.Go` only; `defer func(){stopJobs(); wg.Wait()}()` (`app.go:128`); the consumer and dispatcher loops return only when `ctx` is done, so the `wg.Wait()` at `app.go:216` cannot deadlock; HTTP: `Shutdown` then `Close` fallback, deferred closes run afterwards |
| `select` without a `ctx` case | 13 selects and one plain receive in non-test code; each has `ctx.Done()`, a bounded timer, or waits on a buffered channel that a goroutine with a deadline fills (`repair.go:137`, `provider_http.go:137`, `execution.go:493`, `dispatch.go:31`, `httpserver.go:69`, `app.go:307`, `admission.go:56`, `retry.go:61`, `delivery.go:267`, `netpolicy.go:258`, `executor.go:285`, `recompute.go:273`; `app.go:319` receives after a successful `Shutdown`) |
| retry loops | `RetryWithin` is bounded by budget and attempts and checks `ctx` (`retry.go:50-72`); `Dispatcher.Claim` retries 4 times |

## What was executed

Commands run from `/tmp/claude-0/.../scratchpad/rb/snap` (tracked tree plus the temporary `zz_review_*` files) with the pinned
toolchain through `gt`; heavy runs through `hv nice -n 10`.

| id | attack / command | result |
|---|---|---|
| E-1 | `hv nice -n 10 gt vet ./...` (tests included) | PASS: exit 0, no output |
| E-2 | HTTP server leak probe (`TestZZLeakHTTPServer`): real `httpserver.New` + `Handler`, 200 requests: 60 `/healthz`, 40 `/readyz` with a context-honouring, a context-ignoring (400 ms) and a fast probe, 40 client aborts against a blocked handler, 20 handler panics, 20 uploads aborted mid-body, 20 raw half-open connections (partial header or body then close) | PASS: goroutines before 3, peak 628, 63 right after the requests, 2 after `Shutdown` and settling (limit 3+2); statuses: 200 x60, 503 x40 (probe deadline, by design), abort errors x40, upload aborts x20, 500 x20, probe calls 120 |
| E-3 | AI gateway leak probe (`TestZZLeakAIGateway`): 200 concurrent `RunRequirements` with a provider that answers, honours the context, ignores it, panics, returns garbage (repair loop) or is slow; half of the callers cancel after 0-60 ms; wall clock 120 ms | PASS for leaks: goroutines before 2, peak 307, after release 2; abandoned counter 0 after release; 236 provider calls, about 39 adapter panics did not end the process. RB-02: 41 abandoned at once |
| E-4 | native fuzz, 30 s, 2 workers: `ParsePatchProposal`, `ParseRequirementProposal`, `workflow.Restore`, workflow `CompileDefinitionWithTools` (authored workflow), `rules.Compile`+`Evaluate`+`Restore` (two targets), `metadata.Validator.Parse`/`ParseYAML`/`DecodeJSON`/`DecodeYAML`, `compiler.Restore` + accessors, each with a 3-4 s time guard | PASS, no crasher, no slow input (figures below) |
| E-5 | structure-aware mutation (valid documents; replace by 55 hostile values such as 1e19, depth-40 nests, 70 KB strings, 300-key objects; delete, copy subtrees, duplicate array elements), 25 s per target | PASS: AI parsers 17,118 calls (1,683 accepted, canonical and apply and compile and diff all run on the accepted ones); metadata 4,117 (724 valid); compiler 4,528 (747 compiled, each restored, diffed both ways, storage-classified); workflow 16,144 (1,222 compiled); rules 9,867 (651 compiled). 0 panics, 0 slow (limit 4 s), slowest call 1.03 s on the loaded host |
| E-6 | `Admission` under 4000 goroutines with random cancellation (capacity 5000, limit 3) and `BodyBudget` under 2000 goroutines with double `Release` and `Add` after `Release` | PASS: max holders 3, final `Active 0 Queued 0`, goroutines back to base; budget never exceeded and ended at 0 bytes, 0 subjects |
| E-7 | redactor on 256 KiB of 25 hostile shapes, then 32-256 KiB scaling for five of them | PASS: no quadratic growth (x1.3 to x2.7 per doubling of the input; one 5x step from 32 to 64 KiB on a 34 ms base is noise); worst single call 2.0 s on the loaded host |
| E-8 | `/readyz` probe panic versus handler panic in a child process | handler panic: HTTP 500, process alive; probe panic: process exits with status 2 (RB-01) |
| E-9 | `hv nice -n 10 gt test -race -count=3` on the concurrency tests of 11 packages | NOT RUN (lock never granted); the same tests without `-race`, `-count=3`: all `ok` |

E-4 figures (executions in a 30 s run with 2 workers; the first runs were largely stalled by the input minimiser, which makes
the engine report 0 executions per second for tens of seconds, so `ParsePatchProposal`, `ParseRequirementProposal`,
`workflow.Restore`, `compiler.Restore` and the validator were rerun with `-fuzzminimizetime=0`). Final figures, all without a
crasher: `ParsePatchProposal` 190,752 (359 new coverage inputs); `ParseRequirementProposal` 225,004 (408); `workflow.Restore`
182,242 (390); workflow compile of an authored definition 309,156 (132, first run); `rules` definition compile, evaluate and restore
647,587 (57, first run); `rules.Restore` 427,996 (2, first run); validator (JSON and YAML) 18,817 (363); `compiler.Restore` 141,751 (377).
In the first runs the engine stalled for most of the 30 s on three targets (totals 73 for `workflow.Restore`, 9 for the validator,
13 for `compiler.Restore`: not evidence); the first-run totals of the AI parsers were 236,095 and 282,984.

### E-9: `-race -count=3`: NOT RUN

Reason: the heavy lock was never granted. The first attempt (per-package runs, 06:28) and the second (one combined job, 06:45) sat
behind eight to twelve other waiters (`flock` is not fair; load average 20 to 33); the job was cancelled at 07:00 so that it would not
take the lock after this report was committed. Data-race freedom is therefore UNVERIFIED by this audit.

The job that was queued (run it under `hv` from a clean worktree at the pin; about 5 to 10 minutes on an idle host):

```
hv nice -n 10 gt test -race -count=3 -run 'ParallelAdmission|Admission|Concurren|Cancellation|RateLimiter|Recompute|BodyBudget|BookedBody|ExtendWriteDeadline|RunPatchIsSafe|CorpusUnderConcurrency|HTTPProvider|ACanceledCall|AbandonedProviderCalls|AnAdapterThatIgnores|RejectionLog|OIDC|EmbeddedSchemas|IndependentOlderCompletion|TLS' \
  ./internal/platform/traffic ./internal/platform/postgres ./internal/platform/signin ./internal/platform/access ./internal/platform/httpserver \
  ./internal/tools ./internal/promotion ./internal/ai ./internal/identity ./internal/metadata ./internal/runtime/rules
```

Fallback that was executed (NOT a substitute): the same selection, `-count=3`, WITHOUT `-race`, one package per run (no lock needed):
`traffic`, `postgres`, `signin`, `tools`, `access`, `promotion`, `ai` (31.6 s, including the two leak probes three times),
`httpserver`, `identity`, `metadata`, `runtime/rules` all `ok`. This shows the selected concurrency tests pass three times in a row; it
cannot show the absence of a data race.

## NOT RUN / not covered

* Anything that needs PostgreSQL or Redis: the real `access.API.serve` under client aborts (pool and admission slots, held
  transactions), the database cost of `/readyz`, connection-pool starvation under a `/readyz` flood, the long-migration case of RB-04,
  the DB-backed worker step.
* Fuzzing of `policy.CompileDefinition`, `promotion.Restore`/`RestoreEvolution`, `events.DecodeEnvelope`/`DecodePointer`
  (an existing `FuzzPointer` covers the last), `tools.LoadCatalog`, `intent` parsers outside the AI path, and the token verifier.
* `go vet -tags=integration ./tests/...` (outside `cmd/` and `internal/`).
* Memory growth over hours (soak): not attempted.

## Appendix: probe files (kept outside the worktree, not committed)

`internal/ai/zz_review_{fuzz,leak,redact,mutate}_test.go`, `internal/platform/httpserver/zz_review_{leak,probepanic}_test.go`,
`internal/platform/postgres/zz_review_admission_test.go`, `internal/platform/access/zz_review_bodybudget_test.go`,
`internal/{metadata,compiler,runtime/workflow,runtime/rules}/zz_review_{fuzz,mutate}_test.go`. Each is a plain Go test or native
fuzz target using only the package's own exported or test helpers; the report's numbers come from
`/tmp/claude-0/-home-user-Framwork/5369ba92-9151-56e8-b81f-813fd80ed24e/scratchpad/rb/*.log`.

## Appendix: repro code

All three are plain `go test` files for the packages named; copy them into the package directory, run, and delete. They use no
secret-shaped data.

RB-02, `internal/ai/zz_review_repro_test.go` (`gt test -count=1 -run '^TestReproAbandonedCapIsSoft$' -v ./internal/ai`; observed
`abandoned=100 reached the adapter=100 (documented bound: 16)` and the test fails):

```go
package ai

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type zzFn func(context.Context, ProviderRequest) (ProviderResponse, error)

func (f zzFn) Complete(ctx context.Context, r ProviderRequest) (ProviderResponse, error) { return f(ctx, r) }

func TestReproAbandonedCapIsSoft(t *testing.T) {
	release := make(chan struct{})
	var reached atomic.Int64
	gw := NewGateway(zzFn(func(context.Context, ProviderRequest) (ProviderResponse, error) {
		reached.Add(1)
		<-release // an adapter that ignores its context
		return ProviderResponse{}, errors.New("released")
	}), Options{})
	bundle, err := gw.BuildContext(ContextInput{Task: TaskRequirementCapture, Request: "Orders above 500 USD need admin approval.", Draft: fixtureDoc(t, exWorkflow), DraftRevision: 1, DraftHash: testDraftHash})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = gw.RunRequirements(context.Background(), RequirementInput{Context: bundle, Budget: Budget{MaxAttempts: 1, MaxOutputTokens: 4096, WallClock: 100 * time.Millisecond, TokensRemaining: 1_000_000}})
		}()
	}
	wg.Wait()
	got := gw.(*gateway).abandoned.Load()
	t.Logf("abandoned=%d reached the adapter=%d (documented bound: %d)", got, reached.Load(), maxAbandonedCalls)
	close(release)
	if got > maxAbandonedCalls {
		t.Errorf("%d abandoned calls at once, bound is %d", got, maxAbandonedCalls)
	}
}
```

RB-01, `internal/platform/httpserver/zz_review_readyz_test.go` (`gt test -count=1 -run 'TestReproReadyz' -v ./internal/platform/httpserver`;
observed `200=100 429=0 ... peak=1200`; the second test prints `process error=exit status 2 ... panic: assignment to entry in nil map`
for the probe and `ZZ-STATUS 500` with no process error for the handler):

```go
package httpserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReproReadyzAmplification(t *testing.T) {
	var inFlight, peak atomic.Int64
	probes := map[string]Probe{}
	for i := 0; i < 12; i++ {
		probes[fmt.Sprintf("p%02d", i)] = func(ctx context.Context) error {
			n := inFlight.Add(1)
			for {
				m := peak.Load()
				if n <= m || peak.CompareAndSwap(m, n) {
					break
				}
			}
			defer inFlight.Add(-1)
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		}
	}
	h := Handler(Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), ProbeTimeout: 2 * time.Second, Probes: probes})
	srv := New("", h)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln)
	defer srv.Close()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	var wg sync.WaitGroup
	var ok, limited atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get("http://" + ln.Addr().String() + "/readyz")
			if err != nil {
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			if resp.StatusCode == 200 {
				ok.Add(1)
			} else if resp.StatusCode == 429 {
				limited.Add(1)
			}
		}()
	}
	wg.Wait()
	t.Logf("200=%d 429=%d peak=%d", ok.Load(), limited.Load(), peak.Load())
}

func TestReproPanicHelper(t *testing.T) {
	mode := os.Getenv("ZZ_PANIC_MODE")
	if mode == "" {
		t.Skip("helper")
	}
	opts := Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), ProbeTimeout: time.Second,
		Probes: map[string]Probe{"p": func(context.Context) error {
			if mode == "probe" {
				var m map[string]int
				m["x"] = 1
			}
			return nil
		}},
		Register: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /v1/panic", func(http.ResponseWriter, *http.Request) { var p *int; _ = *p })
		}}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go New("", Handler(opts)).Serve(ln)
	path := "/readyz"
	if mode == "handler" {
		path = "/v1/panic"
	}
	if resp, err := http.Get("http://" + ln.Addr().String() + path); err == nil {
		os.Stdout.WriteString("ZZ-STATUS " + resp.Status + "\n")
	}
}

func TestReproPanicInProbeVersusHandler(t *testing.T) {
	for _, mode := range []string{"handler", "probe"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReproPanicHelper$", "-test.v")
		cmd.Env = append(os.Environ(), "ZZ_PANIC_MODE="+mode)
		out, err := cmd.CombinedOutput()
		line := ""
		for _, l := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(l, "panic:") || strings.HasPrefix(l, "ZZ-") {
				line = l
				break
			}
		}
		t.Logf("mode=%s: process error=%v; %q", mode, err, line)
	}
}
```
