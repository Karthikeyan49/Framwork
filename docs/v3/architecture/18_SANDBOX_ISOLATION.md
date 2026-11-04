# Sandbox Isolation

Same runtime implementation, safer context.

Data modes:
1. separate sandbox record schema/tables with synthetic data
2. controlled redacted snapshot
3. high-assurance ephemeral dedicated DB

Tool modes:
block, simulate, reroute, read-only, dedicated test account.

Production credentials are not mounted in ordinary sandbox execution.
