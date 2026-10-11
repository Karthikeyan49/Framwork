# AI Builder

Input:
user intent + semantic graph + existing definitions + tool catalog + policy/capability summary + naming rules + current draft diff.

Output: `ApplicationPatch`
- add/update/deprecate
- rationale
- assumptions
- unresolved requirements
- proposed tests
- risk flags

Repair loop:
patch → compile → machine-readable diagnostics → bounded repair → compile.
After retry limit, require human intervention.
