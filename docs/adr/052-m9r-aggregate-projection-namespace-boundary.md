# ADR052 — Plan the exact aggregate projection namespace once

18 October2026 (Asia/Kolkata). Status: accepted for implementation after
independent read-only design review, before source. R3d only; all gates required.

The four-slot/JIT-off/covering-index experiment passes full count/integer/decimal/
money/share cases, but filtered HTTP807.830ms and OWNER SQL664.298/HTTP1543.657ms
still fail. The filtered analyzed plan performs14,285 separate numeric point
lookups, ~110ms on top of107ms source scanning. OWNER custom EXPLAIN is141.834ms,
while actual repeated SQL is much slower. Generic-plan skew is a hypothesis,
not a proven cause. Preserve FAIL123.770s and earlier failed runs.

Present READY numeric projections to the LEFT JOIN as one typed derived relation:
select only ID, source_version, value_number and aggregate_valid_v1 under exact
trusted tenant/app/physical revision/hash/entity/field predicates, with OFFSET0
as a projection-namespace boundary. Join on record ID. This prevents per-source-
record parameterized point lookup from crossing the boundary. PostgreSQL still
chooses scan/join methods; no enable_* or plan_cache_mode setting is introduced.
Do not claim OFFSET0 alone proves a hash plan or solves cached-plan behavior.

The unique physical key guarantees at most one p row per main source ID. Missing/
stale/false remain counted invalid sentinels, never WHERE filters. Legacy NULL
flags keep exact late source JSON validation. Canonical source/current access,
scope/filter/cap ordering, strict input checks and forced projection RLS remain.
The whole-p relation contains only typed numeric/header data: do not introduce
casts/raw validation that could make unauthorized corrupt rows raise errors.

The projection scan may exceed the authorized matching-input count (including
sparse filters); this adds no output authority or timing-noninterference claim.
Keep the existing2s statement and transaction limits. Rare-filter/index-only/
arbitrary-volume capacity remain bounded by measured cases, not inferred.

Verify actual generated/cached execution, sparse/empty/ALL/NONE/OWNER/SHARE cases,
missing/stale/false/legacy flags and canonical retained authority. Rerun unchanged
capacity plus write/uniqueness gates and independent adversarial review. On a
miss keep the subgate partial and investigate; never edit budgets or fixtures
to label it complete. No new data, DDL, privilege, setting or dependency.
