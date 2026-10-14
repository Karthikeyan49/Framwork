-- Preserve migrations31/32. Both copying and coverage checks are checkpointed
-- in <=1000 source rows. Normal write triggers preserve verified prefixes.
-- +goose Up
ALTER TABLE runtime.numeric_indexes DROP CONSTRAINT numeric_indexes_state_check;
ALTER TABLE runtime.numeric_indexes ADD CONSTRAINT numeric_indexes_state_check CHECK(state IN('BUILDING','VERIFYING','READY'));
ALTER TABLE runtime.numeric_indexes ADD verification_after uuid;
ALTER TABLE runtime.numeric_indexes ADD verified_rows bigint NOT NULL DEFAULT 0 CHECK(verified_rows>=0);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.numeric_value(raw jsonb,typ text,cur text,req boolean) RETURNS numeric
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
DECLARE s text;n numeric;
BEGIN
 IF raw IS NULL OR raw='null'::jsonb THEN
  IF req THEN RAISE EXCEPTION 'required indexed value missing' USING ERRCODE='22023';END IF;
  RETURN NULL;
 END IF;
 IF typ='integer' THEN
  IF jsonb_typeof(raw)<>'number' OR raw::text !~ '^-?(0|[1-9][0-9]*)$' THEN RAISE EXCEPTION 'invalid indexed integer' USING ERRCODE='22023';END IF;
  n:=raw::text::numeric;
  IF n<>trunc(n) OR abs(n)>9007199254740991 THEN RAISE EXCEPTION 'indexed integer budget' USING ERRCODE='22023';END IF;
  RETURN n;
 ELSIF typ='money' THEN
  IF jsonb_typeof(raw)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(raw))<>2
  OR raw->>'currency' IS DISTINCT FROM cur OR jsonb_typeof(raw->'amount') IS DISTINCT FROM 'string'
  THEN RAISE EXCEPTION 'invalid indexed money' USING ERRCODE='22023';END IF;
  s:=raw->>'amount';
 ELSIF typ='decimal' AND jsonb_typeof(raw)='string' THEN s:=raw#>>'{}';
 ELSE RAISE EXCEPTION 'invalid indexed type' USING ERRCODE='22023';END IF;
 IF length(s)>64 OR s !~ '^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$' THEN RAISE EXCEPTION 'indexed decimal budget' USING ERRCODE='22023';END IF;
 n:=s::numeric;
 IF abs(n)>1000000000000000000 THEN RAISE EXCEPTION 'indexed decimal range' USING ERRCODE='22023';END IF;
 RETURN n;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.build_numeric_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.numeric_indexes%ROWTYPE;rec runtime.records%ROWTYPE;n numeric;pv bigint;pn numeric;last_id uuid;scanned bigint:=0;after_cursor uuid;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT x.* INTO idx FROM runtime.numeric_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema THEN RAISE EXCEPTION 'index witness absent' USING ERRCODE='55000';END IF;
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 after_cursor:=CASE WHEN idx.state='BUILDING' THEN idx.after_id ELSE idx.verification_after END;
 FOR rec IN SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
 AND (after_cursor IS NULL OR r.id>after_cursor) ORDER BY r.id LIMIT 1000 FOR SHARE LOOP
  n:=runtime.numeric_value(rec.custom_data->f,idx.field_type,idx.currency,idx.required);
  IF idx.state='BUILDING' THEN
   INSERT INTO runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_number,source_version)
   VALUES(t,a,v,h,e,f,rec.id,n,rec.row_version)
   ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET value_number=excluded.value_number,source_version=excluded.source_version
   WHERE numeric_projections.source_version<=excluded.source_version;
  ELSE
   SELECT p.source_version,p.value_number INTO pv,pn FROM runtime.numeric_projections p
   WHERE p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.id=rec.id;
   IF NOT FOUND OR pv<>rec.row_version OR pn IS DISTINCT FROM n THEN RAISE EXCEPTION 'index coverage mismatch' USING ERRCODE='55000';END IF;
  END IF;
  last_id:=rec.id;scanned:=scanned+1;
 END LOOP;
 -- App credentials cannot mutate projections or skip checkpoints. After a
 -- verified row unlocks, its atomic trigger/FK preserves value/version/coverage
 -- on each later write/delete. Inserts behind the cursor use the same trigger.
 UPDATE runtime.numeric_indexes x SET
 after_id=CASE WHEN idx.state='BUILDING' THEN coalesce(last_id,idx.after_id) ELSE idx.after_id END,
 verification_after=CASE WHEN idx.state='VERIFYING' THEN coalesce(last_id,idx.verification_after) ELSE idx.verification_after END,
 scanned_rows=x.scanned_rows+CASE WHEN idx.state='BUILDING' THEN scanned ELSE 0 END,
 verified_rows=x.verified_rows+CASE WHEN idx.state='VERIFYING' THEN scanned ELSE 0 END,
 checkpoint=x.checkpoint+1,
 state=CASE WHEN scanned=1000 THEN idx.state WHEN idx.state='BUILDING' THEN 'VERIFYING' ELSE 'READY' END
 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain verification checkpoints; reviewed forward recovery only'; END $$;
-- +goose StatementEnd
