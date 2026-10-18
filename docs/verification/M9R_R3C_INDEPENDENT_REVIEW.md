# Independent R3c integrity review

18 October 2026 (Asia/Kolkata). **Independent bounded-source review: PASS.**
Overall R3c completion still requires the final regression gates. Source is
`d15be92` on `remediation/m9r-foundation`; initial source reviewed was `1f8ab79`.
The reviewer is a separate agent under the two-agent protocol. This is technical
review, not human business acceptance or production deployment approval. M9R
remains PARTIAL; M10 and production activation are outside this review.

## Stable window and scope

The implementation agent paused source edits, builds and database tests while
this reviewer added only `tests/integration/m9r_unique_review_test.go` and this
report. The implementation report could receive documentation-only updates.
No production Go/TypeScript source or applied SQL migration was edited by the
reviewer. Every database group provisions an owned local disposable database and
checks effective driver/database/role targets before probes. No customer data,
secrets, raw driver details, canonical claim values or tokens are reported.

The review covers opt-in metadata/IR/compiler1.4, immutable restore and old
readers, physical-layout separation, SQL52 claims/lifecycle/permits, live Go/SQL
scope authority, API routing and error translation, compatible dataset bindings,
shadow evolution fences, worker privileges and fixed embedded-schema caching.
Migration52 SHA256:
`26ff8244599eec2401df5d699d8d2b2cef949671bf2ae0c0ab261246c9492591`.
It is already applied in disposable fixtures and remains frozen.

## Finding and correction

**MEDIUM — generic23505 mapping broke concurrent same-key reconciliation.**
At `1f8ab79`, preview handling translated every unique violation into a409
request rejection, swallowing the narrowly retryable
`runtime.command_receipts/command_receipts_pkey` collision recognized by
`postgres.KnownAbort`. An existing committed receipt could therefore fail to
replay to a concurrent caller, despite its mutation having rolled back safely.

This was identified by source review, then actually reproduced against an
ignored `git archive 1f8ab79` snapshot with the same independent test. Eight
simultaneous identical command keys produced409: **FAIL2.932s**. The reviewer did
not switch or rewrite the working checkout. At `d15be92`, only
`runtime.unique_claims/unique_claim_value` maps to the new business conflict;
receipt violations retain their original known-abort retry path. The identical
test passes: eight200 responses agree on one record, with one receipt, business
audit entry and outbox event. **Finding closed by executable regression.**

No other production-source or migration finding has been confirmed in the
reviewed scope. This statement does not imply the remaining M9R review findings
or later production milestones are resolved.

## Executable evidence

The final functional command was:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-legacy-api M9R_LEGACY_API_SHA256=bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RUniqueReview' -v ./tests/integration
```

**PASS 20.883s: 12 groups, no skips.** Ignored local output is
`.local/reports/r3c-independent-final.log`. The checksum-pinned retained API was
actually launched with app-only credentials, a fixture public JWKS and owned
loopback port. It is the pre-uniqueness binary built from `197fe85`.

The independent groups establish:

- Populated raw app writes claiming fresh or duplicate non-null keys both fail
  with42501 before conflict lookup. Unrelated raw updates preserve source/claim
  versions. A permit for another canonical key fails. App/worker/dispatcher
  credentials cannot read private claims; worker/dispatcher cannot mint permits.
- A real successful build COMMIT response is dropped by the protocol proxy.
  The caller gets503; GET observes the committed checkpoint and an exact replay
  does not repeat effects. This is not a synthetic returned error.
- A failed duplicate build, exact abort and active compatible non-unique binding
  permit authorized repair of the same three physical records. Rebinding the
  unique source creates generation2, verifies coverage, rejects repaired-key
  duplicates and preserves immutable abort history and existing records.
- Eight concurrent identical command keys commit one record/receipt/audit/outbox
  fact and replay that identity to every caller.
- Live stored Defined-role ALL scopes permit a key claim. OWNER scopes deny both
  fresh and duplicate probes identically while nullable writes remain possible.
  Restoring ALL grants takes effect; explicit field DENY and inactive heads
  revoke access despite a token containing forged owner/tenant claims.
- Registration fences an established older repeatable-read source writer with
 40001. A builder holds real source locks while an authorized DELETE is observed
  blocked in `pg_stat_activity`; release allows deletion and leaves18 records
  and18 claims, with no orphan. The500ms lock budget is unchanged.
- A READY installed constraint blocks whole-dataset shadow evolution adding an
  unrelated entity even after the active compatible1.0 source omits Unique.
  Rejection leaves the dataset OPEN, binding unchanged, all five records present
  and no evolution row. Current code still enforces duplicate/fresh claims over
  that compatible non-unique source.
- The actual retained API reads the populated compatible1.0 binding and performs
  unrelated PATCH, nullable CREATE, key clear and DELETE. Fresh and duplicate
  non-null claims both fail identically. Source/claim version coverage remains
  correct and rejected creates leave no source rows.
- Both tenants have real READY registries and claims. Tenant context forgery
  cannot expose the other registry. A worker column SELECT grant is detected by
  `worker_grants_safe`; restrictive forced RLS still returns no populated claim.
  Disabled claim trigger, private-helper EXECUTE grant and weakened worker policy
  each fail readiness, and restoration repairs readiness.
- Unique/search contenders race for the final shared slot after seven governed
  indexes: exactly one succeeds and the total remains eight. Separate coverage
  corruption fails verification without advancing the checkpoint; correcting the
  owned fixture permits verified completion.
- UTC key equality, pre-epoch microsecond rounding, year9999 carry and reference
  keys remain invariant across three session time zones and changed DateStyle.
  Invalid native integer spelling, out-of-range timestamp, invalid civil date,
  wrong currency, undeclared enum and uppercase reference are rejected.
- A real permit transaction retains authority locks through mutation and commit.
  Concurrent role revocation is observed blocked until release, then commits.
  Subsequent SQL permit issuance fails42501.

The initial four groups separately passed7.772s. The next exploratory run passed
three more groups but failed the retained-binary assertion: the test incorrectly
expected403. The old handler predates the new42501 translation and returns its
generic503 for both fresh and duplicate claims. The test expectation was corrected;
no production source was changed for this limitation. Its failure remains in
`.local/reports/r3c-independent-second.log`.

The pre-fix executable reproduction command was:

```sh
node scripts/with-env.mjs ./scripts/go.sh -C /workspace/Framwork/.local/reports/r3c-prefx-review-src test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RUniqueReviewSameCommandKeyPreservesReceiptRetry$' -v ./tests/integration
```

That ignored snapshot contains the original archived source plus this test file;
it contains no copied environment file, private keys or Git checkout.

`./scripts/go.sh test -race ./internal/metadata ./internal/compiler ./internal/runtime/data`
returned PASS using Go's test cache; it is not claimed as a fresh independent
unit execution. The implementation agent still owns the full final unit gate.

`make test-unique-performance` independently **PASS 200.046s** against the same
stable source. Its actual command is:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RUnique(Constraint|Write)PerformanceFloor$' -v ./tests/integration
```

