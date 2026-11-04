-- Preserve applied39. Close independently reproduced typed-ID mismatch and
-- repeated IR parsing. No records/artifacts rewritten and no grants broadened.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.policy_compare(f jsonb,x jsonb,y jsonb,op text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE typ text:=f->>'type';xs text;ys text;xn numeric;yn numeric;cmp integer;
BEGIN
 IF op='is_null' THEN RETURN x IS NULL OR x='null'::jsonb;END IF;
 IF x IS NULL OR x='null'::jsonb THEN RETURN false;END IF;
 IF y IS NULL OR y='null'::jsonb THEN RAISE EXCEPTION 'invalid condition value';END IF;
 IF f->>'core_id'='true' THEN typ:='uuid';END IF;
 IF op NOT IN('eq','neq','gt','gte','lt','lte','contains') THEN RAISE EXCEPTION 'unknown condition';END IF;
 IF op='contains' AND typ<>'string' THEN RAISE EXCEPTION 'invalid contains';END IF;
 IF typ='money' THEN
  IF jsonb_typeof(y)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(y))<>2 OR y->>'currency' IS DISTINCT FROM f->>'currency' OR x->>'currency' IS DISTINCT FROM f->>'currency' THEN RAISE EXCEPTION 'invalid money';END IF;
  x:=x->'amount';y:=y->'amount';typ:='decimal';
 END IF;
 xs:=x#>>'{}';ys:=y#>>'{}';
 CASE typ
 WHEN 'integer' THEN
  IF jsonb_typeof(x)<>'number' OR jsonb_typeof(y)<>'number' OR ys !~ '^-?[0-9]+$' THEN RAISE EXCEPTION 'invalid integer';END IF;
  xn:=xs::numeric;yn:=ys::numeric;
  IF xn<>trunc(xn) OR abs(xn)>9007199254740991 OR abs(yn)>9007199254740991 THEN RAISE EXCEPTION 'integer budget';END IF;
  cmp:=CASE WHEN xn<yn THEN -1 WHEN xn>yn THEN 1 ELSE 0 END;
 WHEN 'decimal' THEN
  IF jsonb_typeof(x)<>'string' OR jsonb_typeof(y)<>'string' OR length(xs)>64 OR length(ys)>64 OR xs !~ '^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$' OR ys !~ '^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$' THEN RAISE EXCEPTION 'invalid decimal';END IF;
  xn:=xs::numeric;yn:=ys::numeric;IF abs(xn)>1e18 OR abs(yn)>1e18 THEN RAISE EXCEPTION 'decimal budget';END IF;
  cmp:=CASE WHEN xn<yn THEN -1 WHEN xn>yn THEN 1 ELSE 0 END;
 WHEN 'bool' THEN
  IF jsonb_typeof(x)<>'boolean' OR jsonb_typeof(y)<>'boolean' OR op NOT IN('eq','neq') THEN RAISE EXCEPTION 'invalid boolean';END IF;
  cmp:=CASE WHEN xs=ys THEN 0 ELSE 1 END;
 WHEN 'date' THEN
  IF jsonb_typeof(x)<>'string' OR jsonb_typeof(y)<>'string' OR xs !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' OR ys !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' OR substring(xs,1,4)='0000' OR substring(ys,1,4)='0000' THEN RAISE EXCEPTION 'invalid date';END IF;
  cmp:=CASE WHEN xs::date<ys::date THEN -1 WHEN xs::date>ys::date THEN 1 ELSE 0 END;
 WHEN 'datetime' THEN
  IF jsonb_typeof(x)<>'string' OR jsonb_typeof(y)<>'string' OR length(xs)>40 OR length(ys)>40 OR xs !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$' OR ys !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$' THEN RAISE EXCEPTION 'invalid timestamp';END IF;
  IF extract(year FROM xs::timestamptz AT TIME ZONE 'UTC') NOT BETWEEN 1 AND 9999 OR extract(year FROM ys::timestamptz AT TIME ZONE 'UTC') NOT BETWEEN 1 AND 9999 THEN RAISE EXCEPTION 'timestamp range';END IF;
  cmp:=CASE WHEN xs::timestamptz<ys::timestamptz THEN -1 WHEN xs::timestamptz>ys::timestamptz THEN 1 ELSE 0 END;
 WHEN 'string','enum','reference','uuid' THEN
  IF jsonb_typeof(x)<>'string' OR jsonb_typeof(y)<>'string' OR octet_length(ys)>4096 OR (f->>'required'='true' AND ys='') THEN RAISE EXCEPTION 'invalid string';END IF;
  IF typ='enum' AND NOT coalesce((f->'enum_values') ? ys,false) THEN RAISE EXCEPTION 'invalid enum';END IF;
  IF typ IN('reference','uuid') AND ys !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN RAISE EXCEPTION 'invalid reference';END IF;
  IF typ='uuid' THEN cmp:=CASE WHEN xs::uuid<ys::uuid THEN -1 WHEN xs::uuid>ys::uuid THEN 1 ELSE 0 END;
  ELSE cmp:=CASE WHEN xs<ys THEN -1 WHEN xs>ys THEN 1 ELSE 0 END;END IF;
 ELSE RAISE EXCEPTION 'unknown field type';
 END CASE;
 RETURN CASE op WHEN 'eq' THEN cmp=0 WHEN 'neq' THEN cmp<>0 WHEN 'gt' THEN cmp>0 WHEN 'gte' THEN cmp>=0 WHEN 'lt' THEN cmp<0 WHEN 'lte' THEN cmp<=0 WHEN 'contains' THEN strpos(xs,ys)>0 ELSE false END;
END $$;

-- The expensive live definition/IR decision is a statement InitPlan. Keep the
-- cheap exact identity/lease clock fence on each row, including WITH CHECK.
DO $$ DECLARE tab text;keycol text;BEGIN
 FOREACH tab IN ARRAY ARRAY['workflow_bindings','workflow_instances','workflow_history','workflow_receipts','workflow_signals','workflow_outbox'] LOOP
 keycol:=CASE WHEN tab IN('workflow_bindings','workflow_instances') THEN 'id' ELSE 'instance_id' END;
 EXECUTE format('ALTER POLICY workflow_worker ON runtime.%I USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND %I::text=current_setting(''app.job_id'',true)) WITH CHECK(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND %I::text=current_setting(''app.job_id'',true))',tab,keycol,keycol);
 END LOOP;
END $$;
ALTER POLICY event_worker ON runtime.event_envelopes USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND id::text=current_setting('app.job_id',true));
ALTER POLICY inbox_worker ON runtime.event_inbox
 USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND event_id::text=current_setting('app.job_id',true))
 WITH CHECK(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND event_id::text=current_setting('app.job_id',true)
 AND body_hash=(SELECT e.body_hash FROM runtime.event_envelopes e WHERE e.tenant_id=event_inbox.tenant_id AND e.id=event_inbox.event_id));
ALTER POLICY worker_team_boundary ON iam.teams USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()));
ALTER POLICY worker_team_membership_boundary ON iam.team_memberships USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()));
ALTER POLICY worker_audit ON audit.entries WITH CHECK(actor_id=iam.authenticated_actor() AND iam.worker_identity_scope(tenant_id) AND (NOT allowed OR (SELECT coalesce(allowed,false) FROM control.worker_policy_decision())));
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain live authorization; compatible forward fix only';END $$;
-- +goose StatementEnd
