# AI output red team (M14 proposal pipeline, routes and database ports)

Branch `review/ai-output-redteam`, from `11a4ee40a12e0301472b275203ac10d8347eaad9`. Independent red team; no product code, test, ADR or other document was changed. Probe files (`zz_review_*_test.go`) lived only in the worktree and are not committed.

## Progress

| state | detail |
|---|---|
| Part A items (classes 1-3, 5-7, 9) run | 645 items through the builder's expander/checker, the scripted gateway and the pure functions: 0 panics, no hostile answer caught later than its declared layer; one family of expectation failures (36 items, AO-01). |
| Part A class 4 run | 45 payloads in 6 equivalence groups plus requirement semantics: one canonical form per meaning, no collision. |
| Part A class 5 sweep run | 3,202 single-definition mutants over 7 drafts; 677 compile; 304 change the executed IR; 0 under-reported, 0 over-reported. |
| Part A class 8 run | 12 loop scenarios, 3 diagnostic floods, 300 cancel-during-call races, 1 budget case: all held. |
| Part A class 10 run | 38 hostile HTTP provider responses (gzip bomb, truncation, chunked without end, slow drip, 20 KB header, redirects, Content-Length lies, envelope variants, 13 status codes) plus 2 label cases: all held. |
| Part B/C | `TestZZB1` ran: AO-02 confirmed. B2..B5, C1, C2 queued behind the shared lock (log `zzaiout-2`). |

## Verdict

Not yet decided.

## Part A results (pure pipeline, no database)

