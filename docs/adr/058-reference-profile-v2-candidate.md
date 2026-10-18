# ADR058 — Test a calibrated reference profile with four API transactions

19 October 2026 (Asia/Kolkata). Provisional implementation candidate approved
after independent read-only review, before configuration changes. Acceptance
requires all eight workload gates and normal regressions. NOT production proof.

ADR057's app-role ABBA diagnostic runs81.81s and91.382s. The first has stable
numeric visibility8609/9186 pages: unfiltered money cost4 sequential plans take
168.232/171.273ms, cost1.1 covering index plans141.029/145.895ms with19028 heap
fetches. Filtered230.256/210.950 versus230.195/214.445ms shows no clear gain.
The corrected statistics inspection uses only the existing owned-fixture admin
for pg_stats metadata (RLS hides it from normal roles), while every analyzed
business query retains app authority. Second-run visibility6276/9186 is stable
within that run; money194.598/180.832 versus208.373/155.672ms is variable,
filtered215.622/204.976 versus181.062/184.406ms improves. Correlation and page
counts are logged without histogram/customer values. These observations support
testing a cost model, not a guaranteed improvement or demonstrated SSD property.

Define explicit reference profile v2: retain pinned PG18.6,4CPU/8GiB/no swap,
2GiB shared buffers,6GiB effective cache estimate and4MiB work memory; set
random_page_cost1.1. No hardware increase, vacuum/visibility/statistics-target
change, scan-method forcing, driver setting or metadata hint. Verify the actual
connected setting on every performance run. Ordinary infra-up remains lightweight;
make reference-up selects this declared candidate. Production must calibrate its
own deployment;1.1 estimates this measured cached/reference environment only.

Restore actual API default/fallback4 (strict1..10 overrides,FIFO128,pool10).
Default3 missed the unmapped HTTP gates and delivered5697 writes,p95406.116ms;
prior4 observations had better tails/throughput, but source/index/profile differ,
so admission alone is not a proven cause. Test the JOINT v2/default4 envelope;
do not claim independent causation from its results. Keep all default3/cost4
failures and the historical unmapped4 pass. All ten clients,100 samples,100k hot
rows, populated other tenants, individual deadlines and latency gates remain.

Supersedes ADR048's cost4 and ADR055's default3 candidate only if acceptance
passes. Current/retained canonical data, fault/authority correctness,32-client
write/unique capacity and all prior read/browser gates remain mandatory. If any
fails, keep R3d incomplete and investigate; no implicit acceptance by tuning.
No data/DDL/privilege/dependency or production runtime capability changes.
