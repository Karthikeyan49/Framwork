# M9R R2 implementation progress

Report date: 14 October 2026 (IST).

**R2 STATUS: PARTIAL.** The first tranche implements and independently verifies
immutable storage-schema/evolution artifacts and pure record transformations.
R2b implements explicit equal-schema preview bindings, bounded adoption and writer
fencing; independent review is pending. Incompatible schema migration, multiple
data generations/new workflow pins and production activation remain pending.
This is not production persistence or R2 completion.

## R2a pure schema-plan tranche

Storage fingerprints include application/entity/field identity and typed storage
constraints, excluding labels, compiler/catalog versions and UI definitions.
Evolution artifacts bind exact base/target IR and storage hashes. Closed operations
include explicit rename, integer/decimal conversion, missing/null backfill and
optional additions. Destructive/ambiguous changes fail. Record conversion uses
the existing typed value validator and exact arithmetic; source maps/IR remain
unchanged. Typed defaults are checked even before processing an empty dataset.

Standalone IR remains unchanged. Restoring a stored evolution artifact derives
its expected operations from its original programs and instructions, compares
canonical bytes, and never recompiles or approves a release.

| Command actually executed | Result |
|---|---|
| `./scripts/go.sh fmt ./internal/compiler ./internal/runtime/data` | PASS |
| `./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/runtime/data` | PASS initially, 9.720/1.729 s; after independent correction, 12.914/2.093 s |
| `make check` | PASS: source checksums, Go format/vet/module verification, frontend lint/typecheck |
| `git diff --check` | PASS |
| Independent adversarial overlay at 38af753 | FAIL: a valid 2,241,454-byte plan was minted but could not be restored under the 2 MiB reader limit |
| Independent corrected overlay at 3c18ae4 | PASS: shared builder/reader budget rejects excessive artifacts before minting; bounded artifact round-trip, malformed instructions, forged artifacts, exact conversion oracle and immutable copies verified |
| Dataset adoption / interruption / database cutover / final foundation regression at the R2a checkpoint | NOT RUN then; R2b evidence follows below |

The independently reproduced budget defect is fixed in 3c18ae4 and has a permanent
regression test. Exact independent commands, failure and threat-model limits are
in [the review](../verification/M9R_R2A_INDEPENDENT_REVIEW.md). No new dependency,
database migration or customer-data mutation belongs to this tranche.

Recovery: retain original programs/artifacts. Failed transformations produce no
output and cannot activate a dataset. Rollback of application code does not alter
existing data. The forthcoming database executor must provide atomic checkpoints,
live ACLs/tombstones, reference reconciliation and explicit cutover; the pure
transformer does not provide these guarantees by itself.

## R2b preview binding implementation

Additive migration 28 adds tenant-scoped dataset/workspace identity, generation 1,
an explicit retained physical anchor, immutable stored IR bindings and a CAS write
pointer. No record copy/deletion or shared tenant DDL occurs. The authenticated
owner/admin must choose ADOPT or EMPTY and provide an exact supported witness
matching immutable registry source. Missing witnesses block; no Compile call occurs
in a binding's read/write path. Production routes remain disabled.

New EMPTY/binding admission compiles once under the trusted server catalog and
requires the exact artifact hash. Caller-supplied compiler/catalog flags cannot
replace this gate. Historical adoption relies on the exact established namespace
hash plus restored witness/source; old bindings never compile on execution.

ADOPT bumps the old anchor under its write barrier, seals writes, and validates
100 rows per checkpoint: values, identities/versions/timestamps/actors, references,
teams/shares/links and a rolling manifest. SQL limits share/link collections too;
excessive or corrupt input blocks opening. Source rows remain unchanged. Checkpoint
and identifying audit evidence commit together. Lost COMMIT replies reconcile the
retained workspace or checkpoint. Empty workspaces get distinct anchors.

Same-schema bindings preserve record IDs/versions and current physical ACLs;
active writes acquire the shared dataset gate. Stale/legacy writes deny. The SQL
trigger also denies raw bypass and a stale RR snapshot cannot miss adoption.
Old stored IR readers remain immutable. Cursor fingerprints include dataset and
binding. Event sequence keys retain the origin's earlier lineage while envelopes
and audit retain the executing revision/hash. Worker grants remain narrow and
the drift guard includes the new catalogs. Readiness checks the new RLS/owner/fence
contracts. Original React pages remain usable for unadopted scratch previews.

