# M12 design appendix — rule tables and contracts

Companion to [ADR102](../adr/102-m12-candidate-release-builder.md). The ADR owns
decisions; this appendix fixes the detailed tables so independently built pure
packages agree. Tables are data: changing one means publishing a new version
(`release-risk-v2`, `requirement-policy-v2`, `verifier-v2`), never editing v1.

## A. Module layout (respect tests/security/boundaries_test.go)

Subpackages of a boundary module are private; other modules import only the root
package. Therefore:

- `internal/compiler` (root package): `Diff`, `ClassifyStorage` (new files).
- `internal/risk` (new top-level module, add to the boundaries list): facts,
  policy `release-risk-v1`, separation, policy registry.
- `internal/intent` (existing): v2 requirement/acceptance types, validation,
  oracle, probes (v1 `Contract`/`Acceptance`/`Hash` untouched — stored 1.0.0
  bundles recompute `Intent.Hash()`).
- `internal/verification` (new top-level module, add to boundaries): the
  deterministic verifier.
- `internal/promotion` (existing root): bundle 2.0.0, binding/staleness, facts
  extraction, store port wrappers.
- `internal/metadata` (existing): patch/rebase hardening.

## B. compiler.Diff

Pure; compares two restored `IR` values (call `Program.IR()` once per side).

- Per-definition identity `(kind, id)`; hash with `metadata.DefinitionHash` after
  **normalizing away `Definition.SchemaVersion`** (it equals the envelope version;
  a 1.2.0→1.3.0 upgrade must not change every definition). IR/compiler version
  change is reported as separate fact `IR_UPGRADE`.
- Change = ADDED | REMOVED | CHANGED, with `Aspects` for CHANGED: `LABEL`,
  `STORAGE` (field type/required/enum/currency/reference), `RULE_LOGIC`,
  `RULE_FINANCIAL` (plan returns money or reads a money-typed field),
  `WORKFLOW_GRAPH`, `WORKFLOW_TRIGGER`, `APPROVAL_GATE` (approval state roles,
  timeout, outgoing transitions, or a condition reading a money field),
  `TOOL_PIN` (activity tool ref/version/hash added/removed/re-pinned),
  `TOOL_DECLARATION` (authored `kind:"tool"`: side_effect, permissions,
  approval, sandbox_mode), `PAGE`, `RELATIONSHIP`, `PLAN_ONLY` (compiled
  `rules.Plan`/`workflow.Plan` hash differs while the definition hash is equal).
- Also compare compiled plans (`ir.Rules`, `ir.Workflows`) by canonical hash.
- Deterministic order (kind, id); bounded (fail closed above 5,000 definitions);
  canonical JSON `Diff.Hash`. Unknown kind/aspect → aspect `UNCLASSIFIED`.
- Reflect test: every non-nil pointer field kind of `metadata.Definition` (Entity,
  Field, Relationship, Page, Rule, Workflow, Tool) maps to a handled case.

## C. compiler.ClassifyStorage

`ClassifyStorage(base, target Program, intent EvolutionIntent) StorageDelta` never
errors for classification: returns `Class` ∈ EQUAL | ADDITIVE | TRANSFORM |
BREAKING and machine-readable `Reasons` (REMOVED_ENTITY, REMOVED_FIELD,
TYPE_CHANGE, OPTIONS_CHANGE, CURRENCY_CHANGE, REFERENCE_CHANGE, LAYOUT_MISMATCH,
UNDECLARED_RENAME, MISSING_BACKFILL, APP_MISMATCH, …) plus the `EvolutionPlan`
hash when one exists. Property: `Class == BREAKING` iff `PlanEvolution(base,
target, intent)` errors; EQUAL iff storage schema hashes are equal; ADDITIVE iff
the plan is COMPATIBLE with only ADD_ENTITY/ADD_OPTIONAL; TRANSFORM otherwise.

## D. risk package: `release-risk-v1`

`Facts` (all derived server-side): `Changes` (from Diff, reduced to rule-relevant
booleans and sets), `Storage {Class, Reasons}`, `Tools []{Ref, Version,
SideEffect, Approval, Idempotency, Change(ADDED|REMOVED|REPINNED|UNCHANGED)}`,
`Volume` ∈ NONE | SMALL | LARGE | UNVERIFIED, `FirstRelease`, `IRUpgrade`,
`AcceptanceAuthorIsRequester`, `Unclassified bool`, `UnknownCapability bool`.

