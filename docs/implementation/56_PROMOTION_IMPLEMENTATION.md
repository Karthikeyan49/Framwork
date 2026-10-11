# Promotion Implementation

1. permission/approval check
2. freeze draft
3. base-version conflict check
4. recompile against current catalog/schema
5. security + generated regression suite
6. migration validation
7. release bundle/hash
8. expand migrations
9. atomic active-version switch
10. smoke verification
11. observe
12. schedule contract cleanup separately

Failure before activation leaves old version active.
