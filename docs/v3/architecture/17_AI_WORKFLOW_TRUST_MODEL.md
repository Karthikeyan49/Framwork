# AI Workflow Trust Model

AI activities are typed analysis/transformation operations.

AI cannot directly authorize high-risk:
payment, refund, payroll, deletion, permission changes, legal filing.

Flow:
AI result → deterministic rule → policy → approval where needed → Tool Gateway.

Retrieved documents, emails and external tool outputs retain untrusted provenance.
