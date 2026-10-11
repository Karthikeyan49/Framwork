# Promotion Protocol

1. verify approver
2. freeze draft revision
3. compare draft base with current production
4. rebase/recompile if necessary
5. run compiler/security/generated tests
6. validate migration plan
7. collect approvals
8. build immutable hashed release bundle
9. run safe expand migrations
10. atomically activate release pointer
11. smoke test
12. observe
13. schedule destructive contract cleanup separately

Never remove the previous production bundle during activation.
