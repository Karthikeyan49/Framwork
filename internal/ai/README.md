# ai

Model gateway, semantic context builder and proposal pipeline (ADR040, M14).

A model holds no authority and has no identity. Everything it returns is parsed into a
closed, typed, hashed proposal that an authenticated human applies, confirms or rejects.
The public contract is `contract.go`; storage (`control.ai_*`) and the HTTP surface
(`internal/platform/access/ai.go`) are owned by the persistence/API layer. No other module
may import a provider or build a prompt. Status: preview; no real model was called.
