# ADR048 — Explicit PostgreSQL reference memory profile

18 October 2026 (Asia/Kolkata). Status: accepted for implementation following
independent read-only design review, before configuration code.
No production deployment or default local-memory increase is authorized here.

## Evidence and decision

R3d's full100k count is functionally correct but fails the unchanged10-client
SQL500ms/HTTP750ms targets. Its shared table contains190k representative1382-byte
records across10 tenants. The measured plan reads25,821 heap blocks from outside
PostgreSQL's shared buffers; this demonstrates buffer churn, not physical disk
I/O (the operating-system cache may serve these reads). That reference instance
uses the image default128MiB shared
buffers despite its established8GiB memory limit. Namespace source planning
reduces joins but still misses SQL p95 (612.464ms); ordering adds sort/spill work
and is rejected as a remedy. Preserve these failed runs.

Introduce an explicit local performance override, selected only by
`make reference-up`. Keep the same pinned postgres18.6 image,4 CPUs,8GiB memory
and disabled swap. Set shared_buffers2GiB (25% of that existing tier) and
effective_cache_size6GiB (a planner estimate, not an allocation). Keep work_mem4MiB,
random_page_cost4, statement2s, lock500ms, transaction3s and all latency/error
thresholds unchanged. No RLS, authorization, field-validity or query-budget
relaxation is permitted. This allocates the already defined tier; it does not
increase the tier's memory/CPU limits.

The normal Compose file and `make infra-up` remain suitable for smaller developer
machines. Reference mode is a separate explicit override with declared memory,
CPU, swap and PostgreSQL command settings; ordinary startup must not quietly
select it. Changing profiles may recreate only the local PostgreSQL container,
retaining its named volume, credentials and all records. Do not target production
or restart an unrelated deployment. Fail if the host cannot supply the profile.

Reference tests must read actual server parameters plus Docker resource limits
and image identity, and reject a claimed reference run under another profile.
Do not label the earlier default-cache measurements as final-profile results or
hide their misses. Run actual generated SQL including per-input validity, live
canonical authorization and representative mixed-policy cases, then all previous
numeric/scalar/choice/native/search/write/unique gates. Report exact settings,
current-instance timings and limits. Performance outside this configured tier
remains unverified; normal lightweight startup is not a capacity guarantee.

## Recovery and limits

Use the ordinary Compose startup to restore normal development settings while
retaining the volume. Stop any owned benchmark before switching profiles. This
does not address table growth, indexing, arbitrary workloads, production sizing,
connection tuning or backup/DR. If capacity still fails, continue diagnosis and
fix data/query design; memory configuration is not proof that aggregate gates pass.
