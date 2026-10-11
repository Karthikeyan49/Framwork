# policy

M1 Engine.Decide accepts a Subject containing opaque identity.User and optional
tenant.Access. It is the central RBAC + ABAC port. Native grants only explicit
M1 actions: self-owned session discovery, or database-resolved tenant roles with
matching trusted actor IDs and tenant/type/ownership attributes.
Unknown roles/actions/scopes deny; decisions include ID/version and obligations.
The enforcement point must implement every obligation and commit audit before
returning protected data. OPA may later replace the adapter. See ADR-014.
