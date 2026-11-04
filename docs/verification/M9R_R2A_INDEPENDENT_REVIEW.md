# M9R R2a independent review

Date: 2026-10-13. Reviewer: independent Codex review agent.

**Verdict: PASS for the pure R2a tranche at `3c18ae4`.** One confirmed MEDIUM
correctness finding at `38af753` was fixed and independently reverified. No
confirmed unresolved BLOCKER, HIGH or MEDIUM finding remains in this bounded
review. This does not accept dataset adoption, writer fences, resumable database
migration, activation or the complete R2 gate.

## Scope and contract review

Read AGENTS.md, the two-agent protocol, ADR026, `internal/compiler/storage.go`
and its tests, `internal/runtime/data/evolution.go` and its tests, retained value
validation and metadata quotas. The stable reviewed commit is
`38af7530f75fd0e5a45f52f9a341b3ff0a2182d2`. Implementation and repository tests
were not edited. The corrective commit reviewed and tested afterward is
`3c18ae44958f06b50486662d63329c76c1abb6fd`. Independent fixtures were supplied through a temporary Go
overlay under `/tmp/r2a-review`; no database, services, credentials or migration
state were changed.

Storage identity separates typed field/layout facts from execution and UI
identity. Evolution is explicit and bound to both immutable programs; field
loss, overwrite/swaps, unsupported currency/reference/enum changes and arbitrary
conversions are rejected. Artifact bytes and decoded views are defensive copies.
Restore recomputes the closed plan and requires byte equality, so a caller cannot
authorize changed operations by supplying a matching hash alone. Typed backfill
validation deliberately belongs to the data executor and is required even for
empty datasets. Numeric transformations use exact values and reject fractional
or out-of-range narrowing rather than round.

ADR026 describes later dataset identity, live ACL/tombstone readers, workflow
pins, event lineage, gates and resumable copy/cutover/recovery. Those are design
requirements, not capabilities supplied by these pure functions. The functions
perform no SQL, DDL, adoption, checkpointing, reference existence checks or
activation. `TransformRecord` explicitly requires later reference/ACL/manifest
verification before its output can become active. These limits are honest.

## Confirmed finding

**MEDIUM, CLOSED — an accepted evolution artifact could be impossible to restore.**

`internal/compiler/storage.go:248` marshals an artifact and returns a valid plan
without checking its total byte size. `RestoreEvolution` at line269 rejects any
artifact larger than `2<<20`. Per-backfill and instruction-count bounds do not
establish that shared bound.

Executed fixture: 34 entities, eight new optional string fields per entity,
272 explicit MISSING backfills. Each backfill is a valid 4,000-byte string of
double-quote characters, whose JSON representation escapes those characters.
Both programs satisfy metadata quotas. Each proposed entity value map, including
a safety allowance for the core ID during validation, is below64KiB. Planning
returns a valid **2,241,454-byte** artifact. Restoring its exact bytes with its
own hash and original base/target returns `ErrEvolution`.

Impact: persistence of an accepted plan cannot guarantee later recovery. This is
a pure contract failure, not evidence of a tenant escape or deployed migration
failure. Use one shared artifact budget in planning and restore; reject oversize
output before returning a valid plan. Preserve a regression proving that every
accepted bounded fixture restores, or is explicitly rejected at planning.

Commit `3c18ae4` adds the shared `MaxEvolutionBytes = 2 << 20` constant and checks
canonical output size before minting the plan, using the same bound at restore.
Its repository regression uses the reproduced shape. Independent re-verification
rejected the oversized fixture before minting and restored the bounded30,094-byte
artifact with original programs/hash. The initial failing overlay source is
preserved at `/tmp/r2a-review/compiler_pre_fix.txt`; the updated overlay expects
the corrected rejection. These are temporary session artifacts, not committed
tests or recovery documentation.

## Independently executed commands

```sh
./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/runtime/data
```

**PASS:** compiler11.849s and data2.056s. This executed repository tests for
storage identity, explicit rename/conversion/backfill planning, incompatible
contracts, canonical restoration, immutability, exact values, invalid defaults,
wrong bindings, corrupt source values and missing versus explicit-null semantics.

```sh
./scripts/go.sh test -race -count=1 -overlay /tmp/r2a-review/overlay.json -run '^TestIndependentR2A' -v ./internal/compiler ./internal/runtime/data
```

**FAIL overall:** compiler1.385s because
`TestIndependentR2ALargeValidPlanRoundTrips` reproduced the finding above.
`TestIndependentR2AArtifactBindingAndIntentIsolation` passed: caller mutation of
intent/artifact copies leaves the plan unchanged; another base does not match;
hash-consistent forged base/schema/version/operations and malformed, duplicate,
trailing or NUL-containing/null defaults are rejected.

**PASS for data:**1.347s;
`TestIndependentR2AExactConversionAndBackfillIsolation` checked integer widening
and inverse narrowing against an independent rational oracle at positive and
negative safe-integer boundaries. It rejected fractional decimal narrowing,
out-of-range values, scientific notation, NaN, float input, invalid integer
syntax and an unknown entity.

At corrected stable `3c18ae4`, the identical overlay command was rerun with its
large-plan assertion updated to require planning rejection: **PASS**, compiler
2.822s and data1.496s. All three independent tests passed; the other binding,
malformed-default, immutability and precision probes were unchanged.

```sh
./scripts/go.sh test -race -count=1 -run '^(TestStorage|TestEvolution)' -v ./internal/compiler ./internal/runtime/data
```

**PASS at `3c18ae4`:** compiler3.893s and data2.017s, seven roots including the new
repository artifact-budget regression and all storage/evolution contract tests.
No extra database or service checks were needed for this pure corrective change.

One intervening source-read command failed when the exec-server transport
disconnected; a later `pwd` succeeded and testing resumed. That interruption was
not an approval/security rejection or a test failure.

## Limits and handoff

The size finding is closed by executed evidence. No database adoption, writer
fences or resumable migration/activation are implemented by this tranche.
No database adoption/migration/cutover rehearsal, concurrent writer/reference
test, worker pin or event lineage migration, production/OS-isolation acceptance,
full repository suite or remote CI was run for this pure tranche. No performance
benchmark was requested or run. R2 remains incomplete independently of the
artifact-size finding, and earlier R3a performance status is unchanged.
