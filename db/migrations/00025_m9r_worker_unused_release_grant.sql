-- Workers execute pinned workflow bindings; no generic release registry port is
-- required by the current adapter. Preserve release artifacts and role ownership.
-- +goose Up
REVOKE ALL ON control.release_bundles FROM framework_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'do not restore unused worker payload grants';END $$;
-- +goose StatementEnd
