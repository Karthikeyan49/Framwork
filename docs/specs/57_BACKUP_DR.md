# Backup and Disaster Recovery

Minimum:
automated PostgreSQL backup/PITR, object-store recovery, metadata export, secret-manager recovery procedure, tested restore.

Define RPO/RTO by service tier and only claim values proven by restore drills.

Restore drill recompiles active metadata and runs application smoke tests.
