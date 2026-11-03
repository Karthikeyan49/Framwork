# Red team of the M10 tools gateway and the M11 sandbox (final verification round)

## Progress

- Pin `11a4ee4` (code head `585499d`), branch `review/tools-sandbox-redteam`.
- Read: ADR100, ADR101, `internal/tools/*`, `internal/runtime/workflow/tool_step.go`, migrations 00080/00081/00085/00086, `internal/platform/access/sandboxes.go`, policy actions, the three earlier reviews (already attacked: gateway F1-F9, M10R-01..05, M11 R01-R07).
- No database stack: every probe is a unit test with loopback httptest servers, run through `/home/user/bin/gt`.
- Status: probes being written (report incomplete).
