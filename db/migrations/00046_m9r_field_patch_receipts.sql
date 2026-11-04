-- R5d: retain every receipt; add a typed partial-write operation.
-- +goose Up
ALTER TABLE runtime.command_receipts DROP CONSTRAINT command_receipts_operation_check;
ALTER TABLE runtime.command_receipts ADD CONSTRAINT command_receipts_operation_check CHECK(operation IN('create','update','delete','patch'));
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain patch receipts; reviewed forward recovery only'; END $$;
-- +goose StatementEnd
