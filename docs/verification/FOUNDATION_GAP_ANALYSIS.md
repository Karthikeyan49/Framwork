# Foundation gap analysis (validated review vs current code)

Audit date: 2026-10-17 (UTC). Audited tree: `dbc4b5b`, the head of
`claude/m12-candidate-builder` (M0 to M12), merged into the audit branch
`docs/finding-status`. Scope: every row of
[the finding ledger](../migration/M9R_FINDING_STATUS.md) (F01 to F31), every
remediation slice of [the plan](../migration/M9R_REMEDIATION_PLAN.md) (R0 to R7,
R2a to R2c, R3a to R3d, R5a to R5e) and the code added by M10 to M12.

## Progress

- [x] Source documents read: validated review, remediation plan, ledger, R1 to R5c
  reports, M10/M11 and M12 reports, supersession map, ADR024, ADR036 to ADR038, the five
  independent review and honesty documents now in the tree.
- [x] Ledger rows F01 to F31 classified with file and line evidence
  (`docs/migration/M9R_FINDING_STATUS.md`, commits `d457c04`, `20259a7`, `2d0cb77`).
- [x] Remediation slices R0 to R7 classified (same file).
- [x] Fake-assumption scan, executed probes, newly discovered gaps N-01 to N-15 (below).
- [x] Ranked order of work (last section).

Finding-status audit STATUS: COMPLETE. Every row is classified from code and tests.
Nothing that needs a database was executed (see "What could not be verified").

## Scope, baseline and method

Three code lines exist; they are not one tree:

| Line | Tip | Relation to the audited head |
|---|---|---|
| M0 to M12 (audited) | `dbc4b5b` | baseline for every verdict |
| `claude/m13-approval-promotion` | `436c2b5` | M12 plus migration 49, ADR039, promotion routes; read with `git show` only |
| `origin/remediation/m9r-foundation` | `06882b2` | diverged at `e6305df`; carries R5c UI, R5d, R5e, R5f; not in the audited tree; spot-checked with `git show` only |
| `claude/m10-fixes` | `cf02e33` | M12 plus fixes for M10R-01 to M10R-05 (migration 50, DSL 2.2.0); unmerged and unreviewed |

Method. For each ledger row the cited code, migration or test was located and read
(`file:line` in the ledger). The previous status text was not used as evidence. Then:

1. Integration tests were read for what they assert (not whether they pass).
2. Executed here, no database, `nice -n 10 ./scripts/go.sh test -count=1`: 17 unit
   packages PASS (`internal/platform/postgres`, `httpserver`, `policy`, `runtime/data`,
   `runtime/workflow`, `config`, `identity`, `audit`, `promotion`, `risk`, `intent`,
   `verification`, `metadata`, `tools`, `sandbox`, `events`, `tests/security`).
3. Three temporary probes were run and deleted; none is committed (see the scan).
4. The fake-assumption scan searched for placeholder markers, constant returns, flags
   that gate behaviour, caller-declared authority fields, functions with no production
   caller, tests that pin a contract production does not use, and documents that
   overstate code. Repository search found no TODO/FIXME/stub markers worth reporting.

Verdict words: CLOSED, PARTIAL, OPEN, SUPERSEDED (as in the ledger). PASS / FAIL /
NOT RUN are used for executions. "Recorded PASS" is a PASS claimed by an
implementation report that this audit did not reproduce.

## Per-finding verdict summary

| ID | Finding | Verdict at the audited head | Driver or remaining work |
|---|---|---|---|
| F01 | Worker credential cross-tenant and record mutation | CLOSED (DB-credential threat model) | N-02, N-03 on the M10 surface |
| F02 | Roles own tables, broad worker grants | CLOSED | N-14 (denylist guard) |
| F03 | Tenant-controlled shared DDL | CLOSED | none |
| F04 | Records vanish across compiler/release change | PARTIAL | default routes keep hash-keyed namespace; no production dataset; no UI |
| F05 | RLS membership once per row | CLOSED | gate not in CI (N-09) |
| F06 | End-to-end query performance | PARTIAL | only registered numeric index meets the gate; N-09 |
| F07 | Shared dataset writer counter | CLOSED | gate not in CI (N-09) |
| F08 | Write retries / unknown COMMIT | CLOSED (local commands) | external effects: N-04 |
| F09 | Late rejection commits a record | CLOSED | none |
| F10 | Lost in-flight command key | CLOSED (memory-only by design) | frontend tests not run here |
| F11 | Custom indexes / uniqueness | PARTIAL | text/date/search and R3c uniqueness OPEN; N-15 |
| F12 | Aggregates in Go, one join | OPEN | R3d not started |
| F13 | Production runtime / public promotion lifecycle | OPEN at head; authority CLOSED on M13 branch (not merged) | N-05; production data plane absent everywhere |
| F14 | Caller-supplied risk / PASS / independence | PARTIAL | N-12, N-06; recompute fixed at head |
| F15 | DataScope ALL for built-in roles | PARTIAL; R5d, R5c UI on remediation line | N-01 |
| F16 | Fixed four roles | PARTIAL; R5e on remediation line | N-01 |
| F17 | JWT skew, token paste login | OPEN; R5f on remediation line | N-01 |
| F18 | M8 outage window, dead predecessor, one consumer | OPEN | R7 |
| F19 | Audit retention / partitioning | OPEN | R7 |
| F20 | M9 limits, copied IR | PARTIAL | no lease renewal; limits unchanged |
| F21 | Approval on start snapshot | PARTIAL | N-02 for the tool path |
| F22 | Rules have no consumer | CLOSED (claim was wrong) | none |
| F23 | External exactly-once, AI | PARTIAL (M10 built, not accepted) | N-04, N-11 |
| F24 | Rate limit, invalid-auth DB cost | OPEN | R7 |
| F25 | Per-request compile / catalog checks | OPEN (not profiled) | R7 |
| F26 | Two rulebooks, supersession map | PARTIAL | N-08 |
| F27 | Numeric gates, migration ownership | PARTIAL | N-09, N-01 |
| F28 | Portability | OPEN (M17) | nothing implemented |
| F29 | CI/deploy, SSO, search, DR | OPEN (scheduled) | runbooks are stubs |
| F30 | Review process | PARTIAL | M11 and M12 API reviews unfinished and unmerged |
| F31 | Stacked unmerged branches | OPEN | N-01: lineages diverged |

