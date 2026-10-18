# ADR045 — Cache only immutable embedded validation schemas

18 October 2026 (Asia/Kolkata). Status: accepted for implementation after
independent source/library design review; executable review remains required.
No policy/source/program/result cache is added.

The R3c bound-dataset write gate failed p95 690.646ms against the unchanged 500ms
target. Binding restore constructs all five embedded JSON-Schema validators on
every request. An isolated 100-sample diagnostic measured construction at
25.476ms per call versus 0.583ms for validation with an existing validator. The
legacy write floor uses an unbound preview and does not exercise this cost.

Compile the fixed, binary-embedded schema matrix once using sync.OnceValues.
Return a fresh Validator handle each time, sharing only private immutable schema
graphs. The library creates per-validation state; verify concurrent source/error
isolation with the race detector. External schema loading remains disabled.
Failed construction stays a fail-closed error; a changed schema requires a new
binary/process rather than a runtime invalidation protocol.

Never cache tenant membership, roles, grants, DataScope, authorization decisions,
customer metadata, compiler output, record data or database catalog checks under
this change. All live revocation and drift checks remain. Immutable artifacts
and source-kit bytes/hashes are unchanged by constructing their schemas once.

Rerun the same bound 32-client/60s workload and unmodified latency/lock budgets,
plus concurrent validation and retained old/new compile/restore tests. Preserve
the original failed capacity evidence and separate the initial-receipt fixture
correction from the actual latency fix. This does not close broader R7 caching,
admission, audit, worker timeout or connection diagnostics.
