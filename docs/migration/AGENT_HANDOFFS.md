# Agent handoffs

## FROM remediation TO milestone — 20 October 2026 (Asia/Kolkata)

Subject: slice A merged-tree re-baseline at `552fa9e` (complete; combined gate FAIL).

The authorized fast-forward to `remediation/m9r-foundation` is pushed. No
application/shared integration code has been changed. We ran the full
gate on a fresh owned stack, including sign-in ACLs, retained-reader checks,
actual Redis restart, real 600-second outage, all-eight capacity targets and driver
panic. Final evidence is recorded in
[M9R_MERGED_REBASELINE_REPORT.md](M9R_MERGED_REBASELINE_REPORT.md).

Isolation observation: the Redis restart fixture selects the project through
`COMPOSE_PROJECT_NAME`, which permits an owned-stack run. Reference PostgreSQL
probes instead hard-code the default container name; slice A substitutes only
those exact read-only inspect/identity operations in an ignored harness while
retaining the actual connected-cluster/profile checks. No product test is edited.

Provisioning observation: restored 0700 public PostgreSQL init script is not
readable inside the container. A healthy restart can conceal absent initial
roles. Only this new stack was repaired, using the unchanged script through
stdin and readable isolated public configuration copies. Shared/default data,
root environment and ACL bindings are preserved. This is setup evidence, not a
milestone implementation defect.

Independent review: NOT RUN by the user's explicit scheduling instruction. We
do not certify M14 or the merge independently. R6 is held for the milestone
agent; slice A does not duplicate it. B/C and R7 implementation are not started.

Future migration-numbering concern (static evidence, no new migration attempted):
`cmd/migrate/main.go:38` creates a Goose provider without
`WithAllowOutofOrder(true)`; the foundation fixture likewise uses the default.
Goose v3.28.0 documents rejection of newly discovered migrations below the
already applied maximum. This stack is now at 89, while the instruction reserves
58–79 for remediation. Before the first such migration, agree on a governed
ordering strategy or revised reservation; do not renumber pushed migrations or
silently enable unrestricted out-of-order execution. Fresh application of the
existing 67 migrations passes; that is different from subsequently adding 58.
This is not an executed future-upgrade failure or a change request applied in A.
A simple allow-out-of-order switch would not by itself establish equivalent
schema state: a fresh install would apply 58 before 80–89, while an upgraded
installation would apply it after 89. Any overlapping function definitions or
grants could end differently. Future numbering/order design needs both paths
verified; this baseline does not exercise a hypothetical migration 58.

Final slice A evidence at product tree `552fa9e`:

- PASS: check; Go race unit command (27 packages, 7 cached), 9 Node script tests,
  126 frontend tests; build; full integration 439 groups in 1366.644s Go,
  zero failures/skips; browsers 11 + 7; all 9 discovered fuzz targets;
  current pinned API abuse test; actual 600s outage and recovery.
- Reference-v2: seven of eight targets PASS. Aggregate target **FAIL**:
  mapped-current filtered_money p95 755.156ms; mapped-retained sum_money
  822.729ms and filtered_money 755.607ms versus 750ms. All 24 cases completed,
  results correct/zero unexpected request errors; worst SQL p95 309.775ms
  versus 500ms. No budget relaxation, replacement rerun or product fix.
- Unique conflicts pass narrowly: p95 479.989ms versus 500ms. Ordinary writes
  pass with 7,968 writes/p95 301.897ms; unique writes 6,463/p95 350.588ms.
- Driver-panic diagnostic **FAIL**, Go 6.857s: supervised rollback/pool recovery
  did not finish. Known IR-R3d-01 remains unresolved.
- These data do not prove that merging caused a regression: R7b did not rerun
  the six targets other than numeric reads and ordinary writes, and no same-host
  pre-merge control ran.
  Compared with R7a, several latencies are higher; no neutrality claim is made.
- Default-stack containers/environment/ACL and retained artifact are unchanged.
  No SHARED file or application code is edited. Independent review NOT RUN.

The combined gate is **FAIL**, M9R remains **PARTIAL**, and the source remains
preview. Preserve these failures when merging the documentation checkpoint.
Slice B (23-file merge inspection), C (plan/ledger reconciliation and proposed
agent instructions), and user-confirmed R7 ordering are still pending. R6 and
M10+ feature work remain with the milestone agent; no production certification.
