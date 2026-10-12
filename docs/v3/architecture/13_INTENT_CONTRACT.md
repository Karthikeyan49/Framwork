# Intent Contract

Natural-language requirements become typed semantic contracts.

Example:
```yaml
intent_type: approval_rule
entity_id: purchase
field_id: amount
operator: GT
threshold:
  amount: 500000
  currency: INR
approver_role: finance_manager
trigger: submit
```

Missing required semantics become `UNRESOLVED_REQUIREMENT` and block promotion.

Business Acceptance Spec is derived from intent, independently of generated metadata:
- 499999 => no approval
- 500000 => no approval
- 500001 => finance approval
