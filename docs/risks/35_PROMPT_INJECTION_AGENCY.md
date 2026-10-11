# Prompt Injection and Excessive AI Agency

## Principle
LLM reasoning is never authorization.

Controls:
- retrieved docs/email/tool output marked untrusted
- allowlisted tools only
- minimum scopes
- no generic SQL/shell tool
- argument schema validation
- policy check after model selection of tool
- approvals for financial/destructive/irreversible actions
- short-lived credentials
- max steps/time/cost
- output filtering/redaction

Proposal mode is preferred for high-risk actions:
AI proposes → policy/user approves → deterministic tool executes.

## Verification
Direct and indirect injection corpus, malicious document, malicious tool output, privilege-escalation request.