| Rule | Fires on | Level |
|---|---|---|
| S1 | only `page` changes and/or label-only changes | LOW |
| S2 | entity added | MEDIUM |
| S3 | optional field added | LOW |
| S4 | relationship added/removed/changed | MEDIUM |
| S5 | non-financial rule added/changed/removed | MEDIUM |
| S6 | financial rule (money-typed) | HIGH |
| S7 | workflow added/changed (no tool, no approval-gate change) | MEDIUM |
| S7b | trigger widened manual → record.created/updated | MEDIUM floor |
| S8 | approval gate changed, or condition reads a money field | HIGH |
| S9 | tool activity READ/NONE added/removed/re-pinned | MEDIUM |
| S9 | tool WRITE with NATIVE_KEY / LOOKUP_BY_REFERENCE / DEDUPE_WINDOW | HIGH |
| S9 | tool WRITE with idempotency NONE | CRITICAL |
| S9 | FINANCIAL or DESTRUCTIVE | CRITICAL |
| S9 | removal of a WRITE tool activity | MEDIUM |
| S9b | any change to a workflow containing a tool inherits the tool's level | inherit |
| S10 | authored tool declaration: none/read MEDIUM; write HIGH; destructive/financial CRITICAL; gains `data.write` or weakened approval HIGH | |
| S11 | compiled plan changed with identical definitions | MEDIUM |
| S12 | IR or compiler version changed | MEDIUM |
| S13 | first release (no base) | floor MEDIUM |
| S14 | unclassified change | HIGH |
| BLOCK | storage BREAKING; unknown capability; tool not in catalog / tenant not allowed; acceptance author == requester at HIGH or above | BLOCKED |

Storage × volume: EQUAL LOW (all volumes); ADDITIVE LOW/LOW/MEDIUM/HIGH for
NONE/SMALL/LARGE/UNVERIFIED; TRANSFORM MEDIUM/MEDIUM/HIGH/CRITICAL; BREAKING
BLOCKED. Level = max over fired rules (monotone, no downgrade). Output
`Assessment {PolicyVersion, PolicyHash, Level, Blocked, Reasons[sorted rule ids],
FactsHash, Separation}`; `PolicyHash = sha256(canonical{version, rules, matrix,
thresholds, separation})`; a golden matrix test pins the hash of fixed
Facts→Assessment cases. `HighRisk = Level >= HIGH`. Separation: HIGH
`{Approvals:1, Excluded:[requester, acceptance_author]}`; CRITICAL `{Approvals:2,
Excluded:[requester, acceptance_author, sandbox_freezer]}`; else
`{Approvals:1, Excluded:[]}`. Rollback class fact: EQUAL + no WRITE tool
`POINTER_SAFE`; ADDITIVE/TRANSFORM `DATA_NOT_REVERSIBLE`; WRITE tools
`EXTERNAL_EFFECTS`.

## E. intent v2 and the verifier

### Requirement set `requirements-v2`
```
{ schema_version, app_id, requirements: [ { id, category, requiredness
  (MANDATORY|RECOMMENDED|OPTIONAL), requiredness_basis (POLICY_FLOOR|
  OWNER_DECISION), status (RESOLVED|OPEN_QUESTION|REJECTED), provenance
  (AUTHENTICATED_HUMAN|AI_PROPOSED_HUMAN_CONFIRMED), semantics } ] }
```
≤100 requirements, strict JSON (no duplicate/unknown/cased keys; reuse
`metadata.DecodeJSON`). `requirement-policy-v1` floors: `approval_threshold`,
`approval_gate`, `tool_effect` always MANDATORY; `data_migration` MANDATORY when
storage class ≠ EQUAL. A requirement whose category is not in the verifier
vocabulary can never satisfy a MANDATORY floor.

Semantics per category (closed):
- `approval_threshold`: `{workflow_id, entity_id, field_id (money), currency,
  op GT|GTE|LT|LTE|EQ|NEQ, amount (decimal string, currency minor units),
  approver_roles [owner|admin|member|viewer]}`
- `approval_gate`: `{workflow_id, approver_roles[]}` — every initial→complete path
  passes an approval state whose roles EQUAL the required roles (set equality, Amendment 3)
- `tool_effect`: `{tool_ref, max_side_effect NONE|READ|WRITE,
  requires_approval bool, max_sandbox_mode BLOCK|SIMULATE|READ_ONLY|REROUTE|
  TEST_ACCOUNT}` checked against the pinned catalog definition and plan pins
- `data_migration`: `{max_class COMPATIBLE|TRANSFORM}` checked against
  `ClassifyStorage`

### Acceptance spec `acceptance-v2`
`{schema_version, requirements_hash, cases:[{id, requirement_id, given
(typed field values), expect: NO_APPROVAL | APPROVAL_REQUIRED{roles} |
INPUT_REJECTED{reason: TYPE|CURRENCY|ENUM|REQUIRED|RANGE}}]}`. ≤1,000 cases. No
IR/source/compiler/artifact-hash keys anywhere (structural independence).

