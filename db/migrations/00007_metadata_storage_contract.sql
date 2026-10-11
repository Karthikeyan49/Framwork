-- Purpose: reject missing/null registry envelope fields at the storage boundary.
-- Compatibility: SAFE additive validation; tables contain only validated M3 sources.
-- Lock risk: short constraint validation scan; no rewrite/backfill. Forward fix only.
-- +goose Up
ALTER TABLE control.metadata_versions ADD CONSTRAINT metadata_envelope_required
 CHECK (COALESCE(jsonb_typeof(document->'schema_version')='string'
   AND document->>'schema_version'=schema_version
   AND jsonb_typeof(document->'app_id')='string' AND document->>'app_id'=app_id
   AND jsonb_typeof(document->'namespace')='string'
   AND jsonb_typeof(document->'definitions')='array'
   AND jsonb_array_length(document->'definitions') BETWEEN 1 AND 500,false));
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'metadata storage requires a reviewed forward fix'; END $$;
-- +goose StatementEnd
