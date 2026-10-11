# FINAL SYSTEM VERIFICATION

Release is blocked until evidence exists.

## Architecture
- [ ] modular boundaries
- [ ] AI cannot mutate production
- [ ] canonical IR
- [ ] immutable release versioning
- [ ] sandbox/promotion separation

## Tenant/security
- [ ] trusted tenant context
- [ ] RLS enabled on tenant tables
- [ ] normal app role cannot bypass RLS
- [ ] cross-tenant suite passes
- [ ] default-deny policy
- [ ] secrets separated

## Compiler/runtime
- [ ] structural + semantic validation
- [ ] deterministic IR hash
- [ ] parameterized query generation
- [ ] query risk limits
- [ ] deterministic rules
- [ ] workflow durability
- [ ] side-effect idempotency
- [ ] outbox/inbox dedupe

## Sandbox/promotion
- [ ] safe data mode
- [ ] dangerous tools blocked/rerouted
- [ ] complete diff
- [ ] stale-base conflict
- [ ] approval
- [ ] migration validation
- [ ] atomic activation
- [ ] previous release retained

## AI
- [ ] structured patches
- [ ] bounded repair
- [ ] model gateway
- [ ] budgets
- [ ] injection/agency tests
- [ ] AI outage does not break standard runtime

## Observability/operations
- [ ] traces/metrics/logs/audit
- [ ] request/version/workflow/tool correlation
- [ ] backups and restore drill
- [ ] migration rehearsal
- [ ] failure drills and alerts

## Performance
- [ ] target load tested
- [ ] p95/p99 documented
- [ ] slow-query review
- [ ] queue/resource budgets

## Portability
- [ ] application export
- [ ] data dictionary
- [ ] import/export roundtrip

## Review
- [ ] Claude implementation report
- [ ] Codex review: no unresolved BLOCKER/HIGH
- [ ] human architecture/security sign-off
