# Tenant Isolation Verification

Automate:
other-tenant get/list/update/delete/insert; forged tenant header/body; relationship join; aggregates; export; files; cache collision; background queue context; admin impersonation misuse.

Run using the normal application DB role, never superuser/table owner.

Any cross-tenant leak blocks release.
