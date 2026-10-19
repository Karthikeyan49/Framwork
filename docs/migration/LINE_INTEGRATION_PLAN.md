# Integrating the milestone line (M10–M14) with the remediation line (R-slices)

Status: **analysis only, 2026-10-19. Nothing was merged or renumbered.** The facts below come from one executed trial
merge in a scratch worktree that was never pushed. The recommendation needs a decision from the repository owner,
because the remediation line is being developed by another agent.

| Line | Branch | Head at analysis time | Commits since the common base `e6305df` | Files changed since the base |
|---|---|---|---|---|
| Milestone line (M10 to M14, reviews, fixes) | `claude/m14-ai-builder` (also `claude/dazzling-goldberg-69wbkn`) | `5a6895b` | 182 | 246 |
| Remediation line (R3c, R3d, R5c UI, R5d to R5g, R7a) | `origin/remediation/m9r-foundation` | `0a4f7d2` | 105 | 246 |

The two lines diverged after migration `00044`. 34 files are changed by both (docs aside).

## What the trial merge showed (executed)

`git merge origin/remediation/m9r-foundation` into the milestone head:

1. **21 files conflict textually** (38 hunks): `.github/workflows/ci.yml`, `Makefile`,
   `apps/web/src/preview/Workspace.test.tsx`, `apps/web/src/preview/contracts.ts`,
   `contracts/jsonschema/application-v1.3.schema.json` (added on both lines), `contracts/jsonschema/embed.go`,
   `docs/migration/M9R_FINDING_STATUS.md`, `internal/compiler/{README.md,compiler.go,rules.go,workflow_plans.go}`
   (compiler.go 4 hunks), `internal/config/config.go`, `internal/metadata/{README.md,models.go,validation.go}`
   (validation.go 4 hunks), `internal/platform/access/{access.go,metadata.go,workflows.go}`,
   `internal/platform/app/app.go`, `internal/runtime/workflow/{contracts.go,plan.go}`.
2. **Nine migration versions exist on both lines with different content: `00045` to `00053`.** The merged tree does not
   start: `goose.NewProvider` over the merged `db/migrations` fails with `found duplicate migration version 45:
   existing 00045_m10_tool_ledger.sql, current 00045_m9r_workflow_approval_roles.sql` (run in the scratch tree, then
   removed). The remediation line also has `00054`; the milestone line also has `00060`.
3. **ADR numbers `036` to `040` exist on both lines** (milestone line: M10 to M14; remediation line: workflow approval
   roles, field visibility, administration UI, OIDC sign-in, text and date projections). The remediation line continues
   to `060`.
4. **Two incompatible definitions of the published authoring contract `urn:framework:metadata:1.3.0`.** The milestone
   line makes `activity` a `oneOf` that admits tool activities (M10); the remediation line adds
   `state.approval_roles` (R5e). Both are "version 1.3.0" and both are described as immutable once published.

## Hazards that compile or merge cleanly and must be checked after a merge (not yet executed)

* `internal/policy/definition.go` and `store.go` are edited by both lines and merged without markers. The action
  catalog hash and `policy.Version` have to be re-derived; every stored candidate becomes `POLICY_CHANGED` after a
  bump, as after each earlier bump.
* The derived worker guard `control.worker_grants_safe()` (migration `00052` of the milestone line) refuses any
  relation of `control`, `runtime`, `iam` or `audit` that is not on its allowlist and carries a worker grant. The new
  relations of the remediation line (`runtime.scalar_indexes`, `search_indexes`, `unique_constraints`, and others)
  appear to grant the worker nothing and add `worker_no_generic` deny policies, so the guard should hold; running
  every worker-claiming test is the proof.
* `internal/platform/app/app.go` wiring, readiness lists, `internal/platform/access/roles.go` and
  `internal/platform/postgres/transaction.go` are changed by both lines.
* Tests that count migrations or compare policy versions.
* The development database `MAIN` is migrated with the milestone line's numbers (`migrate status`, read only, shows
  `00045` to `00053` and `00060` applied). After a renumbering it has to be rebuilt, not migrated.

## Options

**A. Replay the milestone line on top of the remediation line (recommended).** `CLAUDE.md` puts the remediation gate
before M10, the remediation line has more documents and evidence that cite its migration and ADR numbers, and the
milestone line has fewer. Steps once the remediation line is frozen at its gate:

1. Renumber the milestone migrations (`00045` to `00053`, `00060`) to follow the remediation line's last number, keep
   their order, and update every reference (`db/migrations`, tests that name a version, ADR039/ADR040, the M10 to M14
   reports, `internal/promotion/README.md`, `docs/migration/V2_V3_SUPERSESSION_MAP.md`).
2. Renumber ADR `036` to `040` after the remediation line's last ADR and fix the links.
3. Decide the metadata contract: either one `1.3.0` that carries both changes (acceptable only while neither is
   published outside the repository) or move the tool activities to `1.4.0` and extend the compiler's supported
   versions accordingly.
4. Resolve the 21 conflicting files by hand, with the compiler, metadata and workflow-plan files first.
5. Re-derive the policy catalog hash, rebuild the isolated and the `MAIN` databases from the merged tree and run the
   full gate (`make check`, `make test-unit`, the full integration suite, `make build`, `make test-e2e`; about 35
   minutes on this host).

**B. Merge the remediation line into the milestone line.** The renumbering then falls on the remediation line's
migrations and ADRs, which many of its reports cite. Not recommended.

**C. Keep the lines separate.** Not viable: M10 to M14 are a preview control plane that needs the foundation, and the
remediation line needs nothing from them, so the duplicated numbers only get more expensive with every commit.

## Decisions needed from the owner

1. Direction (A is recommended) and who executes it: the agent that owns the remediation line, or the milestone-line
   agent after the remediation line is frozen.
2. The freeze point of the remediation line (new migrations on either line after the freeze renumber again).
3. The metadata contract decision (step 3).

Cleanup note: the scratch worktree of the trial merge was removed after the facts were recorded; the merge was aborted.
