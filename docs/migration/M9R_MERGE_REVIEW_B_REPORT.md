# Merged-tree review — remediation slice B

20 October 2026 (Asia/Kolkata). Reviewed product tree:
`552fa9efe2092a592e39974ee23ec1ecbc057729`, with the slice A documentation
checkpoint `e78282f24e6b73b72b46ee9e57ad0c043b1366f7` on top.

**Slice B review: COMPLETE WITH OPEN OBJECTIONS. Functional gate: FAIL.
M9R STATUS: PARTIAL.** Three functional/contract gaps (B-01, B-02, B-05),
one historical full-IR compatibility boundary (B-03), minor stale wording
(B-04), and the recurring worker-policy diagnostic require follow-up.

This is the remediation author's review of the 23 resolved files listed in
[the integration plan](LINE_INTEGRATION_PLAN.md), plus adjacent code necessary
to check the requested wiring and compatibility claims. It is not an
independent adversarial review of M10–M14. Independent review is **NOT RUN**,
explicitly deferred by the user until agents become available after
22 October, 21:00 UTC. No agents were spawned or awaited.

## Scope and provenance

Before this slice, fetched origin and explicitly refreshed the integration
tracking ref, then ran `git merge --no-edit origin/integration/m9r-m14`.
Result: **Already up to date**. The integration tip remains `552fa9e`.
The resolved merge is `e219b3185332379d3327ff5c1a70d76752e26455`, whose
parents are remediation `b00da36ca246bf52dcbf26b939ab627792a3abb3` and the
milestone renumbering preparation `83660907075c1070f5898eb8574a4723f89bc543`.
Inspection used the parent/current file differences and the merged call sites,
not just the absence of conflict markers.

No application code, shared integration file, migration, contract, dependency,
AGENTS.md or CLAUDE.md is edited in B. Objections are handed to the milestone
agent in [AGENT_HANDOFFS.md](AGENT_HANDOFFS.md). Slice C's plan/ledger updates,
proposed agent instructions and subsequent R7 implementation are not included.
M9R remains **PARTIAL** and M10–M14 remain a development/test preview.

## Per-file resolution review

“Confirm” means the inspected resolution preserves the identified remediation
contract. It does not certify the whole subsystem or supersede open findings.
Qualifications refer to the evidence and limits below.

