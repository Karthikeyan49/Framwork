# M13 independent adversarial review

Reviewer: "reviewer" agent, wave 3 (independent of the M10-M13 builders; it took over the worktree of the
previous reviewer, which was cut off by an API session limit). Branch `review/m13-final`, subject commit
`e6b7877`. Subject: ADR039 (approval and promotion control plane), migration `00049`,
`internal/promotion` (`decisions.go`, `release_store.go`, `store.go`, `staleness.go`, `bundle2.go`),
`internal/platform/access/{promote,releases}.go`. Product code is not modified by this review.

STATUS: M13 REVIEW COMPLETE for the attacks listed; second and third job pending. Verdict: PASS WITH FINDINGS (2 MEDIUM, 4 LOW, 8 INFO; no HIGH). The database
authority floor held against every direct attack below; the findings are concurrency and gate-completeness
defects of the control plane.

## Progress

| time (UTC) | step | state |
|---|---|---|
| 2026-10-18 15:55 | briefs, AGENTS.md, ADR039, migration 00049 read (previous reviewer) | done |
| 2026-10-19 05:28 | takeover: briefs, ADR039, migrations 48/49, `promotion/*.go`, `access/{promote,releases,access,preview}.go` read | done |
| 05:35 | inherited probes (authority A1-A9, state S1-S5, API P1-P9, migration M1-M2) compiled and run | done |
| 05:40 | own probes: multi-hop rollback, function surface, demoted approver, audit of port refusals, note characters | done |
| 05:50 | cross-tenant / forged-GUC raw probe, viewer flood (availability), `worker_grants_safe`, goose Down, operator guards, large stress, HTTP race | done |
| 05:46 | builder's own `TestM13*` suites re-run on the pinned commit (31 PASS) | done |
| | second job: foundation finding-status audit (`/home/user/review-status`) | pending |
| | third job: re-verification of the M10 review fixes and the verifier fixes | pending |

Method. Probe tests lived in untracked `zz_review_*_test.go` files, kept outside the repository while
committing; the seven reproductions are in Appendix A. Every probe ran against a disposable database
`m8_delivery_<random>` that the fixture creates from this worktree's migrations on the MAIN stack (`dbrun`,
`nice -n 10`, one process at a time); the shared MAIN database itself was never written. Run command:

    /home/user/bin/dbrun nice -n 10 node scripts/with-env.mjs ./scripts/go.sh test -tags=integration \
      -count=1 -timeout 20m -v -run '<regex>' ./tests/integration

## Findings

Severity: HIGH exploitable violation of an authority/integrity invariant; MEDIUM realistic integrity or
availability defect or documented-contract breach; LOW hardening or documentation.

### M13R-01 MEDIUM - an acknowledged rejection does not stop an activation whose transaction began earlier

Contract: ADR039 section 3 "Rejection is bound to the exact hash and permanent: approve and activate refuse
a rejected hash"; section 8 lists the races that are safe and does not list this one.

Evidence: every M13 handler runs in one REPEATABLE READ transaction (`postgres/transaction.go:93`) whose
snapshot is taken before `LoadCandidate`, staleness, the policy read and the port call. `activate_release`
(`00049:175`) tests `EXISTS(... release_rejections ...)` against that snapshot and takes no lock that
`reject_release` conflicts with, so a rejection that commits after the snapshot is invisible and there is no
write-write conflict (write skew, no 40001). The window is the whole request (0.4 s idle, 2 to 3 s measured
under load, bounded by the 3 s envelope), not a nanosecond race.

