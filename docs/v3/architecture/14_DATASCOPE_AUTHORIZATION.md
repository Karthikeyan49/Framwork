# Record-Level Authorization / DataScope

Policy answers:
1. Action decision: may subject perform an operation?
2. DataScope: which records may subject access?

DataScope compiles into a typed Query AST:
owner, team, territory, hierarchy, explicit shares, domain-specific predicates.

Never fetch all rows then filter in UI/application memory.
Never rely on one policy call per list row.
