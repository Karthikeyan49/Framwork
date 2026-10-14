-- Preserve migration31; compatible checkpoint locking avoids an index FK/record
-- deadlock during backfill. Ordinary record writers retain their source lock.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.build_numeric_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.numeric_indexes%ROWTYPE;rec runtime.records%ROWTYPE;n numeric;last_id uuid;scanned bigint:=0;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT x.* INTO idx FROM runtime.numeric_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema THEN RAISE EXCEPTION 'index witness absent' USING ERRCODE='55000';END IF;
 -- A retried checkpoint after a lost response observes progress, never repeats
 -- an unbounded implicit build or silently skips a requested checkpoint.
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 FOR rec IN SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
 AND (idx.after_id IS NULL OR r.id>idx.after_id) ORDER BY r.id LIMIT 1000 FOR SHARE LOOP
  n:=runtime.numeric_value(rec.custom_data->f,idx.field_type,idx.currency,idx.required);
  INSERT INTO runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_number,source_version)
  VALUES(t,a,v,h,e,f,rec.id,n,rec.row_version)
  ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET value_number=excluded.value_number,source_version=excluded.source_version
  WHERE numeric_projections.source_version<=excluded.source_version;
  last_id:=rec.id;scanned:=scanned+1;
 END LOOP;
 IF scanned<1000 THEN
  -- Trigger maintenance protects already-scanned rows and inserts behind the
  -- cursor. Final coverage/value/version verification fails closed. The normal
  -- statement budget bounds this scan; a timeout never makes an index READY.
  IF EXISTS(SELECT 1 FROM runtime.records r LEFT JOIN runtime.numeric_projections p
   ON p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.id=r.id
   WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
   AND (p.id IS NULL OR p.source_version<>r.row_version OR p.value_number IS DISTINCT FROM runtime.numeric_value(r.custom_data->f,idx.field_type,idx.currency,idx.required)))
  THEN RAISE EXCEPTION 'index coverage mismatch' USING ERRCODE='55000';END IF;
 END IF;
 UPDATE runtime.numeric_indexes x SET after_id=coalesce(last_id,idx.after_id),scanned_rows=x.scanned_rows+scanned,checkpoint=x.checkpoint+1,state=CASE WHEN scanned<1000 THEN 'READY' ELSE 'BUILDING' END
 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'preserve projection consistency; reviewed forward recovery only'; END $$;
-- +goose StatementEnd