Executed: Appendix A `TestZZReproM13R01...`: T1 begins and takes its snapshot; T2 runs `reject_release` and
commits (the API answers 201); T1 then calls `activate_release` and commits: SUCCESS, the pointer sits on the
rejected hash (`rejections=1`). With nothing but HTTP (`TestZZReproM13R01b...`): an approved candidate, a reject
request started at t0 and an activate request started 40, 80, 120 and 160 ms later: in all 4 of 4 trials the reject
was acknowledged 201 first and the activation was acknowledged 200 afterwards (87 to 226 ms later); a start offset of
200 ms or more is refused (409). So the window is about the length of the reject request, not a nanosecond race.
The candidate view then reports `status=REJECTED active=true`, approve and activate answer 409
`candidate_rejected`, and an operator can still roll away from it (and never back: `rollback_target_rejected`).
The same skew lets `approve_release` record an approval after a committed rejection (S1c, harmless: activation
re-checks). Related, weaker, same class: disabling a user account (`iam.users.active`) takes no lock, so an
activation that already holds its snapshot still counts that approver (S2c); membership changes are safe
(`FOR SHARE`, S2a/S2b).

Same class, different port: M12 API review F8 ("the base is snapshot-current, not commit-current", tracked). This finding
is the M13 consequence for the veto.

Impact: a veto acknowledged to its author can be overtaken by an activation that started up to ~3 s earlier,
leaving a REJECTED release ACTIVE. Recoverable by rollback; nothing serves the pointer yet (ADR039 section 10).
Fix direction (not applied): make activation and rejection write-conflict, so a stale REPEATABLE READ transaction
aborts with 40001 (for example both insert or update a row keyed by the hash in a per-hash "finality" table), or run the
M13 transactions SERIALIZABLE. A lock-only fence does NOT work: the R5a review's negative control
(`M9R_R5A_INDEPENDENT_REVIEW.md:75`) shows a `FOR UPDATE` fence lets both stale transactions commit, while migration 38's
real tenant-row UPDATE forces them to abort; that is the project's own precedent for this class.

### M13R-02 MEDIUM - the rollback class gate reads only the current release, so a rollback may cross an irreversible release without a plan

Contract: ADR039 section 5 step 3 reads `Risk.Rollback` "of the current release"; AGENTS.md "Metadata rollback
cannot undo incompatible data changes or external actions"; the gate exists so that a pointer move over such
changes needs an acknowledged plan.

Evidence: `rollback_release` (`00049:225-230`) and the handler (`promote.go:582`, `rollbackClass`) read the class
of the current release only; the target may be any release in history (`00049:219`), not only the previous one.

Executed: Appendix A `TestZZReproM13R02...` through the HTTP API: c1 first release (HIGH, ADDITIVE,
DATA_NOT_REVERSIBLE), c2 migration (CRITICAL, TRANSFORM, DATA_NOT_REVERSIBLE, two approvers), c3 label-only change
built on c2 (LOW, EQUAL, POINTER_SAFE). `POST .../releases/rollback {target:c1, expected_current:c3,
expected_generation:3}` with NO plan answers 200 and writes a `ROLLBACK` history row without a plan hash. Rolling
back c2 itself needs a plan; skipping over it does not.

Impact: a two-step rollback (or a plain rollback after one cosmetic release) silently drops the safety check for
every irreversible release in between, and the history row records no acknowledgement. No data plane exists
today, so this is a control-plane defect that becomes data-affecting when one is added.
Fix direction: gate on the worst `Rollback` class over the history entries between target and current (`seq`
range), or refuse non-adjacent targets without a plan.

### M13R-03 LOW - an approver who lost release authority still counts toward quorum

Evidence: `release_approval_count` (`00049:111-118`) counts approvers who are active, non-`viewer` members
(documented, ADR039 section 4 step 3). The handler has no matching Go check at activation (`promote.go:525-534`
counts every recorded approver that the separation roles do not bar).
Executed: Appendix A `TestZZReproM13R03...`: dave (admin) approves a HIGH candidate; dave is changed to role
`member`; dave's own approve now answers 403 `forbidden`; erin's activation still answers 200 on dave's approval.
Removed (`active=false`) and viewer-demoted approvers do not count (held).
Impact: LOW because the text says "non-viewer"; but the earlier admin-only ports (migrations 31, 36, 37) check
`role IN ('owner','admin')`, while `member` and every custom role pass the M12/M13 floor (`release_actor_ok`).

