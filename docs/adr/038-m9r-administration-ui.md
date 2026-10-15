# ADR038 — Governed record and team administration UI

Status: accepted for implementation, 15 October 2026 IST. R5c UI only;
full R5/M9R remain PARTIAL. No M10, production activation or main merge.

## Decision

Keep ADR034/035 APIs as the authority. Add a workspace team panel and separate
ownership/sharing panels for a selected preview record. Do not infer capabilities
from role names or token claims. Read denial hides the corresponding data;
every mutation still receives live server authorization. No new grants, database
migrations, dependencies or IR formats are required. Independent integration
review exposed unused SQL bind parameters in the existing R5d masked-query
builder. Correct construction by emitting an entity-write predicate only when
a writable projected field consumes it, and by resolving a field scope before
binding an opaque action predicate. Preserve every authorization decision, SQL
mask, strict-input guard and opaque header budget; add no new authority.

Team administration covers create, rename/reparent, direct membership activation
and deactivation. Use the exact observed team version, or the exact observed
membership active state (null for an absent retained row). Generate a creation
UUID before dispatch. Teams remain non-deletable. Display the bounded hierarchy
and retained inactive memberships; server hierarchy and budget checks govern.

Ownership reads ordinary SQL-masked record headers, so an assign-only delegate
does not require share-list permission. Sharing uses its own authorized access
view, with an explicit truncated-list warning and manual-ID revocation. Ownership
transfer requires an explicit acknowledgement that access may be lost. Optional
workspace-member suggestions use their own authorized read; the 100-row response
is not a complete member directory. IDs remain usable when suggestions are denied
or incomplete. Team names are rendered as text; no HTML or executable metadata.

All clients validate paths, bounded response shapes, unique identifiers and exact
target/version witnesses. POST transport errors, 5xx and malformed successful
responses mean unknown outcome. There is no automatic POST retry. 4xx responses
are definite rejection. Conflict or revocation clears editable snapshots until
a fresh authorized read. Known success invalidates stale business editing and
reloads; a subsequent read denial must not be described as a failed mutation.

Keep only actor-owned operation/resource IDs and public conflict witnesses in
an in-memory recovery provider above routing/workspace/account unmounts. Never
retain tokens, team names, business values or share lists there. Record intent
before dispatch. Abort/unmount cannot prove rollback. Different actors cannot
see each other's pending operations. Browser reload/closure intentionally loses
this journal; warn before unload while an administration outcome is pending.

An uncertain outcome blocks another change on that resource. Fresh authorized
reads enable an explicit review of current state before a new intent; they do
not prove which request caused that state. A failed/missing/denied read cannot
clear the gate. A pending share recipient absent from a truncated list is not
observable and cannot release the gate: retain it until a complete authorized
read, a read displaying that target, or a separately designed operator-resolution
mechanism exists. An owner header cannot establish share state. Pending creation IDs remain inspectable; another creation is
blocked until review. Membership active-state witnesses retain ADR035's ABA
limits; this UI does not invent durable receipts or exactly-once guarantees.

Share business-command recovery through the same provider so route navigation
does not discard existing actor-owned command keys. Keep the existing command
receipt/reconciliation protocol unchanged. Busy gates and stale-response guards
prevent old session/record/team responses from replacing the active view. Tokens
stay in the existing in-memory connection only.

## Verification and recovery

Use populated decoder/client tests and component tests for CAS, true/false/null
membership witnesses, field-restricted assign-only delegates, revocation,
truncated shares, 4xx/5xx/malformed success, delayed responses, disconnect/account/
workspace/route switches, and unknown-outcome review. Browser tests exercise real
team and record APIs, scope changes, conflicts, denial and lost responses; no
mocked response alone proves a business mutation. Obtain independent adversarial
review of stable code, then run full checks/tests/build and browser regressions.

Rollback is a frontend rollback only: preserve server migrations, owner/team/
share facts and audit. Resolve pending changes by inspecting live state with an
authorized administrator; do not replay old POSTs or remove audit evidence.
Provider/login/skew, R3b–R3d, R6 and R7 (including the unconfirmed connection
failure) remain separate work.
