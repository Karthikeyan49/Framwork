# AI Correctness and Non-Determinism

## Failure mode
AI can create plausible but wrong business logic.

## Solution
AI outputs an `ApplicationPatch`, never executable production code.

Pipeline:
intent → semantic context → structured patch → JSON schema → compiler → policy checks → generated tests → sandbox → human approval.

Patch contains:
- proposed changes
- assumptions
- unresolved requirements
- risk flags
- test suggestions

Required unresolved items block promotion.

Store model/provider, template version, context hash, output hash and compiler version for traceability.

## Verification
Adversarial prompts, repeated generation, compiler rejection corpus, sandbox acceptance tests.
