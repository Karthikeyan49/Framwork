-- Live capability enforcement for exact leased resources. Retain all artifacts.
-- Quiesce workers during an operational cutover; use the compatible Go binary.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION control.policy_compare(f jsonb,x jsonb,y jsonb,op text) RETURNS boolean
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
  IF typ IN('reference','uuid') AND ys !~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN RAISE EXCEPTION 'invalid reference';END IF;
  IF typ='uuid' THEN cmp:=CASE WHEN xs::uuid<ys::uuid THEN -1 WHEN xs::uuid>ys::uuid THEN 1 ELSE 0 END;
  ELSE cmp:=CASE WHEN xs<ys THEN -1 WHEN xs>ys THEN 1 ELSE 0 END;END IF;
 ELSE RAISE EXCEPTION 'unknown field type';
 END CASE;
 RETURN CASE op WHEN 'eq' THEN cmp=0 WHEN 'neq' THEN cmp<>0 WHEN 'gt' THEN cmp>0 WHEN 'gte' THEN cmp>=0 WHEN 'lt' THEN cmp<0 WHEN 'lte' THEN cmp<=0 WHEN 'contains' THEN strpos(xs,ys)>0 ELSE false END;
END $$;
CREATE FUNCTION control.policy_scope_matches(s jsonb,r jsonb,actor uuid,teams uuid[],shared boolean,entity jsonb,depth integer) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE kind text:=s->>'kind';child jsonb;result boolean;v boolean;c jsonb;f jsonb;x jsonb;item jsonb;matched boolean:=false;
BEGIN
 IF s IS NULL OR jsonb_typeof(s)<>'object' OR depth>6 OR (depth=0 AND jsonb_array_length(jsonb_path_query_array(s,'$.**.kind'))>64) OR s ? 'teams' OR s ? 'write_share' THEN RAISE EXCEPTION 'invalid scope';END IF;
 CASE kind
 WHEN 'ALL' THEN RETURN true;
 WHEN 'NONE' THEN RETURN false;
 WHEN 'OWNER' THEN RETURN r->>'owner_user_id'=actor::text;
 WHEN 'TEAM' THEN RETURN coalesce((r->>'owner_team_id')::uuid=ANY(teams),false);
 WHEN 'SHARE' THEN RETURN shared;
 WHEN 'AND','OR' THEN
  IF jsonb_typeof(s->'children') IS DISTINCT FROM 'array' OR jsonb_array_length(s->'children') NOT BETWEEN 1 AND 8 THEN RAISE EXCEPTION 'invalid scope children';END IF;
  result:=kind='AND';
  FOR child IN SELECT value FROM jsonb_array_elements(s->'children') LOOP
   -- Do not short circuit validation of later children.
   v:=control.policy_scope_matches(child,r,actor,teams,shared,entity,depth+1);
   IF kind='AND' THEN result:=result AND v;ELSE result:=result OR v;END IF;
  END LOOP;RETURN coalesce(result,false);
 WHEN 'CONDITION' THEN
  -- Pinned execution bytes remain immutable. A predicate over retired business
  -- data cannot establish current access after schema cutover without a verified
  -- field/type mapping. Suspend that job rather than infer compatibility.
  IF r->>'_policy_current_generation'='false' THEN RAISE EXCEPTION 'conditional access requires current compatible data generation';END IF;
  c:=s->'condition';
  SELECT value INTO f FROM jsonb_array_elements(entity->'fields') WHERE value->>'id'=c->>'field';
  IF f IS NULL THEN RAISE EXCEPTION 'condition field missing';END IF;
  x:=CASE WHEN f->>'core_id'='true' THEN r->'id' ELSE r->'custom_data'->(c->>'field') END;
  IF c->>'op' IN('in','not_in') THEN
   IF jsonb_typeof(c->'value') IS DISTINCT FROM 'array' OR jsonb_array_length(c->'value') NOT BETWEEN 1 AND 32 THEN RAISE EXCEPTION 'invalid condition list';END IF;
   FOR item IN SELECT value FROM jsonb_array_elements(c->'value') LOOP
    -- Validate constants even for absent actual values.
    PERFORM control.policy_compare(f,item,item,'eq');
    matched:=matched OR control.policy_compare(f,x,item,'eq');
   END LOOP;
   IF x IS NULL OR x='null'::jsonb THEN RETURN false;END IF;
   RETURN CASE c->>'op' WHEN 'in' THEN matched ELSE NOT matched END;
  END IF;
  IF c->>'op'='is_null' THEN IF c ? 'value' THEN RAISE EXCEPTION 'invalid null condition';END IF;
  ELSE PERFORM control.policy_compare(f,c->'value',c->'value','eq');END IF;
  RETURN control.policy_compare(f,x,c->'value',c->>'op');
 ELSE RAISE EXCEPTION 'unknown scope';
 END CASE;
