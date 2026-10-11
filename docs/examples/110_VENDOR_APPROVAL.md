# End-to-End Example — Vendor Approval

User: "Create vendor management; purchases above ₹5 lakh require finance approval."

AI patch proposes Vendor entity/page, Purchase relation, threshold rule, finance approval, notification.

Compiler validates field types, role/tool existence, workflow path, query safety, migration class.

Sandbox uses synthetic vendors/purchases and test notification sink.

User reviews diff and workflow simulation.

Promotion rechecks base, builds immutable release and activates it.

Future purchase processing uses deterministic rule/workflow; no LLM call is needed.
