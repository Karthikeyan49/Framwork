# M9R R2b independent review

Date: 2026-10-13 UTC (14 October IST). Independent Codex reviewer.

**Verdict: PASS for bounded R2b at `145eaa4`.** Independent findings at `8d54977`
are fixed and reverified. No confirmed unresolved BLOCKER, HIGH or MEDIUM finding
remains in this equal-schema preview-binding review. No tenant escape or unsafe
deletion was demonstrated. This accepts no full R2, schema migration or production
gate; R4 and the current-host R3 HTTP target remain open.

## Scope

Reviewed stable `8d5497757f07d42a1f98de4fbfd0550c1ff931df`: committed additive
migration28, DatasetStore, logical/physical Scope separation, namespace and cursor
changes, API admissions/execution, audit state, event causal lane, worker grant
guard and ADR026 staging. Migration28 was not edited. Implementation/tests stayed
read-only. This report is the reviewer's only repository edit. Independent Go
fixtures under `/tmp/r2b-review` used owned disposable databases through the
repository's driver/connected-target-verified helper; no shared service restart,
Redis reset or customer data mutation occurred.

Corrections were reviewed at `abd2192608fc18e8a0fad9a9a36f8fdde67c3fec` and final
`145eaa4645439e8f4531f3a592aa1fb24710f24c`. Applied migration28 remains unchanged;
forward29 adds the statement immutability guards.

The new identity maps generation1 preview bindings to an explicitly chosen
physical anchor. New EMPTY/binding admission uses the trusted server catalog;
historical ADOPT requires supported restored bytes/hash, immutable source and an
already-established exact namespace. Execution restores stored IR, verifies its
source and never substitutes current compilation. Only active bindings write;
old equal-schema readers use the same current physical records and ACLs. Tenant
keys, central policy, SQL DataScope and forced RLS remain in the path. Cursor
identity includes dataset/binding. Event sequence identity retains the anchor
while envelopes retain the executing release. No broad worker catalog access
was added.

## Findings before correction

**MEDIUM, CLOSED — owner TRUNCATE could erase immutable binding evidence.** Migration28
adds row UPDATE/DELETE immutability but no statement TRUNCATE guards on datasets
or dataset_bindings. In a populated, owned disposable database, a migration-owner
transaction successfully executed:

```sql
TRUNCATE runtime.datasets,runtime.dataset_bindings
```

The test immediately rolled back. This bypasses the promised immutable evidence
guard; it is not an application/worker permission escape. Runtime roles have no
TRUNCATE grant. Add statement guards with a forward migration; preserve applied28.

Migration29 now guards both tables before TRUNCATE. The corrected own probe
requires an immutability error, rather than accepting an unrelated FK error.
Readiness requires both new statement triggers as well as the original fences.

**MEDIUM reconciliation gap, CLOSED — missing reference projections were not completed.**
VerifyAdoption checks present links against typed record JSON and checks reference
target existence, but does not require/derive a link for every non-null reference.
An independent fixture removed the existing child's link while retaining its
valid JSON reference and target, then ADOPT/verify returned200, OPEN, verified_rows2.

Absent derived projections can also occur legitimately in retained V2/v8 data.
The result is not proof of unsafe deletion: current commands scan JSON references
and retain dataset serialization, and R4 is still blocked. The concern is making
the derived link set complete and hashing that reconciled set before future
projection-dependent behavior. Derive missing equal links atomically with the
verification checkpoint, reject unequal present links, and preserve record
JSON/IDs/versions. Do not treat legitimate legacy absence as invalid record data.

Corrected verification inserts only missing equal projections, rechecks their
exact destination and hashes the complete link facts. An independent mandatory
audit failure proved that derived INSERT and checkpoint both roll back. Successful
retry derives exactly one matching link, preserves original JSON and row version,
and repeated verification preserves checkpoint count/manifest.

## Executed evidence at8d54977

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RDataset' -v ./tests/integration
```

**PASS17.384s, seven roots:** source/catalog admission and continuity, stored IR,
populated A/B isolation and policy, bounded205-row verification/resume, raw and
stale-RR fencing, lost actual COMMIT replies for create/verify, causal sequence,
workspace/cursor isolation and audit-failure rollback.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r2b-review/overlay.json -run '^TestIndependentR2B' -v ./tests/integration
```