| Additional command actually executed | Result |
|---|---|
| `./scripts/go.sh fmt ./internal/runtime/data ./internal/platform/access ./internal/platform/postgres ./internal/policy ./internal/events` | PASS |
| `./scripts/go.sh test -race -count=1 ./internal/runtime/data ./internal/platform/access ./internal/platform/postgres ./internal/policy ./internal/events` | PASS; access/postgres have no unit test files |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RDataset' -v ./tests/integration` | Initial FAIL: operator team fixture lacked trusted actor; fixed fixture. A later compile FAIL: missing `database/sql` test import; corrected. Successive expanded suites PASS; latest seven roots PASS 19.451 s before final regression |
| `make check test-unit build` | PASS: Go vet/format/race units/import boundaries/build, source checksums, Node 5/frontend 28 tests, lint/typecheck/frontend build |
| `make migrate` | PASS: one additive local migration; owned disposable databases also run the full chain and repeat no-op |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM5\|^TestM6R\|^TestM9RDataset\|^TestM8ImmutableEnvelope\|^TestM9DurableSnapshot\|^TestM9R(GenericGrantDrift\|ExactCapability\|ReadinessWithGeneric)' -v ./tests/integration` | PASS 73.627 s; existing CRUD/typed queries/joins/CAS/DataScope, migration/read equivalence, release compatibility, event ordering, immutable workflow pins, worker capability/grant drift, and the seven dataset roots |
| `make test-e2e` | PASS: 4 browser tests, 15.1 s, real CRUD/stale recovery/pagination/read-only policy, credential redaction and mobile layout |
| Final `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RDataset' -v ./tests/integration` after admission hardening | PASS 16.295 s; includes caller-catalog rejection, trusted catalog upgrade and stored-IR execution unchanged |
| Independent R2b review / incompatible shadow conversion / final R1–R4 suite | NOT RUN at this checkpoint |

Current limits: binding accepts identical storage hashes only. Pure plans can
describe/validate transformations, but no database shadow migration executes them
yet. New workflow/data-pin combinations are rejected. The old per-write dataset
counter is still present until R4, so this change does not fix writer contention.
No production/approval authority is inferred from preview binding. No new major
dependency. Source/migration/tests/docs belong in Git; local credentials, owned
fixtures, logs, binaries and builds remain ignored.

Recovery: quiesce old writers before adopting; retain source and binding history.
Read ADOPTING checkpoint status and resume verification after a reviewed repair.
Do not roll an old binary back into adopted writes or restore broad worker grants.
A failed cutover transaction rolls back its pointer/audit; a committed equal-schema
pointer change can be reversed only through a new authorized CAS binding, retaining
intervening record mutations and events. No destructive cleanup is authorized.

## Independent R2b corrections

At 8d54977, independent raw operator testing found that `TRUNCATE` could erase
dataset/binding history despite row mutation guards (MEDIUM). The test rolled it
back in its own disposable database. Migration 29 adds statement guards to both
tables; migration 28 remains unchanged. Runtime readiness checks all five guards.
These prevent accidental operator truncation, not a malicious owner disabling
triggers or dropping tables. Runtime/worker principals have no such privileges.

The reviewer also reproduced an incomplete link reconciliation case: retained
V2 records can have valid JSON references without derived projections. This was
not demonstrated unsafe deletion under the current counter/JSON scan. Verification
now reconstructs only missing matching links, rejects unequal existing links and
hashes the complete derived references. Projection writes, checkpoint and audit
share the transaction; a later validation/audit failure rolls them all back.
Record JSON, IDs and versions remain unchanged. A 256-work-unit budget makes
reference-heavy batches smaller than 100 rows so progress remains bounded.

| Correction command executed | Result |
|---|---|
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RDataset' -v ./tests/integration` | PASS: nine roots 25.652 s, then ten roots 21.173 s with reference work budget; includes permanent legacy-link/audit rollback, owner/runtime TRUNCATE denial and 30-reference-field resume tests |
| Independent pre-fix projection/TRUNCATE overlay | FAIL as expected at 8d54977; full evidence in the forthcoming review report |
| Independent populated grant-drift and corruption/budget probes | PASS at 8d54977: column SELECT drift fails worker readiness and leaks no rows; unequal links, undeclared properties and excessive shares leave zero verification progress |
| Independent correction re-verification | Pending at this checkpoint |
