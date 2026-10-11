# Recovery / Failure Drills

Restore PostgreSQL; replay outbox; restart Redis/workers; simulate external API outage; AI provider outage; object-store failure; metadata rollback.

Core approved ERP CRUD/rules/workflows must continue when AI authoring provider is unavailable, excluding explicitly AI-dependent workflow steps.
