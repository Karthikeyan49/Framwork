# Migration Playbook

Every migration plan records:
purpose, affected releases, compatibility class, forward steps, backfill, row estimate, lock risk, validation queries, recovery/forward-fix, contract cleanup date.

Long backfills run as checkpointed jobs.

Dangerous contract changes require elevated approval and are separated from the feature activation release.
