-- Preserve applied 80/81. The retention trigger referenced invocation columns
-- while firing on the attempts table; PL/pgSQL resolves record fields per
-- statement, so each table gets its own branch. Owner and trigger set unchanged.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.retain_tool_evidence() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF TG_OP<>'UPDATE' THEN RAISE EXCEPTION 'tool evidence is retained' USING ERRCODE='55000';END IF;
 IF TG_TABLE_NAME='tool_attempts' THEN
  IF OLD.finished_at IS NOT NULL OR ROW(NEW.tenant_id,NEW.instance_id,NEW.step_key,NEW.attempt,NEW.kind,NEW.mode,NEW.lease_generation,NEW.started_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.instance_id,OLD.step_key,OLD.attempt,OLD.kind,OLD.mode,OLD.lease_generation,OLD.started_at) THEN
   RAISE EXCEPTION 'finished tool attempts are immutable' USING ERRCODE='55000';END IF;
 ELSIF TG_TABLE_NAME='tool_invocations' THEN
  IF ROW(NEW.tenant_id,NEW.instance_id,NEW.step_key,NEW.tool_ref,NEW.tool_version,NEW.tool_hash,NEW.idempotency_key,NEW.request_hash,NEW.mode,NEW.first_sent_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.instance_id,OLD.step_key,OLD.tool_ref,OLD.tool_version,OLD.tool_hash,OLD.idempotency_key,OLD.request_hash,OLD.mode,OLD.first_sent_at)
   OR NEW.attempts<OLD.attempts OR OLD.state IN('SUCCEEDED','APPLIED') THEN
   RAISE EXCEPTION 'tool invocation identity is immutable' USING ERRCODE='55000';END IF;
 ELSE RAISE EXCEPTION 'unexpected tool evidence table' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain tool ledger evidence; forward recovery only';END $$;
-- +goose StatementEnd
