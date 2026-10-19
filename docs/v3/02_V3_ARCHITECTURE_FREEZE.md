# V3 Architecture Freeze

```mermaid
flowchart TB
 U[User] --> I[Intent Contract]
 I --> AI[AI Builder]
 AI --> P[ApplicationPatch]
 P --> C[Compiler]
 C --> IR[Canonical IR]
 IR --> S[Sandbox]
 S --> V[Acceptance/Security Verification]
 V --> B[Candidate Release Bundle]
 B --> H[Content Hash]
 H --> A[Approval of Exact Hash]
 A --> X[Atomic Activation]
 X --> R[Go Deterministic Runtime]

 R --> D[Dynamic Record Store]
 R --> DP[Typed ERP Domain Packs]
 R --> RULE[Rule Engine]
 R --> WF[Workflow]
 R --> T[Tool Gateway]
 R --> E[Transactional Outbox]
 R --> POL[RBAC + ABAC + DataScope]
 R --> AU[Audit + OpenTelemetry]

 D --> PG[(PostgreSQL)]
 DP --> PG
 E --> PG
 E --> RD[(Redis/Broker Adapter)]
 T --> EXT[External Systems]
```

## Languages
- React/TypeScript: UI
- Go: core compiler/runtime/platform
- PostgreSQL/SQL: durable state
- JSON/YAML: authoring format
- Rust/WASM: later, only for isolated extensions/hotspots
- Python: later, specialized ML/analytics only

## Style
Keep modular monolith for now.