END $$;
CREATE FUNCTION control.policy_grant(doc jsonb,action text,app text,entity text,field text) RETURNS integer
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE g jsonb;allow boolean:=false;deny boolean:=false;
BEGIN
 IF jsonb_typeof(doc->'grants') IS DISTINCT FROM 'array' OR jsonb_array_length(doc->'grants')>64 THEN RETURN 0;END IF;
 FOR g IN SELECT value FROM jsonb_array_elements(doc->'grants') LOOP
  IF NOT coalesce(g->'actions' ? action,false) OR NOT ((g->'resource'->>'app_id' IN('*',app)) AND (g->'resource'->>'entity_id' IN('*',entity)) AND (g->'resource'->>'field_id' IN('*',field))) THEN CONTINUE;END IF;
  IF g->>'effect'='DENY' THEN deny:=true;ELSIF g->>'effect'='ALLOW' THEN allow:=true;ELSE RETURN 0;END IF;
 END LOOP;
 RETURN CASE WHEN deny THEN -1 WHEN allow THEN 1 ELSE 0 END;
END $$;
CREATE FUNCTION control.policy_data_matches(doc jsonb,action text,app text,entity_id text,field text,r jsonb,actor uuid,teams uuid[],shared boolean,entity jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE g jsonb;matched boolean:=false;v boolean;scopes integer:=0;
BEGIN
 IF control.policy_grant(doc,action,app,entity_id,field)<>1 THEN RETURN false;END IF;
 FOR g IN SELECT value FROM jsonb_array_elements(doc->'grants') LOOP
  IF g->>'effect'<>'ALLOW' OR NOT coalesce(g->'actions' ? action,false) OR NOT (g->'resource'->>'app_id' IN('*',app) AND g->'resource'->>'entity_id' IN('*',entity_id) AND g->'resource'->>'field_id' IN('*',field)) THEN CONTINUE;END IF;
  scopes:=scopes+1;IF scopes>8 THEN RAISE EXCEPTION 'scope budget';END IF;
  v:=control.policy_scope_matches(g->'scope',r,actor,teams,shared,entity,0);matched:=matched OR v;
 END LOOP;
 RETURN coalesce(matched,false);
END $$;
-- Lease authority is deliberately distinct from business permission. This
-- no-argument port returns only bounded provenance, never IR/input/record bytes.
CREATE FUNCTION control.worker_policy_decision() RETURNS TABLE(allowed boolean,action text,resource text,policy_version text,policy_revision bigint,app_id text,metadata_revision bigint,ir_hash text,workflow_id text,workflow_version text,plan_hash text,entity_id text,field_id text,record_id uuid,request_id text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_variable
DECLARE a record;role record;b runtime.workflow_bindings%ROWTYPE;o runtime.outbox_intents%ROWTYPE;rec runtime.records%ROWTYPE;d record;c runtime.record_identity%ROWTYPE;doc jsonb;entity jsonb;fields jsonb;fid text;teams uuid[];invalid boolean;shared boolean:=false;record_json jsonb;
BEGIN
 SELECT x.* INTO a FROM control.worker_authority() x;IF NOT FOUND THEN RETURN;END IF;
 SELECT h.active,h.version,v.bootstrap,v.body,v.body_hash,m.role INTO role FROM iam.memberships m JOIN control.role_heads h ON h.tenant_id=m.tenant_id AND h.name=m.role JOIN control.role_versions v ON v.tenant_id=h.tenant_id AND v.name=h.name AND v.version=h.version WHERE m.tenant_id=a.tenant_id AND m.user_id=a.actor_id AND m.active;
 allowed:=false;action:='policy.unavailable';resource:='workflow';field_id:='';
 IF FOUND THEN policy_revision:=role.version;policy_version:=CASE WHEN role.bootstrap THEN 'tenant-role-b4:' ELSE 'tenant-role-v1:' END||role.body_hash;ELSE policy_revision:=0;policy_version:='sql-policy-unavailable-v1';END IF;
 IF a.kind='event' THEN
  SELECT x.* INTO o FROM runtime.outbox_intents x WHERE x.tenant_id=a.tenant_id AND x.id=a.job_id;IF NOT FOUND THEN RETURN;END IF;
  app_id:=o.app_id;metadata_revision:=o.metadata_revision;ir_hash:=o.ir_hash;entity_id:=o.entity_id;record_id:=o.record_id;request_id:=o.request_id;resource:='event';workflow_id:='';workflow_version:='';plan_hash:='';
 ELSE
  SELECT x.* INTO b FROM runtime.workflow_bindings x WHERE x.tenant_id=a.tenant_id AND x.id=a.job_id;IF NOT FOUND THEN RETURN;END IF;
  app_id:=b.app_id;metadata_revision:=b.metadata_revision;ir_hash:=b.ir_hash;workflow_id:=b.workflow_id;workflow_version:=b.workflow_version;plan_hash:=b.plan_hash;record_id:=b.record_id;request_id:=b.request_id;entity_id:=convert_from(b.plan_body,'UTF8')::jsonb->>'entity_id';
 END IF;
 IF role.version IS NULL OR NOT role.active THEN RETURN NEXT;RETURN;END IF;
 IF role.bootstrap THEN
  allowed:=role.role IN('owner','admin','member');action:=CASE a.kind WHEN 'event' THEN 'events.consume' ELSE 'workflows.execute' END;
  IF a.kind='workflow' AND NOT EXISTS(SELECT 1 FROM runtime.worker_lease_source_record()) THEN allowed:=false;action:='workflows.data_scope';END IF;
  RETURN NEXT;RETURN;
 END IF;
 doc:=convert_from(role.body,'UTF8')::jsonb;
 IF doc->>'schema_version' IS DISTINCT FROM 'tenant-capabilities-v1' OR doc->>'name' IS DISTINCT FROM role.role OR octet_length(role.body)>65536 THEN RETURN NEXT;RETURN;END IF;
 action:=CASE a.kind WHEN 'event' THEN 'events.consume' ELSE 'workflows.execute' END;
 IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 IF a.kind='event' THEN allowed:=true;RETURN NEXT;RETURN;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE s->>'type'='activity') THEN
  action:='workflows.activity';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 END IF;
 resource:='entity';action:='data.entity.read';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 resource:='row';action:='data.row.read';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 fields:=convert_from(b.inputs,'UTF8')::jsonb->'fields';
 IF jsonb_typeof(fields) IS DISTINCT FROM 'array' OR jsonb_array_length(fields)>128 THEN RAISE EXCEPTION 'input field budget';END IF;
 FOR fid IN SELECT jsonb_array_elements_text(fields) LOOP
  action:='data.field.read';resource:='field';field_id:=fid;
  IF control.policy_grant(doc,action,app_id,entity_id,fid)<>1 THEN RETURN NEXT;RETURN;END IF;
 END LOOP;field_id:='';
 action:='workflows.data_scope';resource:='entity';
 SELECT x.* INTO rec FROM runtime.records x WHERE x.tenant_id=b.tenant_id AND x.app_id=b.app_id AND x.metadata_revision=coalesce(b.data_revision,b.metadata_revision) AND x.ir_hash=coalesce(b.data_hash,b.ir_hash) AND x.entity_id=entity_id AND x.id=b.record_id;
 IF NOT FOUND THEN RETURN NEXT;RETURN;END IF;
 SELECT x.id,x.canonical_access,g.generation=x.schema_generation AS current_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id WHERE g.tenant_id=rec.tenant_id AND g.app_id=rec.app_id AND g.anchor_revision=rec.metadata_revision AND g.anchor_hash=rec.ir_hash;
 IF FOUND AND d.canonical_access THEN
  SELECT x.* INTO c FROM runtime.record_identity x WHERE x.tenant_id=rec.tenant_id AND x.app_id=rec.app_id AND x.dataset_id=d.id AND x.entity_id=rec.entity_id AND x.id=rec.id AND NOT x.tombstone;
  IF NOT FOUND THEN RETURN NEXT;RETURN;END IF;rec.owner_user_id:=c.owner_user_id;rec.owner_team_id:=c.owner_team_id;
  SELECT EXISTS(SELECT 1 FROM runtime.identity_shares s WHERE s.tenant_id=c.tenant_id AND s.app_id=c.app_id AND s.dataset_id=c.dataset_id AND s.entity_id=c.entity_id AND s.id=c.id AND s.user_id=a.actor_id) INTO shared;
 ELSE SELECT EXISTS(SELECT 1 FROM runtime.record_shares s WHERE s.tenant_id=rec.tenant_id AND s.app_id=rec.app_id AND s.metadata_revision=rec.metadata_revision AND s.ir_hash=rec.ir_hash AND s.entity_id=rec.entity_id AND s.id=rec.id AND s.user_id=a.actor_id) INTO shared;END IF;
 record_json:=to_jsonb(rec)||jsonb_build_object('_policy_current_generation',coalesce(d.current_generation,true));
 WITH RECURSIVE permitted(id,path,depth,cycle) AS (
 SELECT team_id,ARRAY[team_id],0,false FROM iam.team_memberships WHERE tenant_id=a.tenant_id AND user_id=a.actor_id AND active
 UNION ALL SELECT t.id,p.path||t.id,p.depth+1,t.id=ANY(p.path) FROM iam.teams t JOIN permitted p ON t.parent_id=p.id WHERE t.tenant_id=a.tenant_id AND p.depth<9 AND NOT p.cycle
 ),bounded AS (SELECT DISTINCT id,bool_or(depth>8 OR cycle) OVER() bad FROM permitted ORDER BY id LIMIT 65)
 SELECT array_agg(id),coalesce(bool_or(bad),false) INTO teams,invalid FROM bounded;
 IF invalid OR cardinality(teams)>64 THEN RETURN NEXT;RETURN;END IF;
 SELECT x INTO entity FROM jsonb_array_elements(convert_from(b.ir_body,'UTF8')::jsonb->'entities') x WHERE x->>'id'=entity_id;
 IF entity IS NULL OR NOT control.policy_data_matches(doc,'data.entity.read',app_id,entity_id,'',record_json,a.actor_id,teams,shared,entity) THEN RETURN NEXT;RETURN;END IF;
 FOR fid IN SELECT jsonb_array_elements_text(fields) LOOP
  resource:='field';field_id:=fid;
  IF NOT control.policy_data_matches(doc,'data.field.read',app_id,entity_id,fid,record_json,a.actor_id,teams,shared,entity) THEN RETURN NEXT;RETURN;END IF;
 END LOOP;
 allowed:=true;field_id:='';action:='workflows.execute';resource:='workflow';RETURN NEXT;
EXCEPTION WHEN invalid_text_representation OR data_exception OR raise_exception THEN
 allowed:=false;action:='policy.unavailable';RETURN NEXT;
END $$;
CREATE FUNCTION iam.worker_identity_scope(candidate uuid) RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM control.worker_authority() a WHERE a.tenant_id=candidate AND a.actor_id=iam.authenticated_actor())
$$;
CREATE OR REPLACE FUNCTION iam.worker_tenant_scope(candidate uuid) RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT iam.worker_identity_scope(candidate) AND coalesce((SELECT allowed FROM control.worker_policy_decision()),false)
$$;
-- Source helper is now private. Wrapper denies before returning any source bytes.
ALTER FUNCTION runtime.worker_source_record() RENAME TO worker_lease_source_record;
-- Defense against an accidental future EXECUTE grant: the internal helper runs
-- only with its caller's privileges, and requires the private owner's context.
-- The public wrapper/evaluator are SECURITY DEFINER; raw workers are not owners.
ALTER FUNCTION runtime.worker_lease_source_record() SECURITY INVOKER;
REVOKE ALL ON FUNCTION runtime.worker_lease_source_record() FROM PUBLIC,framework_worker,framework_app,framework_dispatcher;
CREATE FUNCTION runtime.worker_source_record() RETURNS SETOF runtime.records LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT r.* FROM runtime.worker_lease_source_record() r WHERE coalesce((SELECT allowed FROM control.worker_policy_decision()),false)
$$;
CREATE OR REPLACE FUNCTION runtime.worker_source_shares() RETURNS SETOF runtime.record_shares LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT s.* FROM runtime.read_shares s JOIN runtime.worker_source_record() r ON r.tenant_id=s.tenant_id AND r.app_id=s.app_id AND r.metadata_revision=s.metadata_revision AND r.ir_hash=s.ir_hash AND r.entity_id=s.entity_id AND r.id=s.id
 JOIN control.worker_authority() a ON a.tenant_id=s.tenant_id AND a.actor_id=s.user_id WHERE a.kind='workflow'
$$;
-- Existing business RLS and security-definer ports use worker_tenant_scope.
-- Keep denial evidence possible using only exact lease identity.
ALTER POLICY worker_membership_boundary ON iam.memberships USING(iam.worker_identity_scope(tenant_id) AND user_id=iam.authenticated_actor());
ALTER POLICY worker_tenant_boundary ON iam.tenants USING(iam.worker_identity_scope(id));
ALTER POLICY worker_audit ON audit.entries WITH CHECK(actor_id=iam.authenticated_actor() AND iam.worker_identity_scope(tenant_id) AND (NOT allowed OR iam.worker_tenant_scope(tenant_id)));
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE (n.nspname='control' AND p.proname IN('policy_compare','policy_scope_matches','policy_grant','policy_data_matches','worker_policy_decision')) OR (n.nspname='iam' AND p.proname='worker_identity_scope') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
ALTER FUNCTION runtime.worker_source_record() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.worker_source_record() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.worker_source_record(),control.worker_policy_decision(),iam.worker_identity_scope(uuid) TO framework_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain artifacts and live authorization; compatible forward fix only';END $$;
-- +goose StatementEnd