Totals over the 31 rows: CLOSED 9 (F01, F02, F03, F05, F07, F08, F09, F10, F22),
PARTIAL 12 (F04, F06, F11, F14, F15, F16, F20, F21, F23, F26, F27, F30), OPEN 10 (F12,
F13, F17, F18, F19, F24, F25, F28, F29, F31). Counting rule: a row is CLOSED only when
its stated acceptance is met at the audited head, within the limit named in the table;
rows closed only on an unmerged branch are not CLOSED.

### Remediation slices

| Slice | Verdict | Note |
|---|---|---|
| R0 | PARTIAL | map exists; agent contract and README stale (N-08) |
| R1 | CLOSED | recorded PASS; residual N-02, N-03, N-14 |
| R2a, R2b, R2c | CLOSED (preview) | R2 as a whole PARTIAL (F04) |
| R3a | CLOSED | recorded PASS; flaky current-host HTTP margin history |
| R3b numeric | CLOSED | other types OPEN |
| R3c, R3d | OPEN | not started |
| R4 | CLOSED (local commands) | external effects N-04 |
| R5a, R5b, R5c-a, R5c-b | CLOSED | recorded PASS |
| R5c UI, R5d, R5e, R5f | OPEN at the head | delivered on the remediation line only (N-01) |
| R6 | PARTIAL | M12 builder present; authority on M13 line; M14 not started |
| R7 | OPEN | external tool semantics built but defective (N-04) |

### Claims from the original validation that have no ledger row

| ID | Claim | Verdict | Evidence |
|---|---|---|---|
| V1 | Database hash integrity is not compiler admission (hash-consistent `{}` IR accepted by PostgreSQL) | PARTIAL | `runtime.workflow_bindings` only checks `sha256` equality (`00020:12-16`) and `framework_app` can insert it (`00020:24`); admission is Go `compiler.Restore` on capture and on the worker (`internal/platform/access/workflows.go:55-60`, `internal/runtime/workflow/store.go:49`). A forged binding is still rejected by the worker, not by the database |
| V2 | RequesterID filled by the builder | CLOSED | `Store.Stage` requires `RequesterID == actor` (`internal/promotion/store.go:50`); the port re-checks (`00048:122-128`) |
| V3 | Intent narrow, ambiguity passes as optional | PARTIAL | typed `requirements-v2` with a hashed requiredness floor (`internal/intent/requirements.go:26,496-520`); unresolved mandatory items block. Only four categories are verifiable, others BLOCK (M12 report) |
| V4 | Acceptance free text, no independence | PARTIAL | `acceptance-v2` is typed; author must differ from requester and source author in the stage port (`00048:93-145`). Independence is actor inequality, not proof of independent derivation (M12 report) |
| V5 | Import boundaries | CLOSED | `tests/security/boundaries_test.go`; executed here, PASS, `risk` and `verification` are registered |
| V6 | Immutable releases, workflow pins, atomic audit/outbox preserved | CLOSED | pins `internal/runtime/workflow/store.go:54,172`; bundle immutability triggers (`00011:47-48`, `00048:60-61`); audit immutable (`00002:91-92`); unit `internal/audit`, `internal/runtime/workflow` PASS here |
| V7 | Compiler migration plans say NONE where data changes | PARTIAL | real evolution operations exist for rename, integer/decimal conversion and backfill (`internal/compiler/storage.go`, `internal/runtime/data/evolution.go`); production use of them waits for M13 activation |

(Part 2: fake-assumption scan, newly discovered gaps, unverified items and the ranked
order of work follow in the next commit of this file.)
