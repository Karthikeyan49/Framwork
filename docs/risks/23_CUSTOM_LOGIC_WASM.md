# Custom Logic Ceiling and WASM Escape Hatch

## Three levels
1. Metadata configuration.
2. Deterministic rule/formula DSL.
3. Sandboxed extension only if 1/2 cannot satisfy requirement.

## Future extension model
WebAssembly/WASI capability model:
- explicit imported interfaces only
- no ambient filesystem/network/DB/secrets
- memory/fuel/time/output limits
- signed/versioned package
- tenant and tool scopes
- kill/disable switch

WIT interfaces define host contracts.

## Verification
Attempt unauthorized filesystem/network/secret access; resource exhaustion; malformed module; cross-tenant invocation.
