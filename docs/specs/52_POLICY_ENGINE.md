# Policy Engine Contract

`Decide(subject, tenant, action, resource, context) -> Decision`

Decision:
allow, reason, obligations, decision_id, policy_version.

Enforcement points:
API, field/query access, commands, workflow activities, tools, promotion, admin operations.

OPA/Rego can implement the decision point later/now, but enforcement remains in the Go application.
