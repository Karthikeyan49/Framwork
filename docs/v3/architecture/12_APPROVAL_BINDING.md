# Approval Binding

Correct order:

Draft → Compile → Sandbox → Acceptance Tests → Final Candidate Bundle → Content Hash → Human Review → Approval of Exact Hash → Activate Same Hash.

No recompile after approval.

If base release, schema snapshot, policy bundle or relevant tool catalog changes, candidate becomes STALE and must be rebuilt, retested and reapproved.

For high-risk changes require requester != approver.
