-- Independent R2b review: protect immutable evidence from accidental owner
-- TRUNCATE as well as row UPDATE/DELETE. Preserve applied migration28 verbatim.
-- +goose Up
CREATE TRIGGER immutable_dataset_truncate BEFORE TRUNCATE ON runtime.datasets
 FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER immutable_binding_truncate BEFORE TRUNCATE ON runtime.dataset_bindings
 FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain dataset identity and immutable IR; reviewed forward recovery only';END $$;
-- +goose StatementEnd
