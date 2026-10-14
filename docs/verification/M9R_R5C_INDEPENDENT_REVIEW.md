# M9R R5c-a independent review

Reviewer: separate agent context, no tracked edits, executed probes only in
temporary `zz_review_` files that were deleted (git status clean). Root and
reviewer did not edit or run tests concurrently against moving files.

## Round 1 — `11e8097`

No HIGH/MEDIUM. LOW findings:

- L1: recipient/team/fence 55000 errors mapped to retryable 409 `record_conflict`.
- L2: listing rejected records with >64 shares (evolution copy permits 256), so
  excess shares could not be found and revoked.
- L3: the SQL port had no actor check of its own (defense in depth).
- L4: ADR gaps (assign-only delegates, team need not contain owner, no-op transfer).

Verified: viewer/member/cross-tenant/collection denials; write-share escalation
blocked after ownership transfer; SECURITY DEFINER owner/search_path/ACL and
explicit tenant filters; canonical vs anchor models and mirror; retired/legacy
route 409; FROZEN/ADOPTING fences and lock order vs evolution; 8-way race
(1 success, 7×409, 0×5xx); rejection rollback and audit; route regressions.
Commands: targeted suites `^TestM9RRecordAccess|^TestM9RShadow|^TestM9RCapabilities|^TestM6RSQL|^TestM5`
PASS 78.4s; 4 probes PASS; unit and vet clean.

## Root fixes — `8f8067a`

Migration 42 (preserving applied 41) addresses L1/L3, rejects no-op transfers,
and — from a failing root regression written for L2 — lets `unshare` revoke a
departed user's share (previously the recipient membership check blocked it).
Listing returns ≤256 recipients with `shares_truncated`. ADR034 updated for L4.
Root `make check test build` PASS (integration 485.973s), `make test-e2e` 5 PASS.

## Round 2 — `8f8067a`

L1–L4 closed. Verified migration 42 preserves owner/prosecdef/proconfig/ACL and
readiness; a published custom `steward` role administers (200), is denied after
demotion (403) and works again after reassignment; inactive recipient grant 400
while unshare 200; 65 shares listed. Remaining LOW L5: `unshare` of a user with
no share returns 409 (documented, accepted). Commands: 5 probes PASS 10.6s;
targeted suites plus `^TestM9RWorker` PASS 96.1s; unit tests PASS.

R5c-a review STATUS: PASS (bounded scope; full R5 remains open)

# R5c-b team administration review

Round 1 at `856a4f1`: no authz bypass, tenant leak, definer or hierarchy
integrity defect. MEDIUM F1: suffix-based team mapping captured GET routes whose
last segment is `teams` (reproduced with a role named `teams`). MEDIUM F2: no
retained-membership budget; 1001 inactive rows made team detail 503
(reproduced). LOW F3: native policy version not bumped. LOW F4: missing closure,
stored-role and custom-role coverage; team version stored in a role field.
Verified validator matches Go/worker resolvers, tenant-fence serialization
under RR, witness semantics, readiness wiring and validator cost (~155 ms at
20k memberships).

Round 2 at `6b50eb7`: F1–F4 closed; full route sweep answered by own handlers;
budget boundary 1000/1001 and typed 409 for operator excess; port contract
preserved by migration 44. Targeted suites PASS (22 tests, 49.5 s).

R5c-b review STATUS: PASS (bounded scope; full R5 remains open)