| # | Resolved file | Verdict and evidence |
|---|---|---|
| 1 | `.github/workflows/ci.yml` | **Confirm.** Existing format/static/unit/integration/build/browser/fuzz steps remain. Raising the job timeout to 120 minutes accommodates the merged suite; it does not relax individual performance or fault-test assertions. This CI job alone does not execute every reference target or the driver-panic diagnostic. |
| 2 | `Makefile` | **Confirm.** Remediation reference, outage, retained-reader and browser targets remain. The integration timeout becomes 90 minutes, not an altered request-latency budget. Existing fuzz recipes cover five targets; A separately discovered and ran all nine. The unchanged launcher recipes expose B-01 below. |
| 3 | `apps/web/src/preview/Workspace.test.tsx` | **Confirm.** Adds the 1.5.0 decode case while preserving retained schema/IR cases and rejection of unsupported 2.0.0. No existing page/component implementation is replaced. |
| 4 | `apps/web/src/preview/contracts.ts` | **Confirm.** Adds IR 1.5.0 and allows unique fields in both 1.4.0 and 1.5.0. Preserves strict field decoding and the prohibition on unique intrinsic IDs. This is client decoding, not authorization. |
| 5 | `contracts/jsonschema/application-v1.3.schema.json` | **Confirm.** Byte-identical to remediation `b00da36`; 1.3.0 retains approval-role meaning and admits no tool activity. Historical milestone tool-IR compatibility is narrower than standalone plan compatibility; see B-03. |
| 6 | `contracts/jsonschema/embed.go` | **Confirm.** Embeds the union of schemas 1.0–1.5. Old schema files are retained rather than replaced by the tool schema. |
| 7 | `docs/migration/M9R_FINDING_STATUS.md` | **Confirm preservation; reconciliation deferred to C.** The complete original remediation ledger is an unchanged byte prefix; milestone history is appended with the number map. No original finding is silently upgraded. Historical appendix wording is not a current production-acceptance claim. |
| 8 | `internal/compiler/README.md` | **Confirm.** Retains module boundaries, R5e approval-role and R3c uniqueness contracts and documents the separate tool contract and M12 pure classification/patch APIs. The module does not execute tools or promote releases. |
| 9 | `internal/compiler/compiler.go` | **Confirm emission; qualify restore closure (B-02).** Preserves 1.3.0 approval and 1.4.0 unique meanings, adds distinct 1.5.0 emission, and supplies the trusted tool resolver only for the tool schema. The trusted catalog is excluded from platform catalog JSON; old platform hashes remain stable. Adjacent `restore.go` does not carry the exact compiler-pair guard into 1.5.0. |
| 10 | `internal/compiler/rules.go` | **Confirm.** Restored rule/source correspondence now admits the 1.5.0 superset while retaining all earlier schema/IR pairs and the legacy non-executable-rule path. Restore validates stored plans; it does not compile. |
| 11 | `internal/compiler/workflow_plans.go` | **Confirm current pairs; qualify historical tool IR (B-03).** Explicit schema/IR pairs include 1.2–1.5; tool plans are refused before 1.5.0. Plan/source matching and trusted catalog pin checks remain distinct. Supporting a legacy standalone tool plan is not support for an entire pre-renumbering tool IR. |
| 12 | `internal/config/config.go` | **Confirm validation; object to launcher integration (B-01).** Existing identity, clock-skew, OIDC, sign-in, admission and TLS rules remain. Tool/AI paths are bounded and reject NUL; internal tool networking and AI provider configuration are refused in production. Normal child launchers remove these new options before `Load` can see them. |
| 13 | `internal/metadata/README.md` | **Confirm.** Documents additive versions, retained immutable schemas, approval/uniqueness semantics and tool authoring without granting metadata ambient execution authority. |
| 14 | `internal/metadata/models.go` | **Confirm types; minor wording objection B-04.** `WorkflowSchema` includes 1.2–1.5 and `IntegritySchema` includes 1.4/1.5; tool fields are typed and optional to preserve old bytes. The `Activity` comment still names obsolete tool schema 1.3.0. |
| 15 | `internal/metadata/validation.go` | **Confirm.** Retains shared immutable embedded validators and strict typed parsing, adds 1.5.0 and duplicate/case-folded JSON-member rejection. No authorization or tenant decision is cached. No valid retained-schema document rejected solely by the new duplicate-member rule was demonstrated in this review. |
| 16 | `internal/platform/access/access.go` | **Confirm.** Peer/global traffic admission precedes bearer verification, body buffering and database work. Subject admission uses verified identity; each retry rechecks live authentication. Only the exact sandbox-freeze pattern selects RC. AI continuation and write-deadline extension happen after the outer transaction has completed, not before front-door admission. Exact role/team route patterns preserve role-named artifact routing. |
| 17 | `internal/platform/access/metadata.go` | **Confirm.** Registers added release/sandbox/AI POST paths within the registry/preview guard and retains governed role/team routes. These preview routes do not enable a production data plane. |
| 18 | `internal/platform/access/workflows.go` | **Confirm.** Batched workflow action authorization reads the record through the leased transaction, applies live policy to every action, retains audit decisions and uses an API-only approver lock. Tool input fields enter the trusted-record/DataScope path. The frozen-sandbox conflict maps only its specific SQL state/message. General approval freshness/revise/lease work remains R7, not resolved by this merge. |
| 19 | `internal/platform/app/app.go` | **Confirm in-process composition; object to launch path (B-01).** Preserves DB admission, traffic, auth skew, live policy loader, separate sign-in Redis, worker lease identity and dispatcher separation. Adds tool catalogs to API/worker, gateway only to worker, and AI configuration to API. Go composition cannot recover settings removed by `runtimeEnv`. |
| 20 | `internal/platform/postgres/transaction.go` | **Confirm.** `WithRuntime`, `WithEventRuntime` and `WithRuntimeIsolation` delegate to one `withRuntime`. Only the private executor carrying its own RR transaction certifies `RuntimeSnapshot`; RC does not. Role check, context cleanup, limits and rollback stay on the leased connection. This does not fix the separately failing injected driver-panic recovery diagnostic. |
| 21 | `internal/runtime/workflow/contracts.go` | **Confirm with compatibility limit B-03.** Preserves closed-role DSL 2.0.0 and tenant-role DSL 2.1.0, adds 2.2.0 pinned tool contracts, and keeps tool-free serialized plans stable through omitted optional members. The restore-only 2.1.0 tool shape has an explicit reader. |
| 22 | `internal/runtime/workflow/plan.go` | **Confirm with compatibility limit B-03.** Retains typed graph/state/retry/role checks; current tool plans require the pin/contract and use DSL 2.2.0. Stored legacy 2.1.0 tool plans restore byte-for-byte and match their older authored source. A new 2.2.0 tool plan cannot claim that old source. Existing DAG/time/history limits remain open R7 scope. |
| 23 | `tests/integration/m8_delivery_test.go` | **Confirm.** Forward-cutover fixture uses `UpTo`, with default zero still applying the complete merged migration set. Worker fixture effects move to isolated `m8_fixture` with forced RLS and exact tenant/job scope; no production worker-guard allowlist is weakened. The actual Redis-restart fixture remains enabled, selected onto the owned project by `COMPOSE_PROJECT_NAME`. |

