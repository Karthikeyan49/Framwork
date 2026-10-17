# sandbox

`Context` (context.go) is the M6R caller-declared sandbox contract carried by bundle
1.0.0; it is not an authority. The server-owned M11 sandbox does not live in this
package: identity, EMPTY-only creation, freeze evidence and the tool-mode ceiling are
implemented in migration 00046, `internal/runtime/data` (CreateSandbox, FreezeSandbox,
Evidence), `internal/platform/access/sandboxes.go` and `internal/tools/mode.go`
(see docs/adr/037-m11-sandbox-v2.md). Other modules must not import private
subpackages or access module tables; see docs/architecture/12_MODULE_BOUNDARIES.md.
