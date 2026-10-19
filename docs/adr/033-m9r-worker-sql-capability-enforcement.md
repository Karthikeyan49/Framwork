# ADR033: Live capabilities at the worker SQL boundary

Status: bounded R5b independently PASS at a5bba7b; full R5/M9R PARTIAL.
See the implementation/review reports for executed scope and remaining limits.

R5a's independent review proved that trusted Go denial left raw SQL able to read
its exact leased record after role/head/field revocation. Keep the authenticated
lease boundary, but distinguish identity/routing/denial evidence from business
access. Neither a GUC nor a supplied PASS flag establishes business permission.

A database-owned, no-argument decision port resolves the exact live lease's
actor, tenant role head and immutable body. It checks the workflow action gates,
entity/row/declared-field read grants and owner/team/share/condition scopes against
current logical record access and the pinned typed IR. Explicit DENY wins; unknown,
malformed or over-budget input fails closed. The evaluator uses fixed operators
and no generated SQL. It evaluates only the leased record, not each row in API
queries. Current snapshots/leases still apply; completed effects are not undone.

All worker workflow RLS, source ports and business security-definer functions
use this live decision. Event envelopes/receipts/effects use events.consume on
the exact envelope's app/entity/record; events contain identifiers, not record
field values. Identity and routing remain available for the exact lease when
business permission is denied, solely to diagnose/audit/reconcile the rejection.
Denial audit uses an exact-ticket predicate; this grants no business write.

The whole pinned workflow input set is one access unit: a denied field blocks
snapshot exposure and execution. If the plan includes any local activity,
workflows.activity must also be granted before execution. This conservative
whole-plan check avoids using raw state updates to bypass an activity gate.
It does not implement field masking or partial workflow inputs. The existing Go
adapter remains an additional check and can impose stricter execution budgets.

Migration 39 is additive in behavior and preserves tables, bindings, IR, records,
receipts and audit. It replaces private-port/policy functions; no customer backfill,
recompilation or job approval witness is fabricated. Forward fixes only. Quiesce
workers for an operational cutover and restart with the compatible binary. Role
recovery publishes another version; no need to delete/recreate a pending instance.

This closes one raw-credential enforcement boundary, not all R5 product gaps,
production readiness, external actions, acceptance authority or M10.

A conditional grant over a retired physical generation fails closed until there
is a verified mapping to current typed data. Ownership/shares use current logical
identity across generations. The immutable workflow snapshot is preserved; this
restriction suspends access rather than rewriting/recompiling an in-flight job.
An explicitly reviewed non-conditional grant can restore access to the same job.
The private source helper is SECURITY INVOKER with no runtime EXECUTE grant;
accidental helper grants cannot acquire schema-owner record privileges.

Independent review at d093e80 found an uppercase UUID condition accepted by SQL
and rejected by Go, plus a repeated-evaluator timeout on the bounded 96-receipt
synthetic scan with an admitted 63-node policy. Forward migration 40 preserves
applied 39, aligns UUID/reference constants with the existing lowercase contract,
and puts expensive policy evaluation in statement InitPlans. Per-row identity
and lease-clock checks remain in USING and WITH CHECK; this is not transaction
caching or a longer timeout. Separate SECURITY DEFINER business ports continue
to validate live policy directly. Full checks/browser tests and reviewer-run
existing regressions passed. Independent final new probes were not run after
a platform interruption; the review preserves that limit. The synthetic receipt
scan passed with a narrow margin and does not certify wider capacity.