### M13R-04 LOW - "body keys must be spelled exactly" is not true for U+017F and U+212A

Contract: ADR039 amendment 7 (exact lower-case spelling) and 10 (`DecodeJSON` rejects keys that differ only by
case folding).
Evidence: `lowercaseKeys` (`promote.go:115-131`) tests `key != strings.ToLower(key)`; U+017F (long s) is its own
lower case, but `encoding/json` folds it to `s`. `DecodeJSON` rejects only a pair of keys that fold to each other
(the duplicate-alias variant is refused, held), not a single aliased key.
Executed: Appendix A `TestZZReproM13R04...`: an approve body whose key is `artifact_ha` + U+017F + `h` (sent as the
JSON escape for U+017F) approves (201); the key `rea` + U+017F + `on` rejects (201). The same decoder serves every strict body route of M12 and M13 (`readDatasetRequest`).
Tracked in part: the M12 API review F3 records "a single aliased key without a duplicate ... is still accepted" as a
residual of the M12 envelopes. It is recorded again because ADR039 amendment 7 and the `promote.go` comment state the
opposite for the M13 bodies.

Impact: a request that any strict JSON reader sees as an unknown field is accepted as the field (request
inspection, signing and audit tooling that matches key names literally will not see it). No authority field is
reachable this way (hash binding still applies).

### M13R-05 LOW - rejection note validation misses Unicode directional and invisible marks

Contract: ADR039 section 2 and `ValidNote` ("no bidirectional ... formatting that could disguise the text").
Evidence: the Go check and the database CHECK/regex cover U+0001-001F, U+007F-009F, U+2028/2029, U+202A-202E and
U+2066-2069 only. Executed (`TestZZReproM13R05...`): notes containing U+200E, U+200F, U+061C (bidi marks), U+200B and
U+FEFF are stored (201) and returned to every member in `rejections[].note`.
Impact: display spoofing in a future UI. The note is never copied to audit (held: own probe `TestZZAuditAndNotes`,
0 audit rows contain the marker).

### M13R-06 LOW - the recorded runtime components are never compared with the running binary

Tracked: M12 verifier review F8, last bullet ("`RuntimeContract.Components` is only self-hashed"). Recorded again because
M13 is the first consumer that approves and activates on the strength of "no recompile after approval" and because the
DSL 2.2.0 change makes the case concrete.

Evidence: `restoreV2` (`bundle2.go:507`) checks only that `ComponentsHash` equals the hash of the body's own
components document; `Staleness` (`staleness.go:109-117`) judges only `IRVersion`. ADR038 section 2 and
`internal/promotion/README.md` present the components hash as part of what a candidate is built against.
Executed: `TestZZReproM13R06...`: a candidate recorded for another runtime component set (no workflow DSL 2.2.0,
supported IR `9.9.9`, `tool-catalog-v0`; hash recomputed) restores, is approvable (201) and activatable.
Effect of the 2.2.0 change in practice: candidates staged by the pre-fix binary remain approvable after the upgrade;
what keeps them honest is only that `Restore` recomputes verification with the CURRENT verifier and requires
`VerifierVersion` equality (a verifier bump fails them closed as `candidate_failed_verification`, not as stale).
Documentation/hardening: say so, or add `UNSUPPORTED_RUNTIME` for a components-hash mismatch.

### Informational (documented or platform-wide; recorded so nobody rediscovers them)

- I-1 Legacy (M6R) releases are a one-way door: after the first M13 activation over a legacy pointer the legacy
  release is not a rollback target (409 `rollback_target_not_released`, port 55000) and nobody, the operator
  included, can add its missing history row (55000 "must extend its previous entry"). Documented (ADR039 section 2);
  the consequence (no way back to the pre-M13 release) deserves a runbook line.
- I-2 `status=APPROVED` counts recorded, floor-eligible approvals and cannot see membership, so a candidate with only
  a legacy viewer approval shows APPROVED while activation answers 409 (inherited probe M2). Documented in
  `DeriveStatus`; do not read it as "activatable".
