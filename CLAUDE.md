# Implementation contract

Read and obey AGENTS.md, README.md, docs/00_MASTER_PLAYBOOK.md, the architectural
source documents, relevant milestone verification, and docs/adr before coding.

Current completed scope is M1. Do not implement M2 data-access/RLS infrastructure or
later business behavior without a new user task. Implement the smallest coherent
milestone, run the documented commands, record actual results and recovery notes,
then request independent review per docs/agents/82_TWO_AGENT_PROTOCOL.md. Review
findings must be fixed and reverified before claiming completion.

The core invariants are: AI drafts only; deterministic runtime; trusted tenant
context; server-side default-deny policy; least-privilege PostgreSQL + RLS;
immutable metadata; sandbox before approval/promotion; transactional outbox;
idempotent governed side effects; secret-free observability. Never introduce
generic execute-SQL/shell tools or arbitrary metadata code execution.
