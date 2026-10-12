# Governed Generic HTTP Connector

Configuration:
allowlisted host, allowed methods, path templates, input/output schema, secret references, timeout, rate limit, idempotency capability, sandbox endpoint, response size limit.

Block by default:
localhost, loopback, private network ranges unless explicitly internal, cloud metadata endpoints, arbitrary redirects to unapproved hosts.

Every call passes Tool Gateway authorization and network policy.