- I-3 A candidate that fails `Restore` cannot be rejected over HTTP (409 `candidate_failed_verification`), though the
  port accepts the veto. It cannot be approved or activated either.
- I-4 A 503 (timeout, lost audit append) leaves no audit row and no `Retry-After` header (platform-wide, no contract
  names it); the business transaction rolls back in full (probe P5).
- I-5 Identity is `(issuer, subject)`; one person with two subjects is two members to every separation rule.
  Consistent with residual 1 of ADR039; separation of duties is only as strong as that.
- I-6 `plan_hash` of a rollback is a 64-hex label the caller supplies; nothing checks it against a stored plan (an
  all-zero or repeated-digit hash is accepted). The "plan" is an attestation of three booleans, as the ADR says.
- I-7 Policy `release-risk-v1` excludes only requester, acceptance author and sandbox freezer. The SOURCE author (and
  the requirement decider) may approve and activate a HIGH or CRITICAL release that someone else requested. A design
  choice of ADR038 section 3, not a defect; recorded because the acceptance author is separated from the source author
  at build time but the approver is not.
- I-8 Rollback never re-runs staleness (ADR039 section 5, documented): a release whose tool pin was withdrawn or denied
  for the tenant, or whose policy bundle changed, can be rolled back to. The M10 execution-time guards still apply and
  no data plane serves the pointer yet.

## Attacks that held (each executed; probe ids refer to the inherited and own probe sets)

- A1/A2 privileges. `pg_roles`: no superuser/createrole/bypassrls among `framework_*`; `framework_app` is a member
  of no role; for PUBLIC, app, worker and dispatcher on the four authority tables `has_table_privilege` and
  `has_any_column_privilege` show no INSERT/UPDATE/DELETE/TRUNCATE/REFERENCES/TRIGGER (table or column level);
  `relacl` of each table is `owner=arwdDxtm, framework_app=r` only. HELD.
- A3 `pg_default_acl` 0 rows, `pg_parameter_acl` 0 rows; `framework_app` has no SET privilege on
  `session_replication_role`, `row_security`, `role`, `session_authorization`, `search_path`, `check_function_bodies`.
- A4 raw `framework_app` session, 38 statements: SET ROLE (schema_owner, migrator, worker, dispatcher, postgres),
  SET SESSION AUTHORIZATION, `session_replication_role=replica`, DISABLE TRIGGER (ALL, USER, by name), DISABLE/NO
  FORCE RLS, DROP/CREATE TRIGGER, DROP/CREATE POLICY, ALTER/CREATE OR REPLACE/DROP FUNCTION on the ports, CREATE
  TABLE/FUNCTION/EXTENSION/EVENT TRIGGER, GRANT role, ALTER ROLE, COPY, INSERT..SELECT, UPDATE, DELETE: all 42501
  (LOCK 25P01). Accepted and harmless: `SET row_security = off` (RLS then errors instead of bypassing) and
  `GRANT INSERT ... TO framework_app` (no-op without grant option; privileges re-read unchanged).
- A5 callable SECURITY DEFINER surface of `framework_app`: all owned by `framework_schema_owner`, all
  `search_path=pg_catalog, pg_temp`, none with dynamic SQL, none public; only the four ports write the authority
  tables (`stage_candidate` only reads the pointer). The internal definers (`release_approval_count`,
  `release_actor_ok`, `release_pointer_recorded`, the trigger functions) are not executable by app, worker or
  dispatcher (`has_function_privilege`; direct calls answer 42501).
- A6 no view or rule depends on the authority tables; every trigger is `ENABLE ORIGIN`.
- A7/A8 per-transaction attacker settings (`search_path=pg_temp,public`, `standard_conforming_strings=off`,
  `check_function_bodies=off`, `row_security=off`, `plpgsql.variable_conflict`, `session_replication_role`) and
  temp-schema shadowing (tables, operators, functions, types): the setting is refused (42501) or the port still
  refuses the requester (42501 DETAIL requester). `framework_app` has no TEMP privilege.
