-- Narrow the role grants introduced in 00010; timestamps and writer role remain
-- database-generated. Keep previously applied migrations unchanged.
-- +goose Up
REVOKE INSERT ON audit.entries FROM framework_worker,framework_audit_writer;
GRANT INSERT(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state) ON audit.entries TO framework_worker,framework_audit_writer;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'never restore unsafe audit provenance grants';END $$;
-- +goose StatementEnd
