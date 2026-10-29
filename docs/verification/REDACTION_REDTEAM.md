# Independent red team of the prompt redactor (`internal/ai/redact*.go`)

Branch `review/redaction-redteam`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`). Scope: `internal/ai/redact.go`,
`redact_fold.go`, the fields that reach a prompt (`context.go`, `repair.go`, `template.go`) and every path that carries author text to a model, a
stored row, an audit row, a log or a response. Written by an agent that did not write the redactor and changed no product code, test or ADR.
All secret-shaped inputs were generated at run time; this file contains prefixes and shapes only.

## Progress (kept current)

| time (UTC) | state |
|---|---|
| start | brief read; redactor, context builder, repair loop, gateway, service, store, route code read in full |
| +45 min | Part A matrix run (class x carrier, 6 samples per cell, about 3,000 cells); first findings below are PRELIMINARY and being verified |
| next | Part C integration probe on the `fix` stack; Part B false-positive corpus, timing, properties, fuzz, mutations; patch verification |

Verdicts use PASS / FAIL / NOT RUN only. Nothing below is final until the "Results" section is filled in.

## Preliminary observations (unverified until the Results section)

- The `assignment` rule needs the separator directly after the name: a quoted key (`"password": "..."`, `'password': '...'`, `os.environ["DB_PASSWORD"] = "..."`)
  defeats it. NOT yet minimised.
- Compound names without `_`/`-` (`dbPassword`, `PGPASSWORD`, `apiSecret`) do not match the `\b` before the name. NOT yet minimised.
- A key hard-wrapped so that its first line is shorter than its rule's minimum leaves both halves. NOT yet minimised.