Adjacent checks: all 57 historical SQL migrations and schemas 1.0–1.4 retain
their remediation bytes. Comparing parsed 1.4 and 1.5 schemas finds version
identifiers and the bounded tool-activity alternative as the differences;
unique-field and tenant-approval rules remain. `internal/ai/schema_text.go`
lists 1.5.0 and migration 00080's metadata CHECK retains 1.0–1.5. The derived
worker guard from 00087 remains the latest definition and was exercised in A.
Role/policy additions preserve earlier governed administration actions and
apply the live loader; token role claims are not converted into authority.
The metadata-version table CHECK is not the only SQL version gate: B-05 below
examines the separate unique-constraint registration selector.

## Objections and compatibility boundaries

### B-01 — configuration discarded by documented launchers (confirmed)

**Priority: medium functional integration defect; milestone handoff.**
`scripts/runtime-env.mjs` does not retain `AI_PROVIDER_FILE`,
`TOOL_CATALOG_FILE`, `TOOL_SECRETS_DIR`, `TOOL_CA_FILE` or
`TOOL_ALLOW_INTERNAL`. Both `scripts/with-env.mjs` (used by `make api/worker`)
and `scripts/dev.mjs` use this allowlist for runtime children. Consequently the
new Go composition sees an empty provider/catalog through these launch paths.
The AI preview remains unconfigured and the compiler/worker cannot use the
configured tools. Directly constructing `config.Config` in integration fixtures
does not exercise this propagation path.

Executed a pure `runtimeEnv` probe with synthetic paths and positive controls,
then a real `with-env.mjs` child probe. Both report missing required settings;
both commands exit 1. Existing API sign-in/admission settings survive. The
operator migration credential remains absent. Neither probe contacts a model,
tool, database or external service. No production or cross-tenant bypass is
demonstrated: the observed failure is fail-closed configuration loss.

Requested milestone fix: explicitly decide each process's minimum configuration
and extend the allowlist and launcher-level regression coverage accordingly.
API needs its AI configuration and trusted compiler tool catalog; worker needs
its catalog and gateway configuration. Do not solve this by copying the entire
parent environment, leaking migrator credentials, or forwarding tool secrets to
dispatcher/web. Verify the documented launcher with configured fixtures as well
as direct Go composition. No fix is applied in B.

### B-02 — 1.5.0 does not inherit the exact compiler identity guard

**Priority: medium compatibility/integrity-contract objection; shared compiler
handoff. Confirmed through the public Restore API.**
`internal/compiler/restore.go:45` applies the exact application/IR/compiler
pairing check only when `ir.IRVersion == IntegrityIRVersion` (1.4.0).
The merged 1.5.0 superset admits the same unique-field contract but bypasses
that exact compiler-identity check. `validateStoredRules` and
`validateStoredWorkflows` check schema/IR pairs, not the compiler label.

The pure probe compiles the same contacts fixture with one declared unique
field under 1.4.0 and 1.5.0. Both unmodified artifacts restore. After replacing
only `CompilerVersion` with `unsupported-compiler` and recomputing the hash,
1.4.0 rejects the artifact but 1.5.0 accepts it. The assertion command exits 1.
This is the exact-pair invariant already exercised by
`TestIntegrityDeterminismStorageAndImmutableReaders` for 1.4.0; its coverage
does not extend to this 1.5.0 path.

This does **not** demonstrate replacement of an already approved hash or a
client-accessible promotion exploit: the probe supplies a new self-consistent
hash to Restore. It also does not demonstrate bypass of SQL unique claims.
Earlier IR versions deliberately preserve historical compiler provenance;
do not reject those as a side effect of fixing this specific superset gap.
Carry the 1.4 invariant into 1.5, or explicitly document and test an alternative
versioned compatibility contract before treating this as resolved. No fix is
applied in B.

### B-03 — pre-renumbering full tool IR is a different compatibility boundary

