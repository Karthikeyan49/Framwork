# Database Bootstrap

Schemas: `iam`, `control`, `runtime`, `audit`.

Sequence:
1. create admin/migration role
2. create normal least-privilege app role
3. create schemas/tables
4. apply RLS policies
5. seed global metadata
6. create test tenants
7. run isolation suite

The normal app role must not own tenant tables and must not have BYPASSRLS.
