-- Purpose: M1 IAM records and append-only security evidence; no business tables.
-- Compatibility: additive / SAFE. Runtime credentials remain read-only for IAM.
-- Validation: verified actor visibility, append-only audit, no context leakage.
-- Recovery: transaction rollback on failure; forward fix after application.
-- +goose Up
CREATE TABLE iam.users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 500),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    active boolean NOT NULL DEFAULT true,
    UNIQUE (issuer, subject)
);
CREATE TABLE iam.tenants (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    active boolean NOT NULL DEFAULT true
);
CREATE TABLE iam.roles (
    name text PRIMARY KEY CHECK (name IN ('owner','admin','member','viewer'))
);
INSERT INTO iam.roles(name) VALUES ('owner'),('admin'),('member'),('viewer');
CREATE TABLE iam.memberships (
    tenant_id uuid NOT NULL REFERENCES iam.tenants(id),
    user_id uuid NOT NULL REFERENCES iam.users(id),
    role text NOT NULL REFERENCES iam.roles(name),
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (tenant_id,user_id)
);
CREATE INDEX memberships_user ON iam.memberships(user_id,tenant_id) WHERE active;
CREATE TABLE audit.entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor_id uuid,
    tenant_id uuid,
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 80),
    resource text NOT NULL CHECK (length(resource)<=80),
    allowed boolean NOT NULL,
    decision_id text NOT NULL CHECK (length(decision_id)<=64),
    policy_version text NOT NULL CHECK (length(policy_version)<=80),
    request_id text NOT NULL CHECK (length(request_id)<=64),
    trace_id text NOT NULL CHECK (length(trace_id)<=32)
);
CREATE INDEX audit_tenant_time ON audit.entries(tenant_id,occurred_at);
REVOKE ALL ON ALL TABLES IN SCHEMA iam,audit FROM PUBLIC;
GRANT USAGE ON SCHEMA iam,audit TO framework_app;
GRANT SELECT ON iam.users,iam.tenants,iam.roles,iam.memberships TO framework_app;
GRANT INSERT ON audit.entries TO framework_app;

-- Narrow IAM backstop required by M1's new tenant-scoped records. M2 still owns
-- reusable business-data RLS/data-access infrastructure. No session-level SET.
ALTER TABLE iam.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam.users FORCE ROW LEVEL SECURITY;
ALTER TABLE iam.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam.tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE iam.memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam.memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE audit.entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.entries FORCE ROW LEVEL SECURITY;
CREATE POLICY operator_users ON iam.users TO framework_migrator USING (true) WITH CHECK (true);
CREATE POLICY operator_tenants ON iam.tenants TO framework_migrator USING (true) WITH CHECK (true);
CREATE POLICY operator_memberships ON iam.memberships TO framework_migrator USING (true) WITH CHECK (true);
CREATE POLICY operator_audit ON audit.entries FOR INSERT TO framework_migrator WITH CHECK (true);
CREATE POLICY operator_audit_read ON audit.entries FOR SELECT TO framework_migrator USING (true);
CREATE POLICY authenticated_user ON iam.users FOR SELECT TO framework_app USING (
    active AND issuer=current_setting('app.auth_issuer',true) AND subject=current_setting('app.auth_subject',true)
);
CREATE POLICY authenticated_membership ON iam.memberships FOR SELECT TO framework_app USING (
    user_id::text=current_setting('app.actor_id',true)
    OR tenant_id::text=current_setting('app.tenant_id',true)
);
CREATE POLICY authenticated_tenant ON iam.tenants FOR SELECT TO framework_app USING (
    active AND EXISTS (SELECT 1 FROM iam.memberships m WHERE m.tenant_id=id
      AND m.user_id::text=current_setting('app.actor_id',true) AND m.active)
);
CREATE POLICY authenticated_audit ON audit.entries FOR INSERT TO framework_app WITH CHECK (
    (actor_id::text=current_setting('app.actor_id',true)
      AND (tenant_id IS NULL OR tenant_id::text=current_setting('app.tenant_id',true)))
    OR (actor_id IS NULL AND tenant_id IS NULL AND action='authentication.denied' AND NOT allowed)
);

-- Also protect evidence against accidental mutation by table-owner operations.
-- Operators can still perform reviewed DDL; this is not immutable external storage.
-- +goose StatementBegin
CREATE FUNCTION audit.reject_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit evidence is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON audit.entries
    FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();
REVOKE ALL ON FUNCTION audit.reject_mutation() FROM PUBLIC;

-- +goose Down
-- Destructive reversal is intentionally not supported by the migration CLI.
DROP TABLE audit.entries;
DROP FUNCTION audit.reject_mutation();
DROP TABLE iam.memberships;
DROP TABLE iam.roles;
DROP TABLE iam.tenants;
DROP TABLE iam.users;
