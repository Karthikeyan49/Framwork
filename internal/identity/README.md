# identity

M1 owns cryptographic bearer authentication, opaque verified Principal/User types
and PostgreSQL users keyed by issuer/subject. Public ports: Authenticator,
NewVerifier, Store.Resolve, Store.Ready, operator-only Store.Provision. Private
signing keys, JWT role claims and tenant claims never enter trusted identity.
Read docs/adr/014-m1-identity-policy-audit.md before changes. Consumers use these
public ports and cannot query iam.users directly.
