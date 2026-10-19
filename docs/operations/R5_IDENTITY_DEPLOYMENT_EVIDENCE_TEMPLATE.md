# R5 external identity deployment evidence — template

Status: NOT RUN. No real provider or domain selected. Local conformance results
belong to the R5f/R5g reports, not this template. Copy for a specific authorized
staging/deployment; preserve this blank template in Git.

- Source/build identity: NOT RUN
- Public provider/client/domain identification (no credentials): NOT SELECTED
- Operator / independent observer / date: NOT RUN
- Approved deployment scope and recovery owner: NOT RUN

| Check | Result (PASS/FAIL/NOT RUN) | Sanitized evidence reference / observer |
| --- | --- | --- |
| Exact HTTPS origin, callback routing, private API, trusted certificates | NOT RUN | |
| Cookie security, no-referrer, callback canary absent from every log/export | NOT RUN | |
| Public PKCE client and pinned API/ID claim interoperability | NOT RUN | |
| Unprovisioned subject / forged roles cannot authorize | NOT RUN | |
| Nonce/state/cookie/time/key failures and replay/overlapping tabs | NOT RUN | |
| Live role/membership revocation and exact-connection 401 handling | NOT RUN | |
| IdP/Redis outage and restored fresh-flow recovery | NOT RUN | |
| Dedicated Redis ACL and worker/frontend secret separation | NOT RUN | |
| Trusted key overlap/retirement/restart rehearsal | NOT RUN | |
| Provider MFA/recovery assurance; required application claims assessed | NOT RUN | |
| Clock synchronization and bounded skew | NOT RUN | |
| R6/R7 / other release gates separately satisfied | NOT RUN | |

Record failures and corrective verification explicitly. Never attach tokens,
authorization codes, cookies, DSNs, key material or raw payload/driver logs.
No authorization to deploy, merge main or start M10 is granted by this file.