- A9 a bound member reads exactly its own `iam.users` row (issuer/subject of others unreadable); `audit.entries` is
  unreadable by the app role.
- Worker and dispatcher sessions: SELECT on `release_history`, `active_releases`, `release_approvals` and EXECUTE of
  the ports: 42501.
- S2a/S2b approver demoted while the activation holds the quorum: the demotion waits for the activation
  (`FOR SHARE`); a demotion committed after the activation snapshot fails the activation with 40001. HELD.
- S3 ABA: a candidate bound to (hash, generation 1) stays stale after the pointer returns to the same hash at
  generation 6 (55000 with both the old and the new generation). HELD.
- S4 mixed parallel approve/reject/activate/rollback/stage (8 workers x 30 operations): only 55000/40001 refusals, no
  40P01/23505/XX000; history is a dense chain ending at the pointer; every ACTIVATE has an approval row.
- P2 strictness table, 23 bodies: duplicate key, case-folded duplicate, upper-case key, unknown field, wrong type,
  null, array/string root, trailing object/garbage, BOM, NUL, invalid UTF-8, deep nesting, empty, over 1 MiB, hash
  case/whitespace/newline: all 400 (M13R-04 is the one miss). Content type: only `application/json` (+parameters).
- P3/P6 wrong verbs, trailing slash, upper-case hash, dot segments, double slash, encoded slash and unknown verbs
  never reach a handler; with preview disabled all seven routes are absent (413/404).
- P4 audit: every outcome has exactly one row keyed by the response `X-Request-ID` with tenant, actor, decision id,
  policy version `m13-native-v10`: allowed (`release.approved`, `release.activated`), replay
  (`release.approval_replayed`), policy deny (`releases.approve`, allowed=false), refusals
  (`releases.<action>.rejected`), 401 (`authentication.denied`). Own probe `TestZZAuditAndNotes`: a PORT-level refusal
  (409 after rollback) is audited once as `releases.activate.rejected`, allowed=false, with the request id.
- P5 a failing audit append gives 503 and writes no approval, pointer move or history row. P7 a lock wait beyond
  `lock_timeout` answers 409 `release_conflict` with no pointer move.
- P8 no collected response body (about 130) contains SQL, driver, role, stack or file text; separation refusals name
  the role, never an actor id.
- M1/M2 legacy-shaped rows (pointer at generation 5, no history; approvals by the requester and a viewer): the first
  M13 activation over the legacy pointer works (history seq 1, generation 6, previous = legacy hash); legacy approvals
  by the requester or a viewer do not satisfy a HIGH quorum (55000).
- Quorum: one approver twice is one approval; CRITICAL needs two distinct eligible approvers (builder suites, S4).
- X1-X5 cross-tenant (own probe `TestZZCrossTenantRaw`, member `zed` of tenant B only): bound to tenant A, approve /
  reject / activate / rollback of tenant A's hashes answer 42501; bound to its own tenant B, tenant A's hash is P0002;
  forged settings (A's actor id with B's subject, B's id with A's subject, random tenant, empty tenant) answer 42501;
  bound either way, zed sees 0 rows of tenant A in all six release tables (RLS); over HTTP approve / history / detail
  answer 403; A's hash under another application of tenant A is P0002 (no existence oracle between apps).
- OG operator-level guards (`TestZZOperatorGuards`): TRUNCATE, DELETE, UPDATE and a hand-written INSERT into every
  release table are refused for the migration role and for the superuser (immutability and recording triggers).
- W `worker_grants_safe()` (`TestZZWorkerGrantsSafe`): true on a fresh database; false for each of table SELECT,
  column SELECT, column INSERT, REFERENCES, TRIGGER, a PUBLIC TRUNCATE and a column REFERENCES grant on the new
  tables; true again after revoking. The table list of each successive definition is a superset of the previous one.
