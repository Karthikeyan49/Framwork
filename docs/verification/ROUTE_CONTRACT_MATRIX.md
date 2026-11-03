# Route inventory, authorization matrix and contract drift (final verification round)

Auditor branch `audit/route-contract-matrix`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`).
Report-only: no product code, test, ADR or document was changed. The probe files (`zz_review_*`) lived only in the worktree and were removed before this commit.
Verdict for this scope: **PASS WITH FINDINGS** (no HIGH, no MEDIUM; nine LOW findings, one of them a documented-claim-without-test).

## Progress

| time (UTC) | state |
|---|---|
| 06:35 | skeleton pushed; 91-pattern inventory and DB-free HTTP semantics executed |
| 06:50 | policy matrix, 91 x 7 route-by-role matrix in a fake-SQL world, classification and ordering probes executed; findings RC-01..RC-09 written |
| open | the optional m13 database probe (`TestZZRoutesMatrix`) stayed queued behind three other agents and was withdrawn: every claim that needs a real PostgreSQL is NOT RUN (section 7) |

## 0. What ran (all executed, none copied from a document)

| probe | how | result |
|---|---|---|
| inventory | `gt test -run TestZZInventory ./internal/platform/access`: reflective walk of the Go 1.27.2 `ServeMux` routing tree after `API.Register` (preview, workflows, registry on); each pattern turned into a concrete path and routed back with `mux.Handler`; every sample path run through `AcceptsPOST` | 91 patterns (85 without workflows, 18 without preview, 14 without registry); 0 route/pattern mismatches; 0 POST routes outside the body allowlist; 0 allowlist shapes without a route |
| HTTP semantics, no database | `TestZZSemantics`, `TestZZRawServer`: `httpserver.Handler` with the real mux, `Auth` nil (a request that reaches `serve` answers 503 `identity_unavailable`), plus raw TCP against `httpserver.New` | tables in section 4 |
| native policy and custom roles | `gt test -run TestZZ ./internal/policy`: `Native.Decide` and `Defined.Decide` over the whole action catalog x 11 subjects, resource-type confusion, cross-tenant resource, cancelled context, grantability of every action per role name, single-action custom roles, DENY precedence, selector scoping, subject binding | section 3.1 to 3.3 |
| route x role over HTTP in a fake-SQL world | `TestZZFakeWorldMatrix`: `database/sql` driver that answers only the runtime-role check, `iam.users`, `iam.memberships`, `tenant_matches_scope` and the `audit.entries` insert (commit/rollback aware), a recording `policy.Engine`, 7 actors x 91 routes; a request that passes the policy reaches a handler that needs SQL and answers 503, a denied one answers 403 | section 3.4 |
| classification and ordering | `TestZZFakeClassification`: 48 requests (percent-encoded separators, case, whitespace, Authorization shapes, media types, body sizes, HEAD) | section 4 and findings |

Limits of the fake world: a 503 means "passed authentication, tenant resolution and the route-level policy decision and then needed SQL", not "succeeded". It proves ordering and the route-level action, not handler behaviour.

## 1. Route inventory

91 patterns come from `access.API.Register` (`access.go:75-161`, `ai.go:76`), 3 from sign-in (`signin.go:25-35`), 3 from `httpserver.Handler` (`httpserver.go:50,53,95`). Registration is conditional: the 14 tenant/role/team/member/session routes always; the 4 metadata routes when `Registry != nil`; the 73 preview, release, sandbox, dataset, data, index, workflow and AI routes only when `PreviewEnabled` (production sets it only for `development` and `test`, `app.go:270`), and 6 of them only with `WorkflowsEnabled`. Each preview row below exists twice (`versions/{revision}` and `datasets/{dataset}/bindings/{binding}`) where the pattern list says so.

Columns: **action** is the route-level decision recorded by the recording engine (first `Decide`); **inner** are the further decisions the handler takes. **audit** is the committed action (`<a>` allowed, `<a>.rejected` handler refusal in a fresh transaction, `<a>` with allow=false for a policy denial). **tests** is a grep-level pointer, not execution evidence.

| route (tenant prefix `/v1/tenants/{tenant}` shortened to `T`) | action / resource | inner | audit | body limit | idempotency / replay | tests |
|---|---|---|---|---|---|---|
| `GET /healthz`, `GET /readyz` | none | | none (access log) | none (body refused) | | httpserver_test.go, smoke.mjs |
| catch-all `/` | none | | none | | 404 `not_found`; 405 + `Allow: GET, HEAD` only for the two paths above | httpserver_test.go, rejections_test.go |
| `GET /v1/auth/provider`, `POST /v1/auth/login/start`, `POST /v1/auth/login/complete` | none (origin + cookie + Redis flow) | | **none** | 8192 B, `application/json` exact | one-time flow, consume before exchange | signin_*_test.go, m9r_oidc_test.go, test-signin-e2e |
| `GET /v1/session` | `identity.session.read` / user (Native only; not in the action catalog) | | `authentication.accepted` / `identity.session.denied` | none | | identity_policy_test.go, m9r_oidc_test.go |
| `GET T/access` | `tenant.access` / tenant | | `tenant.access` | none | | 44 files |
| `GET T/members`, `GET T/members/{id}` | `memberships.read` / membership | | `memberships.read` | none | | m9r_capabilities_test.go, identity_policy_test.go |
| `GET T/metadata/{app}/versions`, `.../versions/{rev}` | `metadata.read` / metadata | | `metadata.read` | none | `?before=N` | 45 files |
| `GET T/metadata/{app}/versions/{rev}/compile` | `metadata.compile` / metadata | | `metadata.compile` | none | | compiler_test.go, m14_api_*_test.go |
| `POST T/metadata/{app}/versions` | `metadata.write` / metadata | | `metadata.write` | 256 KiB + 4096 (`metadata.go:45`), json or yaml | CAS `expected_revision` -> 409 `revision_conflict` | 45 files |
| `GET T/roles`, `T/role-contract`, `T/roles/{role}`, `T/roles/operations/{op}` | `roles.read` / role | | `roles.read` | none | `?after=`, `?name=` | m9r_role_administration*_test.go |
| `POST T/roles` | `roles.write` / role | | `roles.write` | 64 KiB (`readDataCommand`) | `operation_id` + `expected_version` | m9r_role_administration_test.go, roles_wire_test.go |
| `POST T/members/{m}/role` | `roles.assign` / role | | `roles.assign` | 64 KiB | `expected_role` CAS | m9r_capabilities_test.go |
| `GET T/teams`, `T/teams/{team}` | `teams.read` / team | | `teams.read` | none | | m9r_team_admin_test.go |
| `POST T/teams`, `T/teams/{t}/members/{m}` | `teams.manage` / team | | `teams.manage` -> `teams.changed`, `teams.membership.changed` | 64 KiB | `expected_version`, `expected_active` CAS, never retried | m9r_team_admin_test.go |
| `GET T/preview/{app}/commands/{command}` | `commands.reconcile` / preview | | `commands.reconcile` | none | receipt lookup | data_runtime_test.go |
| `POST T/preview/{app}/datasets`, `.../datasets/{d}/verify`, `.../datasets/{d}/bindings`; `GET .../datasets/{d}` | `datasets.manage` / preview | | `datasets.manage` | 256 KiB + 4096 | `expected_binding` CAS | m9r_dataset_test.go, m11_*_test.go |
| `POST .../datasets/{d}/evolutions`; `GET .../evolutions/{e}`; `POST .../evolutions/{e}/{advance,activate,abort,reverify}` | `datasets.evolve` / preview | | `datasets.evolve` | 256 KiB create, 64 KiB steps | `operation_id`, `expected_checkpoint` | m9r_shadow_test.go (all four steps) |
| `POST .../sandboxes`, `.../sandboxes/{s}/freeze`; `GET .../sandboxes/{s}`, `.../evidence` | `sandbox.manage` (POST) / `sandbox.read` (GET) / preview | | same; freeze success -> `sandbox.frozen` | 256 KiB / `{}` | freeze runs READ COMMITTED (`access.go:228`) | m11_sandbox_test.go, m11_freeze_race_test.go |
| `POST/GET .../requirements`, `GET .../requirements/{v}` | `requirements.decide` / `requirements.read` | | same | 256 KiB | CAS `expected_version`, deliberately not retried | m12_candidate_test.go, m12api_review_test.go |
| `POST .../acceptance-specs`; `GET .../acceptance-specs/{s}` | `acceptance.author` / `acceptance.read` | | same | 256 KiB (pre-read 1 MiB) | content-addressed; repeated after a known abort (`access.go:273`) | m12_candidate_test.go |
| `POST/GET .../candidates`; `GET .../candidates/{h}` | `candidates.build` / `candidates.read` | `candidates.build` again (stage gate) | same | 256 KiB (pre-read 1 MiB) | content-addressed; repeated after a known abort | m12*, m13_*_test.go |
| `POST .../candidates/{h}/approve`, `.../reject` | `releases.approve` | database port: separation of duties, quorum | same | <= 256 KiB, ASCII keys | no retry; DB port compare-and-set | m13_api_test.go, m13_adversarial_test.go |
| `POST .../candidates/{h}/activate` | `releases.activate` | pointer CAS | same | | `expected_base`, `expected_generation` | m13_api_test.go |
| `POST .../releases/rollback`; `GET .../releases`, `.../releases/active` | `releases.rollback` / `releases.read` | | same | | `expected_current`, `expected_generation` | m13_api_test.go |
| `POST .../ai/patch-proposals`, `.../ai/requirement-proposals` | `ai.propose` | | `ai.propose` -> `ai.proposal.admitted`, `ai.proposal.created` | 32 KiB (`ai.go:84`) | creation repeated on known abort (5 tries / 4 s); result exchange 6 tries / 6 s | m14_api_*_test.go, m14r_ai_route_test.go |
| `POST .../ai/proposals/{id}/apply`, `.../confirm`, `.../reject`; `GET .../ai/proposals`, `.../proposals/{id}` | `ai.apply` (x3), `ai.read` (x2) | apply also `metadata.write`, confirm also `requirements.decide` (`ai_decide.go:46,81`); reject nothing more | `ai.proposal.applied`, `.confirmed`, `.discarded` | 128 / 64 / 4 KiB | CAS `expected_revision`, `expected_requirements_version`; not retried | m14_api_decide_test.go, m14_api_audit_test.go |
| `GET .../versions/{rev}`, `GET .../datasets/{d}/bindings/{b}` | `preview.read` | | `preview.read` | none | | 48 files |
| `POST .../entities/{e}/query` (both families) | `preview.read` | `data.entity|field|row.read` + SQL DataScope | `preview.read` + per-check rows | 64 KiB | | data_runtime_test.go, m9r_*_test.go |
| `POST .../entities/{e}/commands` (both families) | `preview.write` | `data.entity|field|row.write`; events and workflow triggers | `preview.write`, `record.created|updated|deleted` | 64 KiB, 4 s budget | command key receipt; `RetryLocal` on known abort | data_runtime_test.go, m9r_commands_test.go |
| `POST .../rules/{r}/evaluate` | `preview.read` | `rules.evaluate` x2 | `rules.evaluate` / rule | 64 KiB | pure | m7_rules_test.go |
| `GET/POST .../records/{r}/access` | `preview.read` / `preview.write` | `data.row.share` (GET), `accessAction(op)` (POST) | `preview.*`, `records.access.changed` | 64 KiB | `expected_version` CAS, never retried | m9r_record_access_test.go |
| `GET/POST .../entities/{e}/indexes|search-indexes/{i}[/register|/build]` | `indexes.manage` | `data.entity.read`, `data.field.read` | `indexes.manage` | 64 KiB | `expected_checkpoint` | m9r_*_index_test.go |
| `GET/POST .../entities/{e}/unique-constraints/{i}[/register|/build|/abort]` | `indexes.manage` | `data.UniqueAuthority` | `indexes.manage` | 64 KiB | `expected_generation`, `expected_checkpoint` | m9r_unique_*_test.go (`abort` through the `uniqueStep` helper) |
| `POST .../workflows/{w}/start`, `.../instances/{i}/signals`; `GET .../instances/{i}` (both families, `WorkflowsEnabled`) | `preview.read` | `workflows.*` in the workflow store (viewer denied there) | `preview.read`, `workflows.*`, `workflows.rejected` | 64 KiB | `idempotency_key`, signal sequence | m9_workflow_test.go, m9r_shadow_test.go |

Flags requested by the brief:

* **No action mapping**: none of the 91 has none; 31 distinct route-level actions are produced (32 with the session action). `profile.read` is the only catalog action no route or handler uses.
* **No audit emission**: sign-in (3 routes), `/healthz`, `/readyz`. In the fake world every 401, 403, 400 and 200 outcome (all 91 routes x 7 actors) has a committed audit row; the only requests without one are the 181 that ended in 503 after the fake driver refused a business query (nothing committed, fail-closed).
* **Action differs from the documentation**: none found for the 31 documented actions (ADR034/035/060/101-104, policy.go header comment); misclassification only through percent-encoded separators (RC-02).
* **No test**: 0 of 91 by grep; the three that a first pass flagged (`unique-constraints/.../abort` x2, `evolutions/.../reverify`) are exercised through helper functions (`uniqueStep(..., "abort", ...)`, `shadowStep(..., "reverify")`). There is **no test that enumerates routes x roles**; the release family alone has a registration/allowlist drift guard (`promote_test.go:35`).

## 2. Contract drift

`contracts/` holds JSON Schemas of application metadata and one role-editor wire fixture, **no HTTP contract**; the HTTP contract is the README table, the ADRs and the implementation reports.

| # | document says | code does | verdict |
|---|---|---|---|
| D-1 | README "Operational API" table: 11 endpoints | 91 + 3 + 3 patterns; roles, teams, preview, release, AI are only in ADRs. 30 of 91 routes have no ADR/README sentence naming their literal path (documented by family) | LOW, RC-07 |
| D-2 | `docs/M0_COMPLETION_REPORT.md:143`: unsupported methods answer JSON 405 with `Allow` | only `/healthz` and `/readyz` do; `DELETE` on all 91 API paths answers 404 | LOW, RC-01 |
| D-3 | ADR104 A7: routes are "drift-tested against the body allowlist"; `ai.go:37`: "(ai_test.go compares them)" | there is no `ai_test.go`, no test mentions `aiPOST`. The comparison holds today (probe: 7/7) | LOW, RC-04 |
| D-4 | ADR104 A7: "any other string ... is 404 `not_found`" for `{id}` | `POST .../proposals/not-a-uuid/apply` and an upper-case UUID, with a body, answer 413 `body_not_allowed` before routing (`ai.go:67`, `httpserver.go:182`) | LOW, RC-05 |
| D-5 | ADR104 A7: malformed body -> 400 `invalid_ai_request`, over cap -> 413 `body_too_large` | wrong media type -> 415 `unsupported_media_type` (`ai.go:101`), undocumented; other families answer 400 | LOW, RC-06 |
| D-6 | README: `GET /v1/session` "at most 100 memberships"; `GET .../members` "at most 100, owner/admin only"; metadata list "up to 50 headers, `?before=N`"; `POST` metadata owner/admin; `GET` metadata "active member" | matches code and the executed matrix (members: viewer/member 403, admin/owner pass; metadata GET passes for viewer; metadata POST 403 for viewer/member) | no drift |
| D-7 | ADR060 role routes and codes (`role_not_found` 404, `role_limit` 409, `invalid_role` 400, `roles.read` for contract and operation reads) | match `roles.go:21-35` and the matrix | no drift |
| D-8 | 27 (status, code) pairs written in ADR034-063 and 100-104 | all 27 code strings exist in non-test Go; the 19 ADR104 pairs compared by reading match `ai.go:99-251` (status and code) | no drift |
| D-9 | `docs/verification/M12_API_INDEPENDENT_REVIEW.md` paths `.../preview/app.X/candidates`, `.../candidates/roles` | attack probes, not routes | none |
| D-10 | docs say preview/release/AI routes exist "only where preview exists" | confirmed: `app.go:270` registers them only for `development`/`test`; in production 73 of 91 are absent (404) | no drift |

Routes in docs but not in code: none (a scan of every `GET|POST /v1/...` and `.../` mention in README and `docs/**` found none that is not a registered pattern). Routes in code but not in docs: see D-1.

## 3. Authorization matrix

### 3.1 Native policy `m14-native-v11` (executed: `Native.Decide` over the catalog; Y = allow)

Subjects: owner, admin, member, viewer; "custom" = membership role `finance` under `Native` (no role loader); "outsider" = owner of tenant B asking about tenant A; "mismatch" = `User.ID != Access.ActorID`; anonymous; user without access. For the first four the table is the whole answer; the other seven columns were `-` for every tenant action (the only `Y` is `identity.session.read`, which is self-only by design: `permitsSelf`).

| action (resource type) | owner | admin | member | viewer | | action | owner | admin | member | viewer |
|---|---|---|---|---|---|---|---|---|---|---|
| tenant.access, profile.read, metadata.read, metadata.compile, preview.read, rules.evaluate, sandbox.read, requirements.read, acceptance.read, candidates.read, releases.read, ai.read, workflows.read, data.entity/field/row/aggregate.read | Y | Y | Y | Y | | memberships.read, metadata.write, datasets.manage, datasets.evolve, indexes.manage, sandbox.manage, requirements.decide, acceptance.author, candidates.build, releases.approve/activate/rollback, ai.propose, ai.apply, teams.read/manage, roles.read, data.row.share/assign, workflows.recover/reconcile | Y | Y | - | - |
| preview.write, commands.reconcile, data.entity/field/row.write, events.consume, tools.invoke, workflows.start/execute/activity/cancel/approve/reject | Y | Y | Y | - | | roles.write, roles.assign | Y | - | - | - |

Also executed on `Native`: unknown action (`no.such.action`) denied for every subject; resource-type confusion (each action tried on 17 foreign types as owner) allowed nothing; a resource of another tenant allowed nothing; a cancelled context allowed 0 of 55 actions; two roles (`owner`+`viewer`) = owner.

### 3.2 Custom-role engine (`Defined`, role loader of production)

* A role holds exactly its grants: for each of the 47 single-action roles that compile (of 53 catalog actions; the six refused are `datasets.manage`, `datasets.evolve`, `indexes.manage`, `sandbox.manage`, `roles.write`, `roles.assign`), `self=Y` and `implied=[]` (no action implies another; `SINGLE` rows).
* Grantability by role name (`GRANTABLE` rows): `datasets.manage/evolve`, `indexes.manage`, `sandbox.manage` only to roles named `owner` or `admin`; `roles.write/assign` only to `owner`, and `owner` cannot be published (`store.go` `Publish`: `d.Name() == "owner"`), so those two stay Native-owner-only. `viewer` is refused 26 actions (the write-class ones).
* DENY beats ALLOW (`preview.write` allowed on `app.orders`, denied on `app.secret` with a DENY grant for that app); a selector `app_id=app.orders` denies `app.other`; a label mismatch, another tenant (Access or resource), another actor, a second role, anonymous and a cancelled context all deny (`BIND` rows); a configured empty `admin` allows 0 actions while the bootstrap adapter equals `Native` (the single difference is `identity.session.read`, deliberately outside the loader).
* Divergence: a role **named `viewer`** can be granted `workflows.cancel`, `workflows.approve`, `workflows.reject`, `workflows.recover`, `workflows.reconcile` (`definition.go:171` omits them from the viewer refusal list) although `Native` denies a viewer all five and `policy.go:112-114` makes `recover`/`reconcile` owner/admin only (RC-09).

### 3.3 Actions the route table uses that the policy does not know, and the reverse

* Used, not in the catalog: `identity.session.read` (handled by `permitsSelf`, cannot be granted to a custom role - intended). Audit-only labels that are not actions: `ai.proposal.*`, `sandbox.frozen`, `teams.changed`, `teams.membership.changed`, `records.access.changed`, `workflows.rejected`, `workflows.data_scope`, `tenant.denied`, `policy.unavailable`, `authentication.*`.
* Default for an unknown action is deny in both engines (executed: `no.such.action`, `tools.execute`-style names).
* In the catalog, used by no route or handler: `profile.read`. (`workflows.cancel|approve|reject|recover|reconcile` are reached through `"workflows."+r.Operation`, `signals.go:27`; `events.consume`, `tools.invoke` by workers.)

### 3.4 Route x role over HTTP (executed in the fake-SQL world, 91 routes x 7 actors)

| actor | outcomes over the 91 routes |
|---|---|
| anonymous | 401 x 91 (`authentication.denied`) |
| garbage token | 401 x 91 |
| outsider (member of tenant B only) | 403 x 90 tenant-scoped routes (`tenant.denied`), the one non-tenant route `GET /v1/session` passes (503 = needed SQL) |
| viewer | 403 x 62, passed the route-level gate x 28 (503), 200 x 1 (`access`) |
| member | 403 x 57, passed x 33 (32 x 503, 1 x 400), 200 x 1 |
| admin | 403 x 2 (`POST roles`, `POST members/{m}/role`), passed x 88 (69 x 503, 19 x 400), 200 x 1 |
| owner | passed x 90 (69 x 503, 21 x 400), 200 x 1 |

Denial groups by role (D = 403, P = passed): `viewer/member/admin/owner` = DDDP for 2 routes (roles.write/assign), DDPP for 55 (admin-class actions), DPPP for 5 (non-viewer actions: `commands`, `entities/.../commands`, `records/.../access` POST, `preview.write`), PPPP for 29 (reads). Every row equals the Native table of 3.1 applied to the action in section 1, so there is no route whose HTTP outcome differs from its documented action. The inner guard is what denies a member on `POST .../records/{r}/access` and a viewer on workflow start (route-level `preview.read`/`preview.write` only opens the outer gate).

## 4. HTTP semantics (executed)

Database-free through `httpserver.Handler` with the real mux:

| input | result |
|---|---|
| `HEAD` on a GET route | served by the GET handler (`GET` patterns match `HEAD`); with a token `HEAD /access` -> 200 and an audit row `tenant.access/true`; `HEAD` on a POST-only path -> 404 |
| `OPTIONS`, `TRACE`, `PUT`, `PATCH`, `DELETE` on any API path | 404 `not_found`, no `Allow` (RC-01); on `/healthz`: 405 + `Allow: GET, HEAD` |
| `OPTIONS` with an allowed `Origin` | 204 for GET (any path, even unknown); POST only on allowlisted paths; PUT/other -> 403 `method_denied`; disallowed or `null` Origin -> 403 `origin_denied` on every path including unknown ones |
| trailing slash (`/v1/session/`, `/healthz/`) | 404 |
| `//`, `/./`, `/x/../` | 307 to the cleaned path (the mux, before any handler) |
| `%2e%2e` segment | 404 (not treated as dot-dot) |
| upper-case literal (`/V1/SESSION`, `/ACCESS`) | 404 |
| percent-encoded literal (`/acc%65ss`) | routed to the same handler (decoded consistently by mux, `AcceptsPOST` and `perform`) |
| `%2F` inside a literal join (`<uuid>%2Faccess`, fullwidth solidus) | 404 |
| `%2F` inside a wildcard value (`/tenants/a%2Fb/access`) | reaches `serve`; with a token -> 403 `tenant.denied` |
| tenant id: upper-case UUID, trailing `%20`, `a%2Fb`, malformed, valid-shape but non-existent, existing-but-not-member | 403 `forbidden`, audit `tenant.denied/tenant/false` in all six cases (no existence oracle); anonymous -> 401 first |
| role name `x%2Fmetadata%2Fy` as owner | 400 `invalid_role` after the policy decision (executed). NFC/NFD (`caf%C3%A9`, `cafe%CC%81`) and `%00` reach `serve` (executed, routing only); their rejection by the ASCII `ValidRoleName` (`definition.go:20`) is from reading, not run with a token |
| two `Authorization` headers (even both valid), `bearer` lower case, `Bearer  x` (two spaces), token only in the query string | 401 `authentication.denied` (`access.go:250-254` accepts exactly one header with the exact prefix) |
| body on any GET, or on a POST whose decoded path is not allowlisted (including a malformed tenant/app/id) | 413 `body_not_allowed` before routing and before authentication |
| anonymous POST with a 512 KiB body to an allowlisted path | 401, not buffered (pre-read only after a verified bearer, `access.go:286`) |
| raw TCP: header block 17 KiB -> 200; 21 KiB -> 431 | effective limit is `MaxHeaderBytes` (16 KiB) + net/http's 4 KiB slack |
| raw TCP: differing duplicate `Content-Length` -> 400; `Transfer-Encoding: chunked` on GET -> 413 `body_not_allowed`; TE + CL on an allowlisted POST reaches the handler (net/http drops CL); HTTP/1.1 without `Host` -> 400; NUL in the request line -> 400; `Expect: 100-continue` on a non-allowlisted POST -> 413 without a 100 | as listed |
| order of refusals (fake world, all 91 routes) | origin (403) -> body shape (413) -> routing (404/307) -> peer limit (429) -> bearer parse -> invalid-credential limit (429) -> 401 -> tenant (403) -> app shape (400) -> route policy (403) -> handler. A member with a malformed `{app}` gets 400 `invalid_application` **before** the policy decision (audit `<action>.rejected`); an outsider gets 403 first. Nothing in that order depends on a resource existing. |

## 5. Findings

| id | severity | where | what |
|---|---|---|---|
| RC-01 | LOW | `httpserver.go:95-102` | Wrong method on a routed API path answers 404 `not_found` with no `Allow` header on all 91 patterns (`DELETE` probe: 404 x 91, 405 x 0). The catch-all `/` pattern shadows the mux's own 405. The M0 report (`M0_COMPLETION_REPORT.md:143`) says unsupported methods answer 405 with `Allow`; that holds only for `/healthz` and `/readyz`. No oracle (uniform with unknown routes). Repro: `httptest.NewRequest("DELETE","/v1/session",nil)` through `httpserver.Handler{Register: api.Register}`. |
| RC-02 | LOW | `access.go:540,543,552,555,561,564,570` (string tests on the decoded `r.URL.Path`) vs the mux, which matches escaped segments | A `%2F` inside a wildcard value changes the class of the request. Executed: `GET T/metadata/app.orders/versions/1%2Fpreview%2Fx` as viewer is judged `preview.read` and dispatched to the preview handler (400 `invalid_preview`, audit `preview.read.rejected`); `.../versions/1%2Fpreview%2Fevolutions` is judged `datasets.evolve` (403); `.../versions/1%2Fpreview%2Fx%2Fdatasets` as owner runs the dataset handler (400 `invalid_dataset`); `GET T/members/x%2Fpreview%2Fy` audits `preview.read.rejected`. **No privilege gain found**: the action checked is the action of the handler that then runs, POST routes cannot be spoofed because their last segment is a literal and the allowlist regexes see the decoded path, and no case reached data. It is fragile and mislabels audit evidence. Fix: classify by `r.Pattern` only, as sandbox, release, AI, role and team already do. Repro: the three paths above with a viewer/owner token. |
| RC-03 | LOW | `access.go:539-554` | The route-to-action defaults are permissive: any unmapped path under `/preview/` is `preview.read` (every role), under `/metadata/` `metadata.read`, anything else `tenant.access` plus the generic body. The policy engines are default-deny (executed), so only the route table is fail-open; today 0 of 91 routes fall through unintentionally: 13 land on `preview.read` (version and binding reads, entity `query`, rule evaluation, record-access GET, the six workflow routes), each a read or guarded by an inner decision, and 78 map to 31 other explicit actions. A new route added without a mapping becomes readable by viewers. No test asserts a pattern-to-action table. |
| RC-04 | LOW | `ai.go:37` ("ai_test.go compares them"), ADR104 A7 ("drift-tested against the body allowlist") | The claimed drift test does not exist: no `ai_test.go`, no test references `aiPOST` (only the release family has one, `promote_test.go:35`). Executed here instead: all 5 AI POST patterns are accepted by `aiPOST`, the 2 GET patterns are not, and none of 13 AI-shaped sample paths without a route is accepted. |
| RC-05 | LOW | `ai.go:67`, `httpserver.go:182`, ADR104 A7 | The ADR says a non-UUID `{id}` is 404 `not_found`; for `POST .../ai/proposals/{id}/apply|confirm|reject` with a body, a non-UUID or upper-case id is refused 413 `body_not_allowed` by the front door (executed: `not-a-uuid`, upper-case UUID). A client that sends no body gets the documented 404 (not run with AI configured). Fix the sentence or the allowlist. |
| RC-06 | LOW | `metadata.go:43`, `ai.go:101` vs `preview.go:33-38`, `datasets.go:35-47`, `roles.go` | Media-type and size errors have no common contract. Wrong `Content-Type`: 415 `unsupported_media_type` on metadata POST and AI, 400 with a route code elsewhere (`invalid_role`, `invalid_team`, `invalid_release_request`, `invalid_dataset` - executed). Over the cap: 413 `body_too_large` on metadata (1 MiB+10) and AI, 400 on roles (`invalid_role`), candidates (`invalid_release_request`), entity commands (`invalid_query_or_command`). ADR104 A7 does not mention 415. |
| RC-07 | LOW | `README.md:193-205` | The only route table lists 11 of 91+; no machine-readable route contract; no route x role test. This report's tables are the only enumeration. |
| RC-08 | LOW | `policy.go:138-146` vs `definition.go` `validResource` | `Native` allows `preview.read`, `preview.write` and `metadata.read|compile|write` without requiring `AppID`, the custom-role path requires it, and the other preview-class actions require it. Unreachable today (`access.go:609-620` validates the app first); executed: owner allowed `preview.read` with an empty `AppID`. Defense-in-depth inconsistency. |
| RC-09 | LOW | `definition.go:171` | A role named `viewer` may be granted `workflows.cancel|approve|reject|recover|reconcile` (`GRANTABLE` table), more than the built-in viewer holds and against the "never viewer" intent in `policy.go:100-102`. Executed at policy level only; the database-side workflow ports were not run. |

Repro for each finding (the probes are not in the repository; each is a few lines):

* RC-01: `httpserver.Handler(Options{Register: api.Register, AllowPOST: api.AcceptsPOST})` and `httptest.NewRequest("DELETE", "/v1/session", nil)` -> 404 `not_found`, no `Allow`; the same for `/healthz` -> 405.
* RC-02: with a viewer token, `GET /v1/tenants/<A>/metadata/app.orders/versions/1%2Fpreview%2Fevolutions` -> 403 and the audit row `datasets.evolve`; `.../versions/1%2Fpreview%2Fx` -> 400 `invalid_preview` and the row `preview.read.rejected/preview`.
* RC-03: `API.Register` plus a new `mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/x", a.serve)` is judged `preview.read` for a viewer (read `perform` at `access.go:552`).
* RC-04: `grep -rn "aiPOST" --include=*_test.go .` and `ls internal/platform/access/ai_test.go` find nothing.
* RC-05: `POST /v1/tenants/<A>/preview/app.orders/ai/proposals/not-a-uuid/apply`, `Content-Type: application/json`, body `{}`, any token -> 413 `body_not_allowed`.
* RC-06: owner `POST .../roles` with `Content-Type: text/plain` -> 400 `invalid_role`; owner `POST .../metadata/app.orders/versions` with the same header -> 415 `unsupported_media_type`; a 1 MiB+10 body -> 400 `invalid_role` on roles, 413 `body_too_large` on metadata.
* RC-08: `(policy.Native{}).Decide(ctx, Subject{User: u, Access: ownerAccess}, "preview.read", Resource{TenantID: t, Type: "preview"}, Context{})` -> `Allow == true`.
* RC-09: `policy.CompileDefinition([]byte(`{"schema_version":"tenant-capabilities-v1","name":"viewer","grants":[{"effect":"ALLOW","actions":["workflows.approve"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`), "")` returns no error.

Observations that are not findings: sign-in routes and health probes emit no audit row (ADR039 is silent; the bearer comes from the identity provider); `HEAD` is served by every GET route and audited; the effective header limit is 20 KiB, not 16 KiB; `WriteTimeout` 10 s is extended per AI exchange (`access.go:428`).

## 6. Attacks that held (each with the executed input)

* Anonymous or garbage-token request to each of the 91 routes: 401 x 91, audit `authentication.denied` (fake world, `TestZZFakeWorldMatrix`).
* Outsider (valid token, member of another tenant) to each of the 90 tenant routes: 403 x 90, audit `tenant.denied`; existing, non-existent, malformed, upper-case, `%20`-suffixed and `%2F`-containing tenant ids give the same 403 (`TestZZFakeClassification`): no tenant-existence oracle.
* Viewer, member, admin and owner against every route: 364 cells (91 routes x 4 roles) compared by script with the `Native` row of the route-level action recorded for that route (403 against not-403): 0 mismatches, so no route is more or less permissive than its action.
* Duplicate, lower-case, wrong-scheme, double-space and query-string credentials: 401.
* Wrong action name: `no.such.action` denied by `Native` and `Defined`; every action on a foreign resource type or another tenant's resource denied.
* Custom roles: no implied grants (47 single-action roles), DENY precedence, selector scoping, subject binding.
* Body smuggling shapes: differing `Content-Length` -> 400, chunked GET -> 413, body on an unlisted POST -> 413 before routing, anonymous 512 KiB POST not buffered.
* Path forms: dot segments and double slashes -> 307 to the clean path, `%2e%2e` -> 404, upper-case literals -> 404, no handler reached with a different decoded path than the mux's.

## 7. NOT RUN

* The m13 database probe (`TestZZRoutesMatrix`, `TestZZSemanticsDB`: the same 91 x 7 matrix, the audit rows read back from `audit.entries`, a published custom role assigned to a member, the oversized and media-type cases against real handlers) was submitted through `it-run m13` and stayed queued behind three other agents for more than 12 minutes; it was withdrawn. Everything in sections 3.4 and 4 therefore comes from the fake-SQL world plus the real mux and handlers, not from PostgreSQL. Not shown against a real database: RLS-level cross-tenant 404 vs 403 for resource ids inside an authorized tenant, the custom-role grant path through `control.publish_role`, `HEAD` on audit rows in `audit.entries`.
* Handler behaviour behind the route-level gate (the 503 cells): not run.
* AI routes with a configured provider (404 for unknown ids, `token_expiring`): not run.
* Production wiring with `PreviewEnabled=false`: only the registration count (18 patterns) was run.
* Frontend client routes (`apps/web/src/api/*.ts`) were read for the URL shapes (`/v1/session`, `.../access`, `.../preview/{app}/versions/{rev}`, administration and roles roots) and match registered patterns; the frontend tests were not run.
