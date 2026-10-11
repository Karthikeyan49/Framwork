# Target Architecture

```mermaid
flowchart TB
  U[Users/Admin] --> WEB[React + TypeScript]
  WEB --> API[Go API / Modular Monolith]

  subgraph CORE[Go Core]
    IAM[Identity + Tenant Context]
    POL[Policy Layer]
    META[Metadata Registry]
    COMP[Application Compiler]
    DATA[Query/Data Runtime]
    RULE[Rule Runtime]
    WF[Workflow Runtime]
    TOOL[Tool Gateway]
    EVT[Transactional Outbox/Event Dispatcher]
    SAN[Sandbox Manager]
    PROMO[Promotion Manager]
    AI[AI Builder / Model Gateway]
    AUD[Audit + Observability]
  end

  API --> IAM --> POL --> META --> COMP
  COMP --> SAN --> PROMO --> DATA
  DATA --> RULE --> WF --> TOOL
  CORE --> PG[(PostgreSQL)]
  CORE --> RD[(Redis)]
  CORE --> OS[(Object Storage)]
  CORE --> OTEL[OpenTelemetry]
  AI --> LLM[Claude / GPT / Other]
  TOOL --> EXT[External Systems]
  WASM[WASM Extensions - Later] -. explicit capabilities .-> TOOL
```

## Why modular monolith first
- easier transactions
- fewer deployment failure modes
- easier end-to-end tracing
- simpler for a small team and coding agents
- preserves clean extraction boundaries for future scale

## Future extraction candidates
AI gateway, workflow workers, integration workers, analytics/reporting, WASM host.
