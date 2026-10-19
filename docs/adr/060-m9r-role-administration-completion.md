# ADR060 — Complete governed tenant role administration

19 October 2026 (Asia/Kolkata). Proposed before implementation. Scope: remaining
R5 authoring/assignment UI and identity deployment preparation. Acceptance needs
independent adversarial review and executed regressions. No M10, main merge or
production activation. Preserve ADR032/038/039 and all existing data/history.

## Existing authority

R5a provides immutable role versions, head CAS, publication operation receipts
and owner-only assignment with last-owner protection. R5b–f provide worker
revocation, record/team administration, field masking, custom workflow approvers
and OIDC/PKCE. These working modules remain. The remaining role editor must not
infer privileges from tokens or silently replace native bootstrap permissions.

## Additive read contracts

Add GET `/v1/tenants/{tenant}/roles?after=<role-name>`: at most 100 summaries,
with a keyset continuation when more exist. Select only current tenant heads;
ASCII role names use explicit C ordering. Summaries omit definition bodies.
Existing point reads and immutable publication-operation reads remain compatible.

Add GET `/v1/tenants/{tenant}/role-contract?name=<role-name>` under `roles.read`.
It returns the closed action catalog and existing role-specific ALLOW support,
scope requirements and authoring limits. It describes accepted definitions; it
does not grant the reader any action. Keep the protected owner role explicit.
Use a distinct route so roles named `catalog`, `options` or `operations` remain
readable at their original point URLs. Reject ambiguous/malformed new queries.

Add GET `/v1/tenants/{tenant}/members/{workspace_member}` under
`memberships.read`, with tenant-scoped SQL and typed UUID validation. The existing
100-member list is bounded and cannot prove a target absent. A point read makes
assignment review possible beyond that list. No global IAM role/member discovery,
new grant, table, migration or transaction bypass is required.

## Role management UI

Provide a separate workspace role panel, with explicit pagination and point-name
inspection. Offer create, inspect, edit, activate/deactivate and member assignment.
Use structured grants and recursive typed scopes (ALL/NONE/OWNER/TEAM/SHARE,
CONDITION, AND/OR). Conditions are JSON values with bounded, lossless authoring;
unsupported/unsafe numeric representations fail instead of silently rounding.
Mirror server budgets for usability, while server compilation/policy stays final.
Render names and definitions as text, never executable HTML or policy scripts.

Owner is read-only. Bootstrap role bodies have empty grants but native authority;
display that distinction and require explicit acknowledgement before replacing
their behavior. Configured roles have only listed grants; empty grants deny.
Require explicit review before publishing changes that affect all assigned users.
Never suggest a grant unsupported by the role contract or add privileges implicitly.

Publication sends a fresh UUID, exact observed head version (zero only after an
authorized missing-role read), explicit active state and serialized definition.
Verify the successful response's role, next version, operation ID, active state,
definition and exact request-body hash. Editing an observed role never becomes
an implicit create. A conflict or revoked read clears writable state until reload.

Use ADR038's actor-owned memory journal above navigation/account/workspace changes.
Persist intent before dispatch, keeping only resource/operation IDs, version/hash
and public conflict witnesses, never tokens, policy bodies or condition values.
Transport failures, 5xx and malformed successful responses have unknown outcome;
never automatically retry a POST. Reconcile a publication with its immutable
operation receipt, even if the head advanced; then reload the current head.
An absent/denied receipt cannot claim rollback. A fresh authorized role state or
explicit absence can support a clearly labelled new intent after human review;
its CAS protects against an older publication completing later.

Assignments use a fresh point member read and the existing expected-role label,
not an invented revision witness. Preserve the documented ABA limitation; no
exactly-once/linearizable membership claim. Unknown assignments block that member
until fresh authorized state is explicitly reviewed. Warn before self-demotion;
refresh workspace authority after known self-assignment success. Retain last-owner
protection in SQL. Add user ID, requested role and expected role to successful
assignment acknowledgements alongside the existing `assigned` boolean; this
binds UI success to the request without changing the underlying CAS contract.
Aborted/unmounted requests do not prove rollback. Ignore stale
responses belonging to another connection, workspace or selected resource.

## Identity deployment boundary

No external provider, issuer/audience/JWKS or HTTPS callback is currently configured
in this environment. The user explicitly selected local completion and deployment
preparation, with external-provider verification deferred. Secret values belong
in environment settings. Prepare concrete operator guidance and evidence
checks for pinned public-client PKCE, HTTPS same-origin callback, gateway query
redaction, key rotation, dedicated Redis ACL and provider/MFA assurance.
Do not equate a local fixture with production provider/deployment certification.
Do not deploy or choose a production provider without the required configuration.

## Acceptance and recovery

Execute populated tenant/policy/read denial, pagination and member-point negatives;
actual publication receipt/CAS, deactivation, last-owner and atomic audit failure
tests; client/component tests for malformed witnesses, bootstrap replacement,
recursive scopes, unsafe numbers, late responses, navigation and unknown outcomes;
and real browser API publication/assignment/recovery cases. Independently review
stable code, fix findings, then run full check/test/build, both browser suites and
representative unchanged read/write capacity gates. Record every FAIL/SKIP/NOT RUN.

Rollback removes the new UI/read endpoints only, retaining role heads/versions,
memberships, operation receipts, users and audit. Restore a configured definition
through a new reviewed CAS version; never mutate history, blindly replay a POST
or downgrade to a binary that ignores live role heads. Source kits remain verbatim.
