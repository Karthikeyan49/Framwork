# Upgrade and migration-ordering rehearsal (independent)

Scope: `integration/m9r-m14` at `be6ec7c`, branch `audit/upgrade-rehearsal`. The verifier wrote none of the code or migrations under
test and changed none of them. Every hypothetical migration lives in a scratch copy of the tree that is never committed. All databases
are disposable databases created on the isolated M13 Compose stack (127.0.0.1:5433, project `framework-m13`), or that stack's own
database after `reset.sh`.

## Progress

| Item | Status | Result |
|---|---|---|
| Report skeleton | done | this file |
| T1 populated 57 -> 90 | in progress | NOT RUN yet |
| T1 populated 89 -> 90 | pending | NOT RUN yet |
| T1 schema: fresh 1..90 vs populated 57 -> 90 | pending | NOT RUN yet |
| T2 (i) 00091 replaces a function | pending | NOT RUN yet |
| T2 (ii) duplicate 00091 | pending | NOT RUN yet |
| T2 (iii) file below the applied maximum | pending | NOT RUN yet |
| T2 (iv) holes 58-79 | pending | NOT RUN yet |
| T2 (v) lineage guard | pending | NOT RUN yet |
| T2 (vi) number/count assumptions | pending | NOT RUN yet |
| Verdict on the numbering rule | pending | |
