-- Constrain tenant-scoped IAM/team reads as well as business data to the job.
-- Global self-identity and the static role catalog remain readable for binding.
-- +goose Up
CREATE POLICY worker_membership_boundary ON iam.memberships AS RESTRICTIVE FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(tenant_id) AND user_id=iam.authenticated_actor());
CREATE POLICY worker_tenant_boundary ON iam.tenants AS RESTRICTIVE FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(id));
CREATE POLICY worker_team_boundary ON iam.teams AS RESTRICTIVE FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(tenant_id));
CREATE POLICY worker_team_membership_boundary ON iam.team_memberships AS RESTRICTIVE FOR SELECT TO framework_worker USING(iam.worker_tenant_scope(tenant_id));
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'never broaden worker tenant isolation';END $$;
-- +goose StatementEnd
