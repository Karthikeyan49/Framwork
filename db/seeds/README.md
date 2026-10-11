# Seed data

No automatic users, fixed administrators or business seeds. Local database roles
are initialized by infra/compose/postgres-init.sh; schemas/tables belong to embedded
migrations. M1 users/memberships require the explicit cmd/iam operator command in
README.md, with role/status flags and atomic audit. Local signing keys are generated
only for development/test and never grant membership. Integration tests use unique
fixtures, clean up IAM records and preserve append-only audit evidence.
