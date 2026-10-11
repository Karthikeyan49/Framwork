# M3 independent adversarial review

- Date: 2026-10-11
- Scope: M3 — Metadata Registry
- Reviewer: independent Codex review agent under `AGENTS.md` and
  `docs/agents/82_TWO_AGENT_PROTOCOL.md`
- Result: **PASS** for the reviewed implementation and independently executed
  checks below. No unresolved BLOCKER/HIGH finding remains.

Human milestone approval remains a separate gate. The reviewer made no
implementation or test edits; this report is the reviewer-owned repository change.
The existing uncommitted M2 implementation is the baseline, not new M3 work.

## Scope and architecture

Reviewed the JSON Schema 2020-12 authoring contract, typed seven-primitive models,
bounded JSON decoder, immutable validated Source, registry, server-side metadata
policy/API, migration 00005–00007, readiness, audit correlation, CORS/body boundaries
and real PostgreSQL tests against the roadmap, metadata/IR/compiler specifications,
data/security architecture and verification guidance.

The implementation stays within M3. The source hash identifies normalized typed
authoring storage; it is explicitly not compiler IR identity. Every stored/retrieved
version remains `draft`. Application heads track authoring revision only. No
production activation, approval, promotion, compiler symbol/type resolution,
runtime execution, business CRUD or AI generation is introduced.

The embedded schema rejects unknown structural keys, unsupported primitives and
arbitrary code/remote schema references. Definitions use stable IDs and a matching
namespace. The decoder bounds bytes, depth, node count and decimal precision,
rejects duplicate keys/trailing input/invalid UTF-8/NUL and uses exact decimals.
Source has private state and returns copied bytes. Typed serialization is schema
checked again; persisted JSONB is normalized and its source hash verified on read.
The schema loader cannot fetch external schemas. The managed new dependency
`github.com/santhosh-tekuri/jsonschema/v6` provides 2020-12 validation rather than a
partial handwritten validator. Regexp2 v1.11.0 appears in the module/test checksum
graph; it is not a linked runtime dependency or an indirect go.mod requirement.
The final addition also uses pinned `go.yaml.in/yaml/v3` v3.0.5 to parse a
JSON-compatible YAML subset into nodes. Conversion rejects anchors, aliases,
merge keys, custom tags, timestamp/binary types, non-string mapping keys and
multiple documents, then converges through the same strict JSON/schema/typed path.
Node parsing does not instantiate arbitrary constructors or expand aliases.

Registry SQL uses bound values and tenant-inclusive keys. RLS verifies active
identity/membership; normal credentials cannot modify immutable revisions or
override creation provenance. An immutable trigger adds an ordinary-owner DML
backstop. A deferred tenant/app/revision FK prevents absent-history authoring heads.
Central policy permits metadata reads for known tenant roles and writes only for
owner/admin. Unknown obligations deny. Validation failures and committed changes
have bounded correlated audit evidence; responses wait for audit and outer commit.
POST bodies/CORS are allowed only on the explicit metadata append route.

## Findings resolved and independently verified

| Severity | Finding | Correction/evidence |
| --- | --- | --- |
| HIGH | A valid near-quota compact source could append successfully but become unreadable because PostgreSQL adds whitespace to JSONB text and Get reapplied the smaller authoring-input limit. | An independent read-only probe reproduced 254995-byte accepted source becoming 263503-byte JSONB text and failing reread. Get now uses a separate bounded persisted-input path while still checking the normalized 256 KiB source limit and hash. Real API integration verifies near-quota 201 → GET 200 with the same hash and confirms the stored text exceeds the authoring limit. |
| MEDIUM | Decoded NUL in otherwise schema-valid strings was accepted by the validator but rejected by PostgreSQL JSONB, producing a storage error. | Independent read-only SQL reproduced accepted NUL source / rejected JSONB. Decoder now rejects NUL in strings and object keys; regression tests pass. |
| MEDIUM | The initial SQL document CHECK accepted NULL results for missing/null required envelope fields. | Additive migration 00007 explicitly rejects missing/null/wrong-type envelope fields. Real normal-role direct-insert negatives reject `{}` and a null/empty envelope. The already-applied migration history is preserved. |
| MEDIUM | Schema/trigger presence alone did not prove additive head/storage integrity migrations were installed. | Metadata readiness now checks the validated deferred head FK, validated envelope constraint, forced RLS, immutable trigger and required privileges/columns. Fresh disposable-database tests drop/restore both constraints and disable/restore the trigger, proving fail-closed readiness and recovery. |

