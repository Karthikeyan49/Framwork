# Worker/DB Role Verification

Test:
- dispatcher sees routing metadata but not arbitrary business records
- worker A cannot read tenant B
- transaction-local tenant context cannot leak through pool reuse
- migration/maintenance roles are unavailable to normal runtime
- no runtime role owns tenant tables or bypasses RLS
