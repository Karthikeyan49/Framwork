-- Purpose: bounded provisioning state and database writer provenance.
-- Compatibility: additive / SAFE, defaults preserve existing audit evidence.
-- Recovery: transaction rollback on error, forward fix; never rewrite history.
-- +goose Up
ALTER TABLE audit.entries ADD COLUMN state jsonb NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(state)='object' AND octet_length(state::text)<=2048);
ALTER TABLE audit.entries ADD COLUMN writer_role name NOT NULL DEFAULT session_user;
REVOKE INSERT ON audit.entries FROM framework_app;
GRANT INSERT (actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state)
    ON audit.entries TO framework_app;
-- +goose Down
-- Forward fixes only; append-only evidence is never discarded by the CLI.
ALTER TABLE audit.entries DROP COLUMN writer_role;
ALTER TABLE audit.entries DROP COLUMN state;
