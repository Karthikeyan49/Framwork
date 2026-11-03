# AI output red team (M14 proposal pipeline, routes and database ports)

Branch `review/ai-output-redteam`, from `11a4ee40a12e0301472b275203ac10d8347eaad9`. Independent red team; no product code, test, ADR or other document was changed. Probe files (`zz_review_*_test.go`) lived only in the worktree and are not committed.

## Progress

| state | detail |
|---|---|
| Part A items (classes 1-3, 5-7, 9) run | 645 items through the builder's expander/checker, the scripted gateway and the pure functions: 0 panics, no hostile answer caught later than its declared layer; one family of expectation failures (36 items, AO-01). |
| Part A class 4 run | 45 payloads in 6 equivalence groups plus requirement semantics: one canonical form per meaning, no collision. |
| Part A class 5 sweep run | 3,202 single-definition mutants over 7 drafts; 677 compile; 304 change the executed IR; 0 under-reported, 0 over-reported. |
| Part A classes 8 and 10 | in preparation |
| Part B, Part C | in preparation |

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

(filled as confirmed)

## Attacks that held

- Authority: 135 spellings of server-derived names (case, kebab, space, fullwidth, Cyrillic, zero-width, soft hyphen, RLO, `__proto__`, `$schema`, `@type`) at five positions (top level, operation, definition, definition payload, risk flag): all refused at parse. Three advisory-text items (risk flags and assumptions claiming a cosmetic change, an open question claiming prior approval, a proposed test asserting NO_APPROVAL) were stored READY and left the derived sensitive list intact (`zz-a1-advisory-claims-do-not-lower-sensitivity`, `zz-a1-open-question-claims-approval`).
- Sensitivity: an independent oracle (the compiled IR minus labels and pages, compared as JSON, not `compiler.ComputeDiff`) agreed with `DeriveSensitiveAspects` on 3,202 mutants (every leaf of every definition of orders, orders+tool-pin, vendors, minimal and three published examples: string alternatives, numbers, booleans, array removal/swap/duplicate/reverse, key removal, definition deletion and renamed copies). 133 READY hand-written items re-checked against the IR oracle: 0 changed IR with an empty sensitive list. The builder's own `oracleAspects` is `ComputeDiff` again and so is not independent of the code under test; this oracle is.
- Canonical form (`zz_review_canon_test.go`): 11 spellings of one integer, key order, whitespace, escapes (`é`, `<`, `\/`, surrogate pairs), `null` versus absent definition, empty versus absent `expected_hash` canonicalise to one document and hash; NFC/NFD, a trailing space, case, `&lt;` and array order do not. `500` and `500.00` in requirement semantics hash differently (text preserved), key order does not change the hash.

## NOT RUN

Classes 8 and 10, Part B and Part C: not yet run in this version of the report.
