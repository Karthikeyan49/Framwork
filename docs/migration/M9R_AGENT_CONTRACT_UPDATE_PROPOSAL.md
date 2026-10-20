# Proposed agent-contract updates after integration

20 October 2026 (Asia/Kolkata). **PROPOSED ONLY — NOT APPLIED.**
This document is review material for slice C, not an effective replacement for
AGENTS.md or CLAUDE.md. The user's current migration instruction governs this
session; these files remain byte-identical to the starting checkpoint a1d8b00.
No command in C applies the patch.

## Exact baseline

| File | SHA-256 before C (must still match before a future apply) |
|---|---|
| AGENTS.md | `d449d4d6cbbe4c56e1e8798c3f84695967c7fcff5be2d6c35cab78f7449e71f2` |
| CLAUDE.md | `a947da394e14391db7080058a0f16b1947670befdc021705d7e6fedb3e938c4f` |

If either file changes, re-read it and adapt the proposal rather than overwriting
new rules. Applying this proposal belongs to a later explicit user task. The
existing security/module rules outside these hunks are retained.

## Proposed corrections

- Recognize M10–M14 as existing preview while keeping this agent in remediation;
  keep M9R PARTIAL/combined gate FAIL and identify the live evidence.
- Replace the obsolete noop-only/tool-absent statement; distinguish preview
  activation from the missing production data plane and verifier credential.
- Preserve version meanings, retained artifact limits, shared transaction proofs,
  latest SQL guards, worker allowlist and migration-order decision.
- Preserve branch/ownership restrictions, temporary independent-review deferral,
  explicit D-order confirmation and request-only/deferred product/deployment work.

These are documentation corrections to the user's existing scope, not new
architecture decisions or production approval. R6 ownership and remaining
work are in the [plan](M9R_REMEDIATION_PLAN.md); current findings are in the
[ledger](M9R_FINDING_STATUS.md).

## Proposed patch (unapplied)