Concurrent writers cannot lose an update. Under repeatable read a simultaneous
serialization loser can return a redacted 503 and roll back; a subsequent stale
expected-revision request returns 409. This is a deliberately documented recovery
behavior, not an automatic retry. Tests require exactly one winner and verify a
fresh expected revision succeeds afterwards.

## Independently executed checks

Commands were executed from `/workspace/Framwork` and completed with exit code 0.

| Exact command | Result | Evidence |
| --- | --- | --- |
| `make check` | PASS | Rerun after YAML addition: 84 preserved source-document checksums; Go format/vet/module verification, including contracts; frontend lint/typecheck. |
| `./scripts/go.sh test -race -count=1 ./internal/metadata ./internal/policy ./internal/platform/httpserver ./tests/security` | PASS | Rerun after YAML addition: typed/schema roundtrip, structural corpus, decimal/JSON/YAML limits, unsafe YAML rejection, NUL rejection, source immutability, policy matrix, HTTP regressions and module boundaries. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM3' ./tests/integration` | PASS | Final YAML-inclusive rerun completed in 4.586 seconds. Real normal-role authoring/version history, policy/tenant isolation, atomic audit failure rollback, immutable/column grants, deferred head integrity, concurrent append, POST/CORS bounds, near-quota and numeric JSONB/hash roundtrip, required-envelope storage rejection, YAML viewer denial/draft read/hash/unsafe-input audit/recovery. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM[123]' ./tests/integration` | PASS | Final YAML-inclusive combined M1/M2/M3 regression and fresh seven-migration application/reapplication, owner-role/privilege/RLS drift and new metadata readiness drift/recovery. Reported package runtime: 11.101 seconds. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM1FreshDatabaseMigrations$' ./tests/integration` | PASS | Final fixture addition: actual table-owner UPDATE/DELETE and TRUNCATE of both empty metadata fixture tables hit the immutable trigger. Operations occur only in the uniquely owned disposable database, with fresh migrations/readiness drift checks. |
| `./scripts/go.sh test -fuzz=FuzzJSONInputIsBounded -fuzztime=5s -parallel=2 ./internal/metadata` | PASS | 20202 fuzz executions, no crash; reported package runtime 6.134 seconds. This tests parser robustness, not a proof of every semantic invariant. |
| `./scripts/go.sh test -fuzz=FuzzYAMLInputIsBounded -fuzztime=5s -parallel=2 ./internal/metadata` | PASS | 31700 fuzz executions, no crash; reported package runtime 6.099 seconds. |
| `git diff --check` | PASS | Final whitespace/conflict-marker check. |
| `node scripts/with-env.mjs ./scripts/go.sh run .local/review/m3-jsonb.go` | Completed / findings reproduced | Read-only PostgreSQL queries demonstrated the pre-fix whitespace/NUL failures above. This was adversarial evidence, not a passing pre-fix storage test. |

The ignored read-only reproduction helper was removed after repository regressions
covered the corrected behavior. No reviewer database fixture or role was created
by that helper. No final independent check failed. Exploratory missing-path lookups
were corrected and are not represented as verification commands.

Complete builds, frontend behavioral tests, the M0 foundation integration suite,
live smoke and hosted CI were not independently executed in this M3 review. Any
implementer execution of those checks is separate evidence, not claimed here.

## Limits and future work

- Structural authoring validation does not resolve references, check expression
  types, validate workflow graphs/capabilities/side effects or produce executable
  IR. Those compiler responsibilities remain M4; promotion remains M12.
- JSON and a JSON-compatible YAML subset support schema 1.0.0. Full YAML graph/tag
  features are intentionally rejected. Unsupported versions fail closed;
  automatic upgrades and versioned extension namespaces are absent. Decimal
  normalization supports at most 18 fractional digits.
- Per-document/request complexity and list size are bounded. Aggregate tenant
  storage quotas, rate limiting, retention/export governance and performance
  sizing are later operational/security work. Integration runs retain immutable
  test metadata/audit history; they do not claim to erase all registry evidence.
- RLS settings remain trusted Go context, not cryptographic credentials against a
  stolen database password with arbitrary SQL. Repository Executor is never an
  AI/customer SQL interface. DDL administrators can change protection; readiness
  checks selected contract drift, not every function/schema definition.
- Operator DML cannot alter immutable revisions, but privileged DDL can. External
  immutable archival and disaster-recovery operations remain separate concerns.
- No browser metadata builder or ERP page is implemented in M3. No new milestone
  should begin automatically.

**M3 REVIEW STATUS: PASS. Stop before M4 pending a separate authorized task.**
