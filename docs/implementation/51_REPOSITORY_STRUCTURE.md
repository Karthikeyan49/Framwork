# Repository Structure

```text
/
├── CLAUDE.md
├── AGENTS.md
├── docs/
├── apps/web/
├── cmd/api/
├── cmd/worker/
├── internal/
│   ├── identity/
│   ├── tenant/
│   ├── policy/
│   ├── metadata/
│   ├── compiler/
│   ├── runtime/data/
│   ├── runtime/rules/
│   ├── runtime/workflow/
│   ├── tools/
│   ├── events/
│   ├── sandbox/
│   ├── promotion/
│   ├── ai/
│   ├── audit/
│   └── observability/
├── contracts/jsonschema/
├── db/migrations/
├── db/rls/
├── tests/{integration,e2e,security,performance}/
├── infra/{compose,otel}/
└── scripts/
```

Keep module storage private. Cross-module calls use explicit interfaces/contracts.
