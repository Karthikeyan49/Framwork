# V3 authority and current remediation

Read all docs/v3 Markdown and docs/migration/M0_M6_AUDIT_REPORT.md. V3 is authoritative
where it supersedes V2; retained V2 specs supplement unchanged modules. M6R only is
authorized now; M6 had confirmed open issues. Never begin M7 until M6R independent
review has no BLOCKER/HIGH findings. Preserve existing data, migrations and safe code.
No tenant DDL; immutable stored production IR; exact hash approval; SQL DataScope;
separate worker/dispatcher roles; transactional audit/outbox; tool-specific guarantees.

# Implementation contract

Read and obey AGENTS.md, README.md, docs/00_MASTER_PLAYBOOK.md, the architectural
source documents, relevant milestone verification, and docs/adr before coding.

Current scope is M6R remediation over retained M0–M6. Do not implement M7 rule execution or
later business behavior without a new user task. Implement the smallest coherent
milestone, run the documented commands, record actual results and recovery notes,
then request independent review per docs/agents/82_TWO_AGENT_PROTOCOL.md. Review
findings must be fixed and reverified before claiming completion.

The core invariants are: AI drafts only; deterministic runtime; trusted tenant
context; server-side default-deny policy; least-privilege PostgreSQL + RLS;
immutable metadata; sandbox before approval/promotion; transactional outbox;
idempotent governed side effects; secret-free observability. Never introduce
generic execute-SQL/shell tools or arbitrary metadata code execution.

Read docs/adr/015-m2-tenant-data-boundary.md for runtime transaction ownership,
verified transaction-local scope, RLS installation, tenant keys/FKs and recovery.
Do not expose repository SQL executors to AI, metadata or request payloads.

Read docs/adr/016-m3-metadata-source-registry.md for strict authoring schemas,
immutable draft revisions, quota/hash round trips, expected-revision concurrency,
mandatory policy/audit and forward-only recovery. Schema-valid is not executable.

Read ADR017–020 for compiler/preview-data/dynamic-UI contracts. Raw source never
executes; production preview routes are absent. Enforce action permission plus SQL DataScope before reads and mutations; never
fetch unrestricted rows to filter policy in memory, automatically retry mutations, or evaluate metadata code.