Initial invocation: **FAIL setup**, because the review overlay imported the wrong
jobs package path. Fixed only the temporary import to `internal/platform/jobs`.
Corrected invocation: **FAIL4.166s**, reproducing the two findings above.
`TestIndependentR2BNewCatalogWorkerColumnGrantDriftFailsClosed` passed: readiness
was valid before drift, column-only SELECT on each new catalog made jobs/event
readiness deny; spoofed valid Alice identity/tenant GUCs returned zero catalog
rows; revoking the added grant restored readiness. These are raw worker SQL
probes, distinct from trusted repository context setup.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r2b-review/overlay.json -run '^TestIndependentR2BInvalidPresentProjectionAndInputBudgetRollback$' -v ./tests/integration
```

First invocation: **FAIL7.142s**, because the review's undeclared-property fixture
lacked the trusted actor required by the existing update trigger. Unequal link
and257-share negatives already passed. Fixed only that temporary operator fixture
to bind the exact actor/tenant inside its transaction. Rerun: **PASS7.177s** for
all three cases. Unequal present projection, undeclared JSON property and257
shares return503, retain ADOPTING, and leave checkpoint count/manifest at zero.
No failed verification was reported as OPEN or committed partial progress.

## Independently executed corrected evidence

At stable `abd2192`, the same overlay command was rerun with the two corrected
assertions and expanded negative probes:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r2b-review/overlay.json -run '^TestIndependentR2B' -v ./tests/integration
```

**PASS13.312s, five roots:** owner TRUNCATE immutability; atomic missing-link
reconciliation/fault/replay; column-only worker catalog-grant drift; unequal
projection/unknown property/257-share rollback; and bound owner/field scopes plus
wrong original-source denial. The last probe admitted legitimate populated Alice
and Bob owner reads, returned no rows under field NONE, rejected Alice's write to
Bob's record, and refused a manually stored hash-valid, equal-schema IR whose
label/source disagreed with its immutable registry revision. The latter returned
503 without changing protected records. It did not trigger current compilation
as a substitute for the stored artifact.

The pre-fix review fixture is retained at `/tmp/r2b-review/pre_fix_probe.txt`;
updated assertions and added cases are in `probe_test.go` and `overlay.json`.
These are temporary session artifacts, not repository tests.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RDataset|^TestM9R(ReadinessWithGenericPreparedPlans|GenericGrantDriftFailsReadinessAndStillDeniesRawData|ExactCapabilityMaskedSourceAndTransactionLifetime|RepeatableReadCannotRestoreReplacedLease)$' -v ./tests/integration
```

**PASS43.711s at `abd2192`, fourteen roots:** all ten dataset roots, including
reference-heavy adaptive progress and new owner/runtime TRUNCATE tests, plus
exact masked capability/transaction lifetime, stale repeatable-read fencing,
grant-drift denial and API/worker/dispatcher generic readiness. These are selected
regressions, not an independently executed complete M8/M9 suite.

Source review initially questioned the first-record exception in the nominal256
verification-work budget using a hypothetical86-reference record. That is **not
a demonstrated current defect**: VerifyAdoption restores its witness, and
`compiler.Restore` rejects entities with more than32 fields. The current per-record
cost therefore cannot exceed97. The final commit also rejects cost>256 explicitly
before mutation, preventing dependence on that reader bound in future versions.
No unsupported large-field fixture was represented as an executed bypass.

After inspecting that three-line defensive guard, final stable verification ran:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r2b-review/overlay.json -run '^TestM9RDatasetReferenceHeavyAdoptionHasBoundedProgress$|^TestIndependentR2B(ReconcilesMissingReferenceProjectionAtomically|BoundScopesAndImmutableSourceMismatch)$' -v ./tests/integration
```

**PASS11.572s at `145eaa4`:** reference-heavy bounded/resumable progress, own
derived-link/audit-fault/replay and own field/owner/wrong-source probes. No broader
source change occurred after the fourteen-root run. Both review findings are
closed; no confirmed HIGH or BLOCKER remained in the reviewed boundary.

## Limits and handoff

The implementation is equal-schema, generation1 preview binding only. Pure
transformers are not wired to database migration. Incompatible shadows, optional
addition reader windows, multiple generations/new workflow pins, canonical
cross-generation ACL/tombstone identity, resumable schema copy/cutover and
production activation are NOT IMPLEMENTED here. Existing per-write anchor
serialization remains until R4. No performance benchmark or complete repository/
remote-CI/browser/production/process-isolation suite was executed by this reviewer
for this tranche. Earlier R3a PARTIAL status is unchanged.

Binding witnesses must satisfy the current supported immutable IR reader,
including its32-field entity limit and bounded authoring-request size. This is
not a promise to adopt every schema-valid historical document. Verification also
retains its record/share/link input budgets. The work budget bounds verification
units, not a measured end-to-end latency SLA. Current owner/admin management is
not a replacement for the later policy-administration gate.

All independent test processes finished and owned fixture cleanup ran. CPU/DB
test ownership was released. No shared service was stopped or reset.