**Disposition: confirmed compatibility restriction, requiring explicit cutover
wording/recovery evidence; not a request to give 1.3.0 two meanings.**
Exported the public tool-workflow fixture with the actual producer from
milestone commit `60da6f2`, using an ignored `git archive` source extraction
and a test overlay, not a new checkout/worktree. The old producer compiles and
self-restores its unmodified artifact successfully. Captured identity:

```text
IR/compiler: 1.3.0 / 1.3.0
IR SHA-256: b3aa401e1b0c1285d61fae6db0edfda677fd3809b8b4e356d74f7da698e70a22
workflow plans: 1; standalone plan DSL: 2.2.0
```

The merged compiler rejects that complete old IR unchanged. Its embedded
standalone DSL 2.2.0 plan still restores byte-for-byte. Separately, the existing
legacy DSL 2.1.0 plan golden test passes in the unit gate. Those two plan-reader
results do not establish compatibility of a complete pre-renumbering tool
release whose authoring schema/IR/compiler used the now-reserved 1.3.0 token.

Renumbering new tool authoring to 1.5.0 is the authorized conflict resolution;
retaining remediation's existing 1.3.0 meaning is correct. Qualify the migration
claim accordingly. Before moving any real old milestone database/artifacts,
inventory pinned full releases and choose an explicit preservation/cutover
path, retaining the original reader/branch for recovery where required.
Do not rewrite hashes, relabel stored bytes or silently recompile in-flight
instances. This probe uses a public fixture, not an inventory of customer
releases, and performs no database migration.

### B-04 — stale schema comment (low)

`internal/metadata/models.go` describes `Activity` as a schema 1.3.0 tool
activity. Actual tools are opt-in schema 1.5.0; 1.3.0 has approval-role meaning.
An assertion message in `internal/compiler/workflow_tools_test.go` also says
“1.3 IR identity”. Correct those comments/messages in the milestone lane; do not
change the retained 1.3 schema or serialized version tokens. This is wording,
not an observed runtime behavior failure.

### B-05 — SQL uniqueness registration retains a 1.4.0-only gate

**Priority: medium functional version-integration defect; coordinated
runtime/schema handoff. Confirmed with populated API/database fixtures.**
`runtime.register_unique_constraint` in migration 00052 selects the declared
field only from metadata whose `schema_version='1.4.0'`. No later migration
replaces that function. The general metadata-version CHECK admits 1.5.0, and
the Go schema/compiler/runtime recognize its unique declaration, but SQL
registration cannot discover it and refuses the request.

The isolated test overlay verifies effective driver host/port 15432, connected
database and app/migrator roles **before** creating its disposable database.
It uses existing governed-dataset helpers and two populated three-record
fixtures. For 1.5.0 it uploads a new immutable source revision and binds it
through the normal API; it does not edit stored metadata in place.

| Authored/stored schema | Unique registration HTTP | Records before/after |
|---|---|---|
| 1.4.0 positive control | 200 | 3 / 3 |
| 1.5.0 superset | **400** | 3 / 3 |

Both not-yet-ready write controls return 409. The assertion test **FAILS**
(4.950s Go), solely for the 1.5.0 registration expectation. This is a
fail-closed availability gap: a first-time 1.5.0 unique declaration cannot
start its claim build. It is not evidence of duplicates bypassing enforcement.
The record counts remain unchanged and fixture cleanup completes; this is
not a full customer-data migration/no-loss proof.

Requested coordinated fix: extend the latest guarded SQL registration contract
to the accepted integrity versions through a new additive migration, preserving
manager, source/layout, live authorization, generation and quota guards. Do not
edit pushed migration 00052. Include 1.5.0 registration/build/conflict/denial
tests, not only schema/compiler tests. Agree migration ordering first, as
recorded in A's handoff. No migration or implementation fix is applied in B.

## Executed verification

All database tests use slice A's owned `framework-merged-20261020` stack,
ports 15432/16379 and its ignored 0600 environment. The default `framework-m0`
database is not migrated, reset or used for B. See
[slice A](M9R_MERGED_REBASELINE_REPORT.md) for the harness, retained reader and
resource profile. Ignored B evidence is under `.local/merge-review-b-20261020/`.

The fresh functional chain exits **2**, 2298.676s wall time. `make check` and
unit stages pass; full integration completes in **2061.520s** with **438 PASS,
1 FAIL, 0 SKIP** (439 top-level groups). The worker-policy failure below stops
Make before its build target. A separately executed `make build` result is
recorded below; it does not turn the failed chain into a pass.