- D goose Down of 00049 (`TestZZGooseDown`): refused ("forward recovery only"), objects and version 49 stay, Up
  proceeds to 00050 afterwards.
- S5 16 workers x 80 mixed operations (1.65 s): 35 approvals, 25 rejections, 15 rollbacks, 3 activations won, 273+45
  55000 and 49 40001 refusals, no 40P01/23505/XX000; history dense and chained; every ACTIVATE row has its eligible
  quorum recorded. (Candidates go stale quickly in that workload, so HIGH/CRITICAL activations are rare in S5; the
  CRITICAL path is covered by `TestM13APICriticalQuorumOnATransformRelease` and the demotion races S2a/S2b.)

## Residual assessment (documented residuals, ADR039 "Consequences")

- Identity bound by API-set settings: confirmed as described. `authenticated_actor()` requires `app.actor_id`, issuer
  AND subject to match one active `iam.users` row, and a member cannot read other users' issuer/subject (A9), so
  impersonation needs the victim's issuer and subject, not merely the user id (which every member can read). Not
  worse than stated.
- Forged bodies stopped only by `LoadCandidate`: confirmed; every M13 handler goes through `loadPromotable`. The only
  other body readers are `Store.Release` (restore only; used for the rollback class and legacy views; fail-closed to
  "plan required") and the operator tool `CheckActiveCompatibility`; `Store.Active` has no production caller.
  See M13R-06 for the components gap.
- 3 s envelope: confirmed; availability measurement below.
- Non-EQUAL storage activation without a data plane: confirmed (history records the class; nothing serves it).

## Test baseline and measurements

- Builder suites on the pinned commit `e6b7877` (my run, alone, `nice -n 10`, MAIN stack disposable databases):
  `-run TestM13` 31 tests PASS in 87.8 s (ports 6, API 8, adversarial 16, fixture 1), no flake observed.
  The seven reproductions of Appendix A FAIL on the same tree (about 25 s), which is the point: the builder suites do not
  exercise these cases.
- Availability (documented residual 3; also tracked as M12 API review F9, MEDIUM, "a viewer can exhaust the API"): one candidate read (restore + recompute + staleness) 0.40 s on an idle host.
  With 24 concurrent viewer read loops (a viewer holds `candidates.read`) the admin approve latency rose to about
  0.8 to 1.1 s (replays and the activation), 0 of 9 requests timed out (503). On a loaded host the same measurement
  earlier showed 2.0 to 2.7 s with 12 loops and still no 503, i.e. close to the 3 s envelope. The residual is accurate;
  the amplification by read-only members (cost of a read equals cost of an approval) is not stated in ADR039.
- Strictness and audit results were taken from the inherited API probe (run in full, one run of 22 s); its failing
  assertions in that run were probe-order artefacts (an earlier approve from the same loop had already satisfied the
  quorum, and "approve with a query string" is a legitimate approve), not product defects; the real misses are
  M13R-04 only.

## Not covered

- Tool-catalog staleness through the HTTP handlers (a candidate whose IR pins a tool that is then changed, removed or
  denied for the tenant): the closure in `releases.go:466-477` was read and the pure `Staleness` unit tests exist, but
  no tool-pinned candidate was built (it needs the M10 provider fixture); NOT RUN.
- Sandbox evidence "unfrozen" staleness (evidence is immutable, so only reachable by forging the lookup): NOT RUN.
- Real production-scale timing without the race detector and on a quiet host; the envelope numbers above are from a
  shared 4-core host.
- Compromised-API scenarios beyond those the builder's `TestM13AdversarialCompromisedAPI*` and A7/A8 cover: a process
  that can run arbitrary SQL as `framework_app` is bounded by the floors verified here, and by nothing else.
- Operator/superuser abuse (disabling triggers as the table owner, `session_replication_role=replica`): out of scope by
  the AGENTS.md threat model; the legacy-row rehearsal uses that power openly.
- Multi-region, backup/restore of release history, and the UI: not present in the subject.
- The M10-M12 code that M13 builds on was read only where M13 depends on it (stage port, risk, `Restore`).