### Oracle and derived probes
Oracle for `approval_threshold` is a pure function of the requirement and an
input amount (exact `big.Rat` via `erp.Money` and currency minor units; wrong
currency = INPUT_REJECTED CURRENCY). Derived probes (always added, never removable):
`t−1 minor unit`, `t`, `t+1 minor unit`, `0`, a negative amount, wrong currency.
Authoring-time check: every human case must agree with the oracle, else the spec
is rejected (a human declaring `500001 INR → finance approval` for `GT 500 USD` is
rejected).

### Verifier `verifier-v2` (vocabulary `acceptance-vocabulary-v1`)
Inputs: restored `compiler.Program` (never compiles), requirement set,
acceptance spec, `ClassifyStorage` delta, tool catalog + plan pins, sandbox
evidence. Executes: for threshold cases, set the money field and the case's other
`given` values, evaluate the workflow `Next` walk from `initial_state` using the
runtime's `workflow.Program.Next`/`rules.Program.Evaluate` until an approval state
is reached or a terminal state; compare approval presence and roles to the
oracle. BLOCKED reasons: `LEGACY_IR` (IR < 1.2.0 workflow/rule), `NO_EXECUTABLE_PLAN`,
`UNSUPPORTED_BY_RUNTIME` (approver role or trigger the runtime cannot express),
`UNSUPPORTED_BY_VERIFIER`, `OMITTED_REQUIREMENT`, `OPEN_MANDATORY`, `FLOOR_VIOLATION`.
FAIL reasons are stable codes with the failing case id. Report = canonical JSON,
deterministic (no time, no randomness, sorted), bound to requirement-set hash,
acceptance hash, IR hash, sandbox evidence hash, verifier and vocabulary
versions; `Report.Hash`. Mutation fixtures (each must FAIL or BLOCK with its
code; a control fixture PASSes): GTE instead of GT, wrong approver role, rule
constant true, unconditional transition bypassing approval, wrong currency field,
unsupported role/trigger, omitted requirement, `approval_gate` with a bypass
path, tool exceeding `max_side_effect`.

## F. Staleness (`promotion.Staleness(bound, current)`)

| Input | Fresh iff | Reason |
|---|---|---|
| base release hash | equals active `artifact_hash` | BASE_CHANGED |
| base generation | equals active `generation` (ABA) | BASE_GENERATION_ADVANCED |
| base storage schema | recomputed equals bound | SCHEMA_CHANGED |
| policy bundle hash | recomputed equals bound | POLICY_CHANGED |
| relevant tool pins | every pin resolves via `Lookup(ref,version,hash)` | TOOL_CHANGED / TOOL_REMOVED |
| tool tenant allowlist | `AllowsTenant(tenant)` for every pin | TOOL_TENANT_DENIED |
| risk | `Evaluate(latest policy, bound facts + current volume)` level ≤ bound | RISK_ESCALATED |
| sandbox evidence | row exists, FROZEN, hash/IR/schema equal | EVIDENCE_INVALID |
| IR version | still in `SupportedIRVersions()` | UNSUPPORTED_RUNTIME |
| runtime components | recorded `ComponentsHash` equals the running binary's (`RuntimeComponents()`); empty never matches | RUNTIME_CHANGED |

Sorted, deduplicated; pure over plain inputs; never stored.

## G. Bundle 2.0.0 field list (Go tags are the JSON keys)

`Version`, `HighRisk`, `RequesterID`, `BaseRelease`, `BaseGeneration`,
`Source{Hash,Revision,SchemaVersion,JSON}`, `IR{Hash,Version,CompilerVersion,JSON}`,
`RuntimeContract{Version,ComponentsHash}`,
`Dependencies{PolicyBundleHash,ToolsHash,ToolPins[],SchemaLayout,BaseSchemaHash,TargetSchemaHash}`,
`Evolution{Hash,JSON}` (optional), `Diff{Hash,JSON}`,
`Risk{PolicyVersion,PolicyHash,Level,Reasons,FactsHash,Facts,Separation,Rollback}`,
`Requirements{Version,Hash,PolicyVersion,JSON}`,
`Acceptance{ID,Hash,AuthorID,RequirementsHash,JSON}`,
`Sandbox{DatasetID,EvidenceHash,Evidence}` (≤16 KiB),
`Verification{VerifierVersion,VocabularyVersion,Outcome:"PASS",ReportHash,Report}`.
No timestamps, random values or map iteration order inside the bundle. `Restore`:
size ≤ 8 MiB; `DisallowUnknownFields`; canonical re-marshal equality; recompute
Diff/Risk/Verification/dependencies-that-are-pure and require byte equality;
unknown policy/verifier version fails closed.

## H. Policy actions and staging

New actions: `requirements.read`, `requirements.decide`, `acceptance.read`,
`acceptance.author`, `candidates.read`, `candidates.build`. Reads: all roles.
Writes: owner/admin; custom roles may grant; never a role named `viewer`.
`policy.Version` bump to `m12-native-v9`. Staging via `control.stage_candidate`;
no direct INSERT on `control.release_bundles` for `framework_app`.