| Exact command or stage | Result |
|---|---|
| `git fetch origin` | PASS |
| `git fetch origin refs/heads/integration/m9r-m14:refs/remotes/origin/integration/m9r-m14` | PASS |
| `git merge --no-edit origin/integration/m9r-m14` | PASS, already up to date |
| `node .local/merged-rebaseline-20261020/run.mjs make check test build GOFLAGS=-v` | **FAIL**, exit 2; worker-policy statement timeout |
| `make check` stage in that chain | PASS: frozen-doc checksums, format, vet, module verification, frontend lint/typecheck |
| Unit stage in that chain | PASS: 27 Go race packages (7 cached), 9 Node tests, 126 frontend tests / 19 files |
| Full integration stage in that chain | **FAIL**, 438 pass / 1 fail / zero skips, including enabled retained-binary and actual owned-Redis restart tests |
| Build stage in that chain | **NOT RUN**, Make stops on integration failure |
| `node .local/merged-rebaseline-20261020/run.mjs make build` | PASS separately: eight Go binaries and TypeScript/Vite frontend build |
| `node .local/merge-review-b-20261020/launcher-probe.mjs` | **FAIL**, exit 1; B-01 missing required API/worker options; positive controls pass |
| `node .local/merge-review-b-20261020/actual-launcher-probe.mjs` | **FAIL**, exit 1; B-01 reproduced in real `with-env` child processes |
| `/workspace/Framwork/scripts/go.sh test -overlay=/workspace/Framwork/.local/merge-review-b-20261020/old-producer-overlay.json -run '^TestMergeReviewBExportOldIR$' -count=1 -v ./internal/compiler` (cwd ignored source archive of `60da6f2`) | PASS, 0.100s Go; old producer and its self-restore |
| `./scripts/go.sh run .local/merge-review-b-20261020/version-probe.go /workspace/Framwork/.local/merge-review-b-20261020/old-tool-1.3.ir.json` | **FAIL**, exit 1; B-02 unsupported compiler accepted in 1.5.0; also observes B-03 full-IR rejection and standalone-plan preservation |
| `node .local/merged-rebaseline-20261020/run.mjs node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -overlay=/workspace/Framwork/.local/merge-review-b-20261020/unique-probe-overlay.json -run '^TestMergeReviewBUniqueSchemaSuperset$' -count=1 -timeout=2m -v ./tests/integration` | **FAIL**, 4.950s Go; populated 1.4 control passes, 1.5 registration returns 400 (B-05) |
| `node .local/merge-review-b-20261020/preservation-check.mjs` | **FAIL**, exit 1 against older A timestamps; qualification below |
| `node .local/merge-review-b-20261020/preservation-check.mjs .local/merge-review-b-20261020/default-container-observation.json` | PASS at interim and final B checks; credential inputs, retained reader and observed default container metadata remain unchanged during the observation interval |
| `node .local/merged-rebaseline-20261020/run.mjs make docs-verify` | PASS after report edits: 84 retained architecture checksums and 43 V3 checksums |

Existing focused regressions executed within the functional gate include
`TestM12APIReleaseRoutesEndingInRolesAreNotRoleRoutes`,
`TestM11RFreezeRunsReadCommittedAndRefusesAnyOtherIsolation`,
`TestM9RTrafficPeerAndGlobalRejectBeforeSignatureOrBody`,
`TestM9RTrafficRechecksActualTokenExpiryAfterDatabaseAdmissionWait`,
`TestIndependentR5dOpaqueSnapshotProofRejectsAutocommitAndRawRR`,
`TestIntegrityDeterminismStorageAndImmutableReaders`,
`TestToolWorkflowLeavesExistingContractsUnchanged` and
`TestLegacyToolPlanRestoresByteForByte`. All pass. Names containing
“Independent” refer to existing test fixtures, not a fresh independent review.

Preservation qualification: the first B preservation command exits **1** when
compared with A's 09:41 UTC saved container observation. Default-stack container
IDs and restart counters still match, and root environment/ACL hashes and the
retained-reader checksum match. However, all three default start timestamps are
now approximately 10:52:44 UTC, earlier than B's 12:28 UTC gate launch. The older
saved timestamps are approximately 09:18:31 UTC. No default-stack lifecycle
command is issued in B; this observation does not establish the cause of the
earlier restart. Do not extend A's earlier “unchanged start times” observation
into a claim of uninterrupted uptime. A new B observation is retained for the
end-of-slice comparison. Container metadata and credential hashes alone are not
proof of unchanged database contents.