The reference was pinned PostgreSQL18.6, four CPUs,8GiB and disabled swap. It
retained the2s statement/500ms lock budgets and500ms write p95 threshold.

| Independently executed case | Actual result |
| --- | --- |
| Hot100k source rows, nine other tenants with10k each, average1509-byte records | PASS:100k copied and independently verified through203 checkpoints; build14.503856s |
| Fresh key CREATE,100 measured requests after warmup,10 clients | PASS:p95 144.937ms,p99 171.318ms,zero unexpected errors |
| Expected duplicate CREATE,100 measured requests after warmup,10 clients | PASS:p95 68.700ms,p99 111.716ms,100 expected conflicts,zero unexpected errors |
| Distinct record updates changing the unique key,32 clients/60.172s | PASS:10,365 successful writes,p95 215.076ms,p99 237.536ms,172.26 writes/s,zero visible errors |

Capacity output is retained in ignored
`.local/reports/r3c-independent-capacity.log`. These are local reference
measurements, not a production SLA or arbitrary-workload capacity certificate.

The complete12-group inventory is `RawClaimsAndRoleIsolation`,
`ActualUnknownCommitReconciliation`, `AbortRepairCompatibleBindingAndEvidence`,
`SameCommandKeyPreservesReceiptRetry`, `LiveDefinedRoleScopesAndRevocation`,
`RegistrationFencesOldSnapshotAndBuildDeleteOverlap`,
`InstalledHeadFencesUnrelatedEntityEvolution`,
`RetainedBinaryReadPatchClearDeleteAndKeyDenial`,
`PopulatedTenantWorkerAndReadinessDrift`,
`CrossFamilyQuotaRaceAndCoverageCorruption`,
`CanonicalBoundariesAndSessionInvariance`, and
`PermitLocksLiveAuthorityThroughCommit`, each prefixed `TestM9RUniqueReview`.

The execution transport disconnected after these completed runs. An optional
13th probe of aborting after a committed1000-row partial copy was not added or
executed; it is NOT RUN, not additional PASS evidence. An optional extension
calling SQL permit issuance directly under a live OWNER scope was also not
added/executed; existing tests cover API OWNER denial and SQL inactive-head
denial. Reconnection confirmed the same checkout and preserved files. No
additional SQL/source changes were made during recovery.

## Limits and handoff

This review does not certify arbitrary unbounded queries, imports, production
rollout, external tool effects, R6 acceptance authority or remaining R7 operations.
No cryptographic claim is made that shared app credentials identify binary age.
Workers have an actual separate SQL grant boundary; legacy write paths omit
permits. The old API's503 for newly denied unique-key claims is a known operator
compatibility limitation, not a value-existence distinction.

Workflow start retains its existing read/snapshot behavior before declared
integrity becomes READY. It does not attest readiness or bypass mutation triggers.
There is no uniqueness administration UI, READY-constraint removal or
constraint-preserving shadow evolution. Recovery preserves records and evidence,
uses exact abort/generation checkpoints and a compatible active repair binding;
no destructive down or silent rebuild is authorized.

Final full `make check test build`, both browser suites and retained query/write
gates are implementation-agent handoff checks, not yet independently claimed by
this report. Overall R3c completion must wait for those recorded results.