Items run through `corpus.Library.Expand` + `scriptedRun` + `Case.Check` (the builder's own expander, scripted provider, strict parser, `ApplyProposal`, real compiler, `DeriveSensitiveAspects`) and also through the pure functions to record the refusal layer, the canonical fixed point (`Parse(canonical)` re-canonicalises to itself, checked on every accepted item) and the time.

| class | items | READY (stored) | refused at parse | refused later (conflict/compile) | panics | max per item |
|---|---|---|---|---|---|---|
| 1 authority | 135 | 3 (advisory text only) | 132 | 0 | 0 | 52 ms |
| 2 forbidden targets and ids | 122 | 0 | 119 | 3 | 0 | 10 ms |
| 3 strict decoding | 124 | 21 (valid by design) | 98 | 5 | 0 | 30 ms |
| 5 sensitivity (hand-written) | 40 | 33 | 1 | 6 | 0 | 53 ms |
| 6 requirement proposals | 96 | 18 | 78 | 0 | 0 | 4 ms |
| 7 free text | 104 | 87 | 11 | 6 | 0 | 27 ms |
| 9 resource | 24 | 13 | 6 | 5 | 0 | 154 ms |
| total | 645 | 175 | 445 | 25 | 0 | 154 ms |

Class 4 (canonical form) is the 45-payload differential below; class 5 additionally has the 3,202-mutant sweep.

## Findings

### AO-01 LOW: `holdsRedaction` refuses only the exact placeholder bytes

`internal/ai/redact.go:460-463` (`holdsRedaction`, used at `internal/ai/patch.go:124`) is `strings.Contains(json.Marshal(definition), "[REDACTED:")`. ADR104 B.4 states that a placeholder "the model echoes back can never be written into the draft". Only the byte-exact prefix is refused. Executed (`zz-a7-placeholder-{label,component,enum}-*`, 12 spellings at 3 positions = 36 items, all stored `READY`): `[redacted:secret]`, `[Redacted:secret]`, `[REDACTED :secret]`, `[ REDACTED:secret]`, `[REDACT<U+200C>ED:secret]`, `[REDACTED<U+200D>:secret]`, `[REDAC<U+00AD>TED:secret]`, fullwidth brackets and colon, a Cyrillic E, `[REDACTED` without the colon, `&#91;REDACTED:secret]` and the escape `\u005bREDACTED:secret]` all pass; the exact `[REDACTED:secret]` and `see [REDACTED:pem] above` are refused. Repro: `gt test -count=1 -run TestZZReviewA567 ./internal/ai` with the probe in the appendix (any `update` of `page.order.list` whose label is `[Redacted:secret]`).
Breaks: a model (or a prompt-injected one) can overwrite a label, a component label or an enum value with text that a reviewer reads as the server's placeholder; the original text is lost from the draft revision and nothing marks the lookalike. No secret is exposed and a human still applies, so LOW. Zero-width and soft-hyphen variants are invisible in a review UI.

### AO-02 MEDIUM: `ai.read` and `ai.propose` reach draft definitions that `metadata.read` protects (no composition rule for reads)

`internal/platform/access/ai.go:266-310` authorises the read and propose routes with `ai.read` / `ai.propose` alone; the composition rule of ADR104 A6 exists only for the writes (`ai_decide.go:39` `metadata.write` for apply, `ai_decide.go:79` `requirements.decide` for confirm). `Service.Admit`/`Prepare` (`internal/ai/service.go:242-309`) builds the context from the whole draft inside the caller's scope, and a proposal view returns the full definition of every definition it updates.
Executed (`TestZZB1CustomRolesReachTheDraftThroughTheAISurface`, `it-run m12db`, tenant A): a stored role holding only `ai.read`: `GET .../metadata/{app}/versions/1` answers 403, yet `GET .../preview/{app}/ai/proposals/{id}` of a proposal created by the owner answers 200 with the full `workflow.order.approve` definition (states, approval roles; 1,172 body bytes). A stored role holding `ai.read` + `ai.propose` (no `metadata.read`): the request sent to the provider contains the whole draft (`workflow.order.approve`, `field.order.amount` present in the segments), and the READY proposal it created returns the whole `entity.order` field list; by choosing which definition to relabel it can read any definition back.
Breaks: default-deny at the capability level. A custom role created to audit or to ask the model (a plausible role) reads, and has sent to the operator's provider, metadata it cannot read through the metadata routes. Default roles are unaffected (`ai.propose` is owner/admin only, who hold `metadata.read`; viewers hold both reads). The ADR states the composition for writes only, so this is a gap in the stated contract rather than a defect against it.
Fix direction (not applied): require `metadata.read` for the list/get/propose routes, as apply requires `metadata.write`, or document that granting `ai.read`/`ai.propose` grants read of draft definitions.

### AO-03 LOW: a NEEDS_HUMAN proposal reports `derived.sensitive: []` and `aspects: []` for changes that are sensitive

`internal/ai/gateway.go:146` sets `run.Derived = Derived{Targets: targetsOf(*parsed), Sensitive: []string{}, Aspects: []string{}}` when the loop ends with diagnostics, and `internal/ai/service.go:559-564` stores it with the proposal, so the API view of a NEEDS_HUMAN proposal says "no sensitive aspects" about content that was never classified. Executed (gateway, scripted provider, orders draft): one answer that deletes `workflow.order.approval` and `rule.order.high_value` and adds a page that references a missing field ends `NEEDS_HUMAN` with `derived.targets` = the three operations and `derived.sensitive = []`, `aspects = []`; the same two deletions without the broken page are `READY` with `sensitive = [rule.order.high_value:RULE_FINANCIAL, rule.order.high_value:RULE_LOGIC, workflow.order.approval:APPROVAL_GATE, workflow.order.approval:WORKFLOW_GRAPH]`.
Breaks: `derived` is documented as what the server computed (ADR104 A9). NEEDS_HUMAN cannot be applied through the API (`proposal_not_ready`), but its purpose is that a human takes over; a reviewer who reads an empty sensitive list and re-does the change through the ordinary metadata routes meets none of the acknowledgement. Fix direction: leave `sensitive`/`aspects` null (unknown) for NEEDS_HUMAN, or classify the operations that apply.

### AO-04 LOW: the invisible-character rule for model text leaves the common smuggling characters open

`cleanText` (`internal/ai/schema.go:304-319`, applied to every string and key of a proposal) names "the invisible-instruction smuggling channel" and refuses C0/C1 controls, bidi overrides and isolates, U+2028/2029, U+FEFF, U+200B, U+2060, U+180E and the tag block U+E0000-E007F. Executed (probe over all 1.1 million code points, then `zz-a7-rationale-char-*`): 326 invisible or format characters still pass as rationale, assumption, risk-flag detail and, through `ApplyProposal`, as definition labels: variation selectors U+FE00-FE0F and **U+E0100-E01EF** (the supplement block used for hidden-text injection), U+200C/U+200D, U+200E/U+200F, U+061C, U+00AD, U+034F, Hangul fillers U+115F/1160/3164/FFA0, U+17B4/17B5, Mongolian selectors U+180B-180D, invisible operators U+2061-2064, U+FFF9-FFFB, U+13430-1343F. The human's request rule (`validRequestText`, `internal/ai/service.go:156-175`) is stricter than the model-output rule: it also refuses U+061C, U+200E/200F, U+2061-2064 and U+FFF9-FFFB (10 of the 326); the input-side `dirtyRune` (`internal/ai/redact.go:358-372`) is the same list as `cleanText`, so these characters also reach later prompts.
Breaks: a hidden payload in a label that a human applies is stored in the draft, returned, and placed in later contexts as UNTRUSTED data (a persistent injection channel the code comment says it closes), and a reviewer cannot see text that the model attached to a rationale. Defence in depth only (the segments are labelled UNTRUSTED, a human applies), hence LOW. Fix direction: refuse all `unicode.Cf` characters and the default-ignorable code points in model text, and make the request and output rules one function.

## Attacks that held

- Authority: 135 spellings of server-derived names (case, kebab, space, fullwidth, Cyrillic, zero-width, soft hyphen, RLO, `__proto__`, `$schema`, `@type`) at five positions (top level, operation, definition, definition payload, risk flag): all refused at parse. Three advisory-text items (risk flags and assumptions claiming a cosmetic change, an open question claiming prior approval, a proposed test asserting NO_APPROVAL) were stored READY and left the derived sensitive list intact (`zz-a1-advisory-claims-do-not-lower-sensitivity`, `zz-a1-open-question-claims-approval`).
- Sensitivity: an independent oracle (the compiled IR minus labels and pages, compared as JSON, not `compiler.ComputeDiff`) agreed with `DeriveSensitiveAspects` on 3,202 mutants (every leaf of every definition of orders, orders+tool-pin, vendors, minimal and three published examples: string alternatives, numbers, booleans, array removal/swap/duplicate/reverse, key removal, definition deletion and renamed copies). 133 READY hand-written items re-checked against the IR oracle: 0 changed IR with an empty sensitive list. The builder's own `oracleAspects` is `ComputeDiff` again and so is not independent of the code under test; this oracle is.
- Aliasing and determinism: 158 applied documents were vandalised through every slice, map and `json.RawMessage` they hold; the input draft and the proposal never changed and a second `ApplyProposal` gave the same bytes.
- Canonical form (`zz_review_canon_test.go`): 11 spellings of one integer, key order, whitespace, escapes (`é`, `<`, `\/`, surrogate pairs), `null` versus absent definition, empty versus absent `expected_hash` canonicalise to one document and hash; NFC/NFD, a trailing space, case, `&lt;` and array order do not. `500` and `500.00` in requirement semantics hash differently (text preserved), key order does not change the hash.

## NOT RUN

Classes 8 and 10, Part B and Part C: not yet run in this version of the report.