These are deliberately **NOT RUN again in B**: both browser suites, nine fuzz
targets, actual 600-second outage, eight reference capacity targets, traffic
binary probe and driver-panic diagnostic. A executed all of those against the
same unchanged product tree. Its aggregate-capacity **FAIL** (three HTTP p95
cases over 750ms) and injected driver-panic **FAIL** remain open; a green
functional gate in B would not clear them. No performance neutrality, causal
merge regression or production acceptance is claimed.

### Existing R7 diagnostic reproduced during the B gate

`TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` **FAILS** at
`tests/integration/m9r_worker_policy_test.go:158`: the bounded compensation
SELECT is canceled under the unchanged **2000ms** statement timeout
(SQLSTATE 57014). The case takes 5.34s overall, including setup. It uses
96 seeded receipts and seven scoped grants/63 scope nodes per capability.
Its subsequent EXPLAIN and slow-write lease-expiry assertions are not reached
in this invocation. No budget is increased and no replacement rerun is used.

The ledger already records this fixture's narrow headroom and a prior EXPLAIN
timeout. B now observes the initial SELECT timing out, rather than only its
instrumented EXPLAIN. This is evidence for the open R7 diagnostics work, not
proof that the merge introduced the cost. A's earlier full integration pass
remains historical evidence and does not make this failed B invocation green.
Diagnosis/fixing is outside B; carry it to the user-confirmed D1 order.

## Recovery and completion boundary

Only documentation is changed. Operational probes and old-source export
fixtures are ignored local artifacts, not runtime code or distributable builds.
No records, immutable IR, pinned plans, migrations, role grants or customer data
are changed to make the review pass. Preserve existing release bytes; do not
“repair” a historical artifact by relabeling or recompiling it.

Commit and push only `remediation/m9r-foundation` after recording the failed gate.
Do not push integration/main or open a PR. The milestone agent owns fixes in
its lane and integration re-verification. Stop after B; C is the next bounded
slice. R7 ordering needs the user's decision before D implementation.

## Durable probe sources

These are verification-only recipes. Save the following snippets under the
ignored `.local/merge-review-b-20261020/` directory using their named files.
They are not CI tests or implementation fixes. Run database probes only through
the owned-stack harness in the command table; the populated probe refuses an
effective driver port other than 15432 before fixture creation. The old-producer
export runs against an ignored `git archive 60da6f2` source extraction, not the
current compiler or a second Git checkout.

### launcher-probe.mjs

```js
import {runtimeEnv} from '../../scripts/runtime-env.mjs';
const source={TOOL_CATALOG_FILE:'/operator/catalog.json',TOOL_SECRETS_DIR:'/operator/secrets',TOOL_CA_FILE:'/operator/ca.pem',TOOL_ALLOW_INTERNAL:'true',AI_PROVIDER_FILE:'/operator/provider.json',SIGNIN_REDIS_URL:'signin-placeholder',API_DB_CONCURRENCY:'4',MIGRATION_DATABASE_URL:'operator-placeholder'};
let failed=false;
for(const [kind,required] of [['api',['TOOL_CATALOG_FILE','AI_PROVIDER_FILE']],['worker',['TOOL_CATALOG_FILE','TOOL_SECRETS_DIR','TOOL_CA_FILE','TOOL_ALLOW_INTERNAL']]]){
 const child=runtimeEnv(source,kind);
 const missing=required.filter(k=>child[k]!==source[k]);
 console.log(`${kind}: required_configuration_preserved=${missing.length===0} missing=${missing.join(',')} operator_credential_absent=${!Object.hasOwn(child,'MIGRATION_DATABASE_URL')}`);
 if(missing.length)failed=true;
}
const api=runtimeEnv(source,'api');
console.log(`positive_control: signin=${api.SIGNIN_REDIS_URL===source.SIGNIN_REDIS_URL} admission=${api.API_DB_CONCURRENCY===source.API_DB_CONCURRENCY}`);
process.exitCode=failed?1:0;
```

### child-env-probe.mjs

```js
const kind=process.argv.at(-1).split('/').at(-1);
const keys=kind==='api'?['TOOL_CATALOG_FILE','AI_PROVIDER_FILE']:['TOOL_CATALOG_FILE','TOOL_SECRETS_DIR','TOOL_CA_FILE','TOOL_ALLOW_INTERNAL'];
const missing=keys.filter(k=>!process.env[k]);
console.log(`${kind}: launcher_child_missing=${missing.join(',')}`);
process.exitCode=missing.length?1:0;
```

### actual-launcher-probe.mjs

