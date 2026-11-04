-- Table ACL revocation does not revoke previously granted column ACLs. Seal
-- legacy writes at both layers; V3 is the sole runtime write adapter.
-- +goose Up
-- +goose StatementBegin
DO $$ DECLARE t text;a record;BEGIN
 FOREACH t IN ARRAY ARRAY['preview_records','preview_datasets'] LOOP
  FOR a IN SELECT attname FROM pg_attribute WHERE attrelid=('runtime.'||t)::regclass AND attnum>0 AND NOT attisdropped LOOP
   EXECUTE format('REVOKE INSERT(%I),UPDATE(%I),REFERENCES(%I) ON runtime.%I FROM framework_app,framework_worker',a.attname,a.attname,a.attname,t);
  END LOOP;
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE runtime.records ADD FOREIGN KEY(tenant_id,owner_team_id) REFERENCES iam.teams(tenant_id,id);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'never restore legacy writer privileges';END $$;
-- +goose StatementEnd