## Appendix A - reproductions

Copy the block to `tests/integration/zz_review_m13_repro_test.go`, run, delete. Each test FAILS while its defect
exists (executed 2026-10-19 on `e6b7877`: all seven FAIL). The file is ASCII only.

    /home/user/bin/dbrun nice -n 10 node scripts/with-env.mjs ./scripts/go.sh test -tags=integration \
      -count=1 -run 'TestZZRepro' ./tests/integration

```go
//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
)

// reproHold runs query as subject in tenant A: it takes the REPEATABLE READ snapshot,
// reports "ready", waits for "proceed", runs the query and commits.
func (e *m13Env) reproHold(subject string, ready chan<- struct{}, proceed <-chan struct{}, query string, args ...any) error {
	return postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, subject, e.f.tenantA); err != nil {
			return err
		}
		ready <- struct{}{}
		<-proceed
		var dst any
		return q.QueryRowContext(ctx, query, args...).Scan(&dst)
	})
}

// M13R-01: a rejection that commits after an activation transaction took its snapshot, and
// before that transaction calls the port, does not stop the activation.
func TestZZReproM13R01RejectedHashActivatedByStaleSnapshot(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	y, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.approvePort(e.dave.subject, y); err != nil {
		t.Fatal(err)
	}
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.reproHold(e.erin.subject, ready, proceed, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, y, "", int64(0))
	}()
	<-ready // the activation transaction exists (its snapshot predates the rejection)
	if _, err = e.rejectPort(e.f.bobSubject, y, "RISK_UNACCEPTABLE", "veto"); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	aerr := <-done
	hash, _, _ := e.activePointer()
	if aerr == nil && hash == y {
		t.Errorf("M13R-01: the release pointer moved onto a hash whose rejection had committed before the activation committed (rejections=%d)", len(e.rejectionsOf(y)))
	}
}

// M13R-01 over plain HTTP: reject and activate requests that overlap by a few hundred
// milliseconds. The rejection is acknowledged (201) first and the activation still succeeds.
func TestZZReproM13R01bHTTPOverlap(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	overtaken := 0
	for k, delay := range []time.Duration{40, 80, 120, 160} {
		base, gen, _ := e.activePointer()
		c, _ := e.build("500", m13Relabel(fmt.Sprintf("Race %d", k)), e.carol)
		m13Code(t, "approve", e.httpApprove(e.dave.token, c), 201)
		var wg sync.WaitGroup
		var rCode, aCode int
		wg.Add(2)
		go func() { defer wg.Done(); rCode = e.httpReject(e.bob, c, "RISK_UNACCEPTABLE", "veto").Code }()
		time.Sleep(delay * time.Millisecond)
		go func() { defer wg.Done(); aCode = e.httpActivate(e.erin.token, c, base, gen).Code }()
		wg.Wait()
		if hash, _, _ := e.activePointer(); rCode == 201 && aCode == 200 && hash == c {
			overtaken++
		}
	}
	if overtaken > 0 {
		t.Errorf("M13R-01b: %d of 4 overlapping reject/activate pairs ended with the rejected hash active", overtaken)
	}
}

// M13R-02: the rollback class gate reads only the CURRENT release, so a rollback that crosses an
// earlier DATA_NOT_REVERSIBLE release needs no acknowledged plan.
func TestZZReproM13R02RollbackAcrossIrreversibleRelease(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	required := func(on bool) func(*metadata.Document) {
		return func(d *metadata.Document) {
			for i := range d.Definitions {
				if d.Definitions[i].ID == "field.order.note" && d.Definitions[i].Field != nil {
					d.Definitions[i].Field.Required = on
				}
			}
		}
	}
	c1, _ := e.build("500", required(true), e.carol)
	m13Code(t, "approve c1", e.httpApprove(e.dave.token, c1), 201)
	m13Code(t, "activate c1", e.httpActivate(e.erin.token, c1, "", 0), 200)
	c2 := m13apiBuildMigration(t, e, required(false), e.carol) // CRITICAL, TRANSFORM, DATA_NOT_REVERSIBLE
	m13Code(t, "approve c2 dave", e.httpApprove(e.dave.token, c2), 201)
	m13Code(t, "approve c2 erin", e.httpApprove(e.erin.token, c2), 201)
	m13Code(t, "activate c2", e.httpActivate(e.dave.token, c2, c1, 1), 200)
	c3, _ := e.build("500", func(d *metadata.Document) { required(false)(d); m13Relabel("Hop three")(d) }, e.carol) // label only: POINTER_SAFE
	m13Code(t, "approve c3", e.httpApprove(e.dave.token, c3), 201)
	m13Code(t, "activate c3", e.httpActivate(e.erin.token, c3, c2, 2), 200)
	if w := e.httpRollback(e.erin.token, c1, c3, 3, nil); w.Code == 200 {
		t.Errorf("M13R-02: rollback c3 -> c1 crossed the TRANSFORM release c2 without a rollback plan (HTTP %d)", w.Code)
	}
}

// M13R-03: an approver demoted to a role that cannot approve (HTTP 403) still counts toward quorum.
func TestZZReproM13R03DemotedApproverStillCounts(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c1, _ := e.build("500", nil, e.carol)
	m13Code(t, "approve", e.httpApprove(e.dave.token, c1), 201)
	e.setRole(e.dave.id, "member")
	m13Code(t, "dave can no longer approve", e.httpApprove(e.dave.token, c1), 403)
	if w := e.httpActivate(e.erin.token, c1, "", 0); w.Code == 200 {
		t.Errorf("M13R-03: activation was admitted on the approval of a member who no longer holds releases.approve (HTTP %d)", w.Code)
	}
}

// M13R-04: a single body key spelled with U+017F (long s, folds to "s") is accepted as the field.
func TestZZReproM13R04KeyAlias(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c1, _ := e.build("500", nil, e.carol)
	body := `{"artifact_ha` + `\` + `u017fh":"` + c1 + `"}` // JSON escape of U+017F inside the key
	if w := m13apiRaw(e.h, e.root+"/candidates/"+c1+"/approve", e.dave.token, "application/json", body); w.Code != 400 {
		t.Errorf("M13R-04: approve with the key artifact_ha<U+017F>h answered %d (a misspelled key must be 400)", w.Code)
	}
}

// M13R-05: bidirectional and invisible formatting marks other than U+202A-202E and U+2066-2069 are stored in a rejection note.
func TestZZReproM13R05NoteFormatting(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	for _, r := range []rune{0x200E, 0x200F, 0x061C} {
		x, _ := e.build("500", m13Relabel(fmt.Sprintf("Note %X", r)), e.carol)
		if w := e.httpReject(e.dave.token, x, "OTHER", "a"+string(r)+"b"); w.Code == 201 {
			t.Errorf("M13R-05: rejection note with U+%04X was stored", r)
		}
	}
}

// M13R-06: the recorded runtime components are never compared with the running binary.
func TestZZReproM13R06RuntimeComponentsUnbound(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	hash, err := e.stageDoctored(func(b *promotion.BundleV2) {
		b.RuntimeContract.Components.WorkflowDSL = b.RuntimeContract.Components.WorkflowDSL[:len(b.RuntimeContract.Components.WorkflowDSL)-1] // build without DSL 2.2.0
		b.RuntimeContract.Components.SupportedIR = []string{"9.9.9"}
		b.RuntimeContract.Components.ToolCatalog = "tool-catalog-v0"
		raw, _ := json.Marshal(b.RuntimeContract.Components)
		sum := sha256.Sum256(raw)
		b.RuntimeContract.ComponentsHash = hex.EncodeToString(sum[:])
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := e.httpApprove(e.dave.token, hash); w.Code == 201 {
		t.Errorf("M13R-06: a candidate recorded for another runtime component set was approved (HTTP %d)", w.Code)
	}
}
```