```diff
diff --git a/AGENTS.md b/AGENTS.md
index fd94ab4..90bb183 100644
--- a/AGENTS.md
+++ b/AGENTS.md
@@ -1,9 +1,13 @@
 # Coding-agent contract

-Current authorized work is M9R foundation remediation after the published
-M0–M9 review. Follow [the plan](docs/migration/M9R_REMEDIATION_PLAN.md) and
-[findings](docs/verification/M0_M9_REVIEW_VALIDATION.md). Preserve working code
-and data. Do not begin M10 automatically.
+Current authorized work is M9R foundation remediation on the merged preview.
+M10–M14 preview code exists; remediation remains this agent's lane. Follow the
+[current plan](docs/migration/M9R_REMEDIATION_PLAN.md),
+[live ledger](docs/migration/M9R_FINDING_STATUS.md) and
+[handoffs](docs/migration/AGENT_HANDOFFS.md), with the original
+[validated review](docs/verification/M0_M9_REVIEW_VALIDATION.md) as baseline.
+M9R remains PARTIAL and combined acceptance FAIL. Preserve working code/data;
+R6 and new M10+ feature work belong to the milestone agent, not this lane.

 ## Architecture authority

@@ -73,8 +77,10 @@ belongs to `internal/platform/app`.
   additional restrictive query-match policy. Search text is never authority.
   Preserve parser witnesses, entry/exit context cleanup, shared quotas, search
   field authorization and correlated source/target live-view point fetches.
-  ADR044 adds opt-in schema/IR/compiler1.4 unique fields. A declaration is not
-  enforcement: require READY claims and live full-scope authority before any new
+  ADR044 adds opt-in schema/IR/compiler1.4 unique fields; schema 1.5 is its tool
+  superset. B-02 compiler pairing and B-05 SQL unique registration remain open
+  in the merge review. A declaration is not enforcement: require READY claims
+  and live full-scope authority before any new
   non-null key. Preserve transaction-bound permits, exact attempt checkpoints,
   legacy denial and evolution fences. Map only the named claim constraint to409;
   receipt-key collisions must retain known-abort reconciliation. ADR045 shares
@@ -112,18 +118,59 @@ belongs to `internal/platform/app`.
   Exact attempts, delegated policy and atomic local effect/audit/receipt precede
   ACK. Preserve identity and evidence during dead-letter/replay.
   ADR062–063 separate5s publication/15m receipt budgets with20 backed-off attempts.
-  Event jobs require RC with no shared-snapshot certificate; API/workflow stay RR.
+  Event jobs require RC with no shared-snapshot certificate; API/workflow
+  default to RR. Only the exact sandbox-freeze route uses RC through the common
+  withRuntime port. RuntimeSnapshot certifies only its own RR executor.
   Migration57 refuses active old readers/binders; drain owned emitters, all workers
   and dispatchers. Preserve direct restrictive event RLS and inspect private helper
   structure before the owner-executed semantic readiness probe. No online cutover
   or raw-process liveness guarantee; see the R7b runbook.
 - ADR023: workflows pin IR/plan/input bytes. Preserve fenced leases, durable
   history, versioned signals, cancellation, recovery and compensation limits.
-  Only `local.noop` is registered. M9R corrects the historic worker boundary;
-  external tools and production activation remain later milestones. ADR036 adds
+  local.noop and governed operator-catalog tools exist in preview; R7 still
+  owns general revise/resubmit, renewal and approval semantics. M9R corrects
+  the historic worker boundary; M13 activation is a preview control-plane
+  pointer, without a production data plane. ADR036 adds
   opt-in metadata/IR1.3 and Workflow DSL2.1 tenant approval roles; preserve legacy
   readers and live revocation fences. Role references never grant capabilities.

+- ADR100–104: M10 operator catalog and per-tool idempotency/reconciliation;
+  M11 synthetic sandboxes/frozen evidence; M12 server-derived candidates/risk
+  and executable acceptance; M13 SQL approval/activation authority; M14 typed
+  AI draft/requirement proposals with human apply/confirm. Never duplicate R6.
+  The deterministic verifier runs in the API trust domain; a separate verifier
+  credential and production data plane remain missing. Real-model deployment
+  and independent M14/merge review are unverified. B-01 launcher settings remain
+  dropped until a verified fix lands. ADR100 A1 already derives tool approval
+  from the pinned plan and checks current-record freshness; extend that gate.
+
+## Integration coordination
+
+- Work only on remediation/m9r-foundation; fetch and merge integration before
+  each slice. Push only remediation. The milestone agent merges it into
+  integration. No rebase/reset/force-push, branch deletion, PR or main merge
+  in this task. Use file-based handoffs; name every SHARED edit.
+- Version meanings: schema/IR/compiler 1.3 approval roles, 1.4 unique, 1.5 tools;
+  workflow DSL 2.0 closed roles, 2.1 tenant roles, 2.2 pinned tools. Legacy 2.1
+  tool plans have a restore-only reader; full old tool IR is not thereby
+  compatible (B-03). Never relabel or recompile immutable pinned artifacts.
+  Coordinate a future 1.6 superset and update every metadata/compiler/restore,
+  frontend, AI, JSON-schema and SQL gate, including operation-specific gates.
+- Start SQL replacements from the latest merged definition and preserve all
+  guards. Record intentional message moves in m13DroppedMessages with reason.
+  Migration 00087's derived worker guard wins; add any legitimate new worker
+  grant to its latest allowlist in a new migration. Worker test effects belong
+  in m8_fixture/m9_fixture, outside control/runtime/iam/audit. Use UpTo(n) in
+  forward rehearsals rather than counting all remaining migrations.
+- Number reservations are remediation 00058–00079/ADR064–099 and milestone
+  00080–00099/ADR100–119. The applied maximum is 89: agree and verify both
+  fresh and upgraded ordering before adding a lower number. Do not silently
+  enable out-of-order execution, renumber pushed migrations or rebuild MAIN.
+- Use owned services and verify effective driver/connected targets before
+  fixture mutations. Preserve shared/default data and private configuration.
+  Current aggregate-capacity, worker-policy and driver-panic failures remain
+  in the A/B reports; passing reruns or docs changes do not close them.
+
 ## Delivery and verification

 Use the existing checkout; no worktree unless requested. Deliver small coherent
@@ -143,3 +190,10 @@ remaining gaps and recovery. Contract-only services and IR test labels are not
 business acceptance evidence. Fix and reverify review findings before declaring
 completion. Existing user authorization governs work; request a human decision
 only for a concrete consequential action outside that scope.
+
+Current user scheduling override: no independent agents until 22 October 2026,
+21:00 UTC; do not spawn or wait meanwhile. Record independent review NOT RUN,
+not waived or passed. Stop after each bounded slice. R7 D1–D7 implementation
+ordering needs the user's confirmation; wider R2/R3/R5 product features and
+real IdP/MFA/HTTPS verification remain request-only/deferred. This temporary
+instruction does not remove later independent-review or production gates.

diff --git a/CLAUDE.md b/CLAUDE.md
index 8612746..1ed343d 100644
--- a/CLAUDE.md
+++ b/CLAUDE.md
@@ -3,11 +3,19 @@
 Use [AGENTS.md](AGENTS.md) as the single coding-agent contract. Read its linked
 architecture, module ADRs and verification requirements before changing code.

-Current work is [M9R foundation remediation](docs/migration/M9R_REMEDIATION_PLAN.md),
-following the [validated review](docs/verification/M0_M9_REVIEW_VALIDATION.md).
+Current work is [M9R foundation remediation](docs/migration/M9R_REMEDIATION_PLAN.md)
+on the merged M10–M14 development/test preview, following the
+[validated review](docs/verification/M0_M9_REVIEW_VALIDATION.md),
+[live ledger](docs/migration/M9R_FINDING_STATUS.md) and
+[handoffs](docs/migration/AGENT_HANDOFFS.md).
 The [supersession map](docs/migration/V2_V3_SUPERSESSION_MAP.md) identifies replaced
 historical instructions and capabilities that remain contracts or previews.

 Preserve working code/data, deliver small independently reviewed changes, and
 record actual evidence and gaps. A milestone PASS does not certify production
-readiness. Do not continue M10 without the remediation gate and a new user task.
+readiness. M9R remains PARTIAL and combined acceptance FAIL. This agent builds
+remediation only; the milestone agent owns R6 and new M10+ work, including the
+production data plane and separate verifier credential. Do not duplicate them.
+Keep the existing preview code and open A/B findings. R7 implementation order
+requires user confirmation. Use only remediation/m9r-foundation; integration
+and main pushes, PRs and production exposure need separate authorization.
```
