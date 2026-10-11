-- Purpose: establish logical schemas; no tenant tables in M0.
-- Compatibility: additive / SAFE. No data backfill or row locks.
-- Validation: four schemas exist and are owned by the migration role.
-- Recovery: forward fix; Down is only for empty disposable development databases.
-- +goose Up
CREATE SCHEMA iam;
CREATE SCHEMA control;
CREATE SCHEMA runtime;
CREATE SCHEMA audit;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- +goose Down
DROP SCHEMA audit;
DROP SCHEMA runtime;
DROP SCHEMA control;
DROP SCHEMA iam;
