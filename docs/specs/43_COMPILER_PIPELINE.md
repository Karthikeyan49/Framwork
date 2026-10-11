# Application Compiler Pipeline

Stages:
1. structural schema validation
2. source schema upgrade
3. symbol collection
4. reference resolution
5. type checking
6. expression validation
7. relation/fanout validation
8. query-risk analysis
9. policy/capability validation
10. workflow graph validation
11. side-effect validation
12. migration planning
13. generated-test planning
14. normalization
15. IR hash/release diagnostics

Given the same source, dependency catalog, schema snapshot and compiler version, normalized IR should be deterministic.
