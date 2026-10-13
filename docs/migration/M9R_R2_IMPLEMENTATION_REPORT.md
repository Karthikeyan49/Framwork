# M9R R2 implementation progress

Report date: 14 October 2026 (IST).

**R2 STATUS: PARTIAL.** The first tranche implements and independently verifies
immutable storage-schema/evolution artifacts and pure record transformations.
Dataset binding, database adoption, checkpointed migration, writer fencing and
activation are still pending. This is not production persistence or R2 completion.

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
| Dataset adoption / interruption / database cutover / final foundation regression | NOT RUN; implementation pending |

The independently reproduced budget defect is fixed in 3c18ae4 and has a permanent
regression test. Exact independent commands, failure and threat-model limits are
in [the review](../verification/M9R_R2A_INDEPENDENT_REVIEW.md). No new dependency,
database migration or customer-data mutation belongs to this tranche.

Recovery: retain original programs/artifacts. Failed transformations produce no
output and cannot activate a dataset. Rollback of application code does not alter
existing data. The forthcoming database executor must provide atomic checkpoints,
live ACLs/tombstones, reference reconciliation and explicit cutover; the pure
transformer does not provide these guarantees by itself.