```js
import {spawnSync} from 'node:child_process';
const env={...process.env,TOOL_CATALOG_FILE:'/operator/catalog.json',TOOL_SECRETS_DIR:'/operator/secrets',TOOL_CA_FILE:'/operator/ca.pem',TOOL_ALLOW_INTERNAL:'true',AI_PROVIDER_FILE:'/operator/provider.json'};
let failed=false;for(const kind of ['api','worker']){const r=spawnSync(process.execPath,['scripts/with-env.mjs',process.execPath,'.local/merge-review-b-20261020/child-env-probe.mjs',`./cmd/${kind}`],{env,stdio:'inherit'});if(r.status!==0)failed=true;}process.exitCode=failed?1:0;
```

### version-probe.go

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"os"
)

func main() {
	raw, e := os.ReadFile("contracts/examples/contacts-draft-v1.json")
	if e != nil {
		panic("fixture unavailable")
	}
	v, e := metadata.NewValidator()
	if e != nil {
		panic("validator unavailable")
	}
	failed := false
	for _, version := range []string{"1.4.0", "1.5.0"} {
		var d metadata.Document
		if json.Unmarshal(raw, &d) != nil {
			panic("fixture decode")
		}
		d.SchemaVersion = version
		defs := []metadata.Definition{}
		for _, x := range d.Definitions {
			if x.Rule != nil || x.Workflow != nil {
				continue
			}
			x.SchemaVersion = version
			if x.ID == "field.contact.name" {
				x.Field.Unique = true
			}
			defs = append(defs, x)
		}
		d.Definitions = defs
		b, _ := json.Marshal(d)
		s, e := v.Parse(b)
		if e != nil {
			panic("fixture validation")
		}
		p, ds := compiler.Compile(s, compiler.PlatformCatalog())
		if !p.Valid() {
			fmt.Println("fixture diagnostics", ds)
			os.Exit(2)
		}
		_, normal := compiler.Restore(p.JSON(), p.Hash(), compiler.SupportedIRVersions())
		ir := p.IR()
		ir.CompilerVersion = "unsupported-compiler"
		b, _ = json.Marshal(ir)
		h := sha256.Sum256(b)
		_, wrong := compiler.Restore(b, hex.EncodeToString(h[:]), compiler.SupportedIRVersions())
		fmt.Printf("ir=%s normal_restore=%t mismatched_compiler_rejected=%t unique=%t\n", version, normal == nil, wrong != nil, compiler.HasUniqueConstraints(p))
		if wrong == nil {
			failed = true
		}
	}
	if len(os.Args) > 1 {
		b, e := os.ReadFile(os.Args[1])
		if e != nil {
			panic("legacy fixture unavailable")
		}
		h := sha256.Sum256(b)
		_, e = compiler.Restore(b, hex.EncodeToString(h[:]), compiler.SupportedIRVersions())
		var ir compiler.IR
		_ = json.Unmarshal(b, &ir)
		fmt.Printf("old_milestone_ir=%s compiler=%s plans=%d unchanged_current_restore=%t\n", ir.IRVersion, ir.CompilerVersion, len(ir.Workflows), e == nil)
		if len(ir.Workflows) > 0 {
			planRaw, _ := json.Marshal(ir.Workflows[0])
			plan, pe := workflow.Restore(planRaw, workflow.Hash(planRaw))
			fmt.Printf("old_standalone_plan_dsl=%s restore=%t bytes_preserved=%t\n", ir.Workflows[0].DSLVersion, pe == nil, pe == nil && bytes.Equal(plan.JSON(), planRaw))
		}
	}
	if failed {
		os.Exit(1)
	}
}
```

### export-old-ir_test.go

```go
package compiler

import (
	"os"
	"testing"
)

func TestMergeReviewBExportOldIR(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.3.json")
	if err != nil {
		t.Fatal("fixture unavailable")
	}
	p, ds := Compile(source(t, raw), toolCompilerCatalog(testToolCatalog(t)))
	if !p.Valid() || len(ds) != 0 {
		t.Fatal("old producer failed", ds)
	}
	if _, err = Restore(p.JSON(), p.Hash(), SupportedIRVersions()); err != nil {
		t.Fatal("old self restore failed")
	}
	if err = os.WriteFile("/workspace/Framwork/.local/merge-review-b-20261020/old-tool-1.3.ir.json", p.JSON(), 0600); err != nil {
		t.Fatal("export failed")
	}
	t.Logf("producer_ir=%s compiler=%s hash=%s plans=%d", p.IR().IRVersion, p.IR().CompilerVersion, p.Hash(), len(p.IR().Workflows))
}
```

### unique-schema-probe_test.go

```go
//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
)

