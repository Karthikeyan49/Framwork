# M9R publication and interruption checkpoint

17 October 2026 (UTC). **Overall M9R STATUS: PARTIAL.**

This checkpoint answers the request to push the current work and distinguish
completed remediation from pending work. It changes documentation only. It does
not certify production readiness or start M10.

## Repository recovery and publication boundary

The provided checkout was clean on branch `work` at `81b88d2`, an older M9
baseline. The remote remediation branch was independently read with
`git ls-remote` and contained `197fe85cb353833b8b616e2c5cdd0d3301e2c597`.
An explicit refspec fetch followed by a fast-forward restored the existing local
`remediation/m9r-foundation` branch to that published checkpoint. No source or
customer data was deleted and no history was rewritten.

The unfinished R3c uniqueness files described in the preceding session are absent
from this checkout. Searches of the workspace/temporary directories, Git stash,
reflog and unreachable objects did not recover them. The previously reported
local ADR commit `d8685da` is not available as a local Git object. The single
unreachable blob found is an older M8 report, not the missing work. No second
active agent holds a recoverable checkout in this environment.

Therefore the earlier R3c implementation and tests **cannot be described as
published or complete**. Reconstruct and reverify them from the retained design
notes below; do not assume a missing migration was never applied elsewhere.
The last recoverable published migration is 51. Inspect each target database's
migration history before choosing a new migration number or applying SQL. If a
target already has migration 52, recover its exact applied bytes/checksum and
forward-fix; never substitute newly written SQL under an applied number.

The commit containing this report publishes the interruption record and an
updated finding-ledger link. The final response records the actual pushed hash
and remote equality. `main` remains at `9da3f5f`; it is not merged or deployed.

## Completed and preserved in Git

These are bounded foundation capabilities. Their linked reports, rather than
this checkpoint, contain executed evidence, negative cases and limitations.

| Area | Preserved completion |
| --- | --- |
| R0 | Architecture supersession map, explicit capability status and concise agent contract. |
| R1 | Independently tested worker credential/resource confinement and dispatcher separation. |
| R2a–R2c | Stable preview dataset bindings, legacy adoption, immutable schema generations and checkpointed shadow evolution with reconciliation. Production active-release execution remains later work. |
| R3a/R3b | Statement-level membership evaluation and governed numeric, text/civil-date, enum/reference, datetime/boolean and selective full-text search indexes. Only the reported workloads are certified. |
| R4 | Known-abort retry boundaries, command receipts, unknown-COMMIT reconciliation, bounded FIFO admission and concurrent-write regression evidence. |
| R5a/R5b | Versioned tenant capability roles and live SQL worker-policy enforcement. |
| R5c | Governed record owner/team/share and team administration APIs, plus record/team administration UI. |
| R5d | Field masking, safe partial writes and strict pinned-input guards. |
| R5e | Custom tenant workflow approval roles, legacy IR compatibility and live signal-time authorization. |
| R5f | Bounded OIDC/PKCE browser sign-in and configured clock tolerance; external deployment remains unverified. |

Latest completed source is `29a61ac`, documented and previously pushed by
`197fe85`. The [search implementation report](M9R_R3B_SEARCH_IMPLEMENTATION_REPORT.md)
records the previous environment's executed full checks/build, 230 integration
tests, 96 frontend tests, nine application and seven sign-in browser cases, and
six capacity gates. Those suites were **not rerun during this publication-only
recovery**; this report does not convert historical results into a fresh run.

## Still pending

1. **R3c uniqueness:** reconstruct the unavailable work, review the design and
   migration history, then implement, independently attack and fully verify it.
2. **R3d aggregates/planning:** supported parameterized SQL aggregates with exact
   numeric semantics, independent fixture oracles and representative load gates.
3. **Remaining R5/product work:** role-definition administration UI and actual
   external identity-provider/client, gateway/logging and MFA deployment evidence.
   Search UI, ranking/languages and broad/common-term capacity are also separate.
4. **R6 authority:** owner-controlled requirements, executable independent
   acceptance, authenticated artifact-bound attestations, derived risk and exact
   immutable active-release execution. The plan assigns integration to M11–M14;
   contracts alone do not complete it.
5. **R7 operations:** outage-tolerant event delivery/replay/subscriptions, workflow
   renewal/cycles/history and approval freshness, rate limiting before expensive
   authentication/audit, audit lifecycle, measured safe caching, connection and
   worker-policy timeout diagnostics, and an executed backup/restore drill.

Production hosting, identity-provider details, residency/retention ownership and
the deployment DR tier remain external prerequisites. Local fixtures do not
verify them. M10 remains paused; external tools, AI, production activation and a
merge into `main` have not been performed.

## R3c reconstruction notes — design only, not verified source

The preceding session described an opt-in metadata/IR/compiler 1.4 contract with
`Field.Unique`, retaining 1.0–1.3 readers/hashes. Uniqueness must not change the
physical storage identity for otherwise compatible metadata. Unsupported types
and intrinsic-ID uniqueness are rejected; enforce declared constraint quotas in
both compiler and restored-plan validation.

Use fixed shared tenant-scoped constraint/claim tables, not tenant DDL. Define
NULL, empty string, Unicode, exact numeric/money and timestamp rounding semantics.
Database uniqueness arbitrates concurrent claims atomically with the business
mutation, receipt, audit and outbox. Hash collisions fail closed. Registration,
bounded backfill, independent verification, abort/restart generations and stale
checkpoint reconciliation must preserve source records and immutable evidence.

Prevent hidden-value existence leaks: non-null key claims require closed policy
proof plus independently validated live SQL authority over the full relevant
scope. Bind private permits to transaction/backend, actor, resource, generation
and value. GUCs are not authority. Workers and old binaries cannot acquire new
nonnull keys through a bypass. Preserve permitted old reads, unrelated patches,
NULL operations and deletions. Block whole-dataset evolution whenever installed
or declared constraints would be lost; do not silently copy away integrity.

Historical session notes report focused correctness checks and a 32-client write
case passing, but the combined uniqueness capacity command **failed** because a
fixture widened records with an UPDATE that did not advance `row_version`.
The source/logs are unavailable here, so these are resumption clues, not retained
verification evidence. Fix the fixture by creating representative-width rows at
initial INSERT, preserve the row-version trigger, and rerun. Independent source
review, the full project/browser regressions and final capacity gates were still
pending at interruption. Do not mark R3c PASS from those notes.

## Checks executed for this checkpoint

| Command / action | Result |
| --- | --- |
| `git status --short --branch`, `git branch -a`, `git log`, `git reflog`, `git stash list`, `git fsck --full --unreachable --no-reflogs` | PASS recovery inspection; missing R3c work not recovered. |
| `git ls-remote --heads origin main remediation/m9r-foundation` | PASS; published main/remediation boundaries confirmed above. |
| `git fetch origin refs/heads/remediation/m9r-foundation:refs/remotes/origin/remediation/m9r-foundation` | PASS. |
| `git merge-base --is-ancestor remediation/m9r-foundation origin/remediation/m9r-foundation` | PASS before fast-forward. |
| `git switch remediation/m9r-foundation` and `git merge --ff-only origin/remediation/m9r-foundation` | PASS; restored published implementation without rewriting history. |

Documentation validation and final Git publication results accompany this
checkpoint in the final response. No source/configuration/migrations were changed;
application, database, browser and performance tests are NOT RUN in this recovery.
Generated artifacts, dependencies, private keys and environment secrets are not
Git content. Preserve the separate remediation branch and forward-only recovery.