func TestMergeReviewBUniqueSchemaSuperset(t *testing.T) {
	for _, binding := range []struct{ key, role string }{{"DATABASE_URL", "framework_app"}, {"MIGRATION_DATABASE_URL", "framework_migrator"}} {
		cfg, err := pgx.ParseConfig(os.Getenv(binding.key))
		if err != nil || cfg.Host != "127.0.0.1" || cfg.Port != 15432 {
			t.Fatal("owned driver target required before fixtures")
		}
		db, err := postgres.Open(os.Getenv(binding.key))
		if err != nil {
			t.Fatal("target preflight unavailable")
		}
		var name, role string
		err = db.QueryRowContext(context.Background(), `SELECT current_database(),session_user`).Scan(&name, &role)
		db.Close()
		if err != nil || name != cfg.Database || role != binding.role {
			t.Fatal("connected target mismatch before fixtures")
		}
	}
	t.Log("owned cluster port/driver/database/roles verified before fixture creation")
	m8Database(t)
	for _, version := range []string{"1.4.0", "1.5.0"} {
		t.Run(version, func(t *testing.T) {
			f, h, base, token, app, _ := uniqueFixture(t, 3, false)
			scalarReviewTarget(t, f)
			if version == "1.5.0" {
				var raw []byte
				scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=3`, f.tenantA, app).Scan(&raw))
				var doc map[string]any
				if json.Unmarshal(raw, &doc) != nil {
					t.Fatal("fixture decode")
				}
				doc["schema_version"] = version
				for _, item := range doc["definitions"].([]any) {
					item.(map[string]any)["schema_version"] = version
				}
				raw, _ = json.Marshal(doc)
				root := "/v1/tenants/" + f.tenantA
				if w := m3Call(h, "POST", root+"/metadata/"+app+"/versions", token, 3, raw); w.Code != 201 {
					t.Fatalf("schema1.5 authoring status=%d", w.Code)
				}
				base = uniqueReviewBind(t, h, base, token, root+"/preview/"+app+"/versions/4", 2)
			}
			rows := choicePages(t, h, base, token, m5Query())
			if len(rows) != 3 {
				t.Fatal("populated fixture required")
			}
			before := m5Call(h, base+"/commands", token, map[string]any{"operation": "create", "command_key": uuid(t), "values": uniqueValues("not-ready")})
			if before.Code != 409 {
				t.Fatalf("not-ready write did not fail closed: status=%d", before.Code)
			}
			path := base + "/unique-constraints/field.contact.memo0/register"
			w := m5Call(h, path, token, map[string]any{"expected_generation": 0})
			var body map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			code := ""
			if err, ok := body["error"].(map[string]any); ok {
				code, _ = err["code"].(string)
			}
			after := choicePages(t, h, base, token, m5Query())
			var storedVersion string
			scalarSQL(t, f.operator.QueryRow(`SELECT schema_version FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 ORDER BY revision DESC LIMIT 1`, f.tenantA, app).Scan(&storedVersion))
			t.Logf("schema=%s stored_schema=%s populated_before=%d populated_after=%d registration_status=%d public_error_code=%s", version, storedVersion, len(rows), len(after), w.Code, code)
			if len(after) != 3 {
				t.Error("registration attempt changed records")
			}
			if w.Code != 200 {
				t.Errorf("schema superset cannot register declared unique field: status=%d", w.Code)
			}
			if !strings.Contains(base, "/datasets/") {
				t.Fatal("not a governed dataset fixture")
			}
		})
	}
}
```

Create the overlay files from the repository root (after extracting the old
source archive and saving the snippets):

```js
import fs from "node:fs";
const root = process.cwd();
const dir = root + "/.local/merge-review-b-20261020";
const overlays = {
  "old-producer-overlay.json": [
    dir + "/old-milestone-source-60da6f2/internal/compiler/merge_review_b_export_test.go",
    dir + "/export-old-ir_test.go"
  ],
  "unique-probe-overlay.json": [
    root + "/tests/integration/merge_review_b_test.go",
    dir + "/unique-schema-probe_test.go"
  ]
};
for (const [name, [target, source]] of Object.entries(overlays)) {
  fs.writeFileSync(dir + "/" + name, JSON.stringify({Replace: {[target]: source}}));
}
```

The captured output and expected assertions above remain failed evidence until
coordinated fixes land and are verified. Merely regenerating a hash or observing
a standalone plan restore is not a fix.
