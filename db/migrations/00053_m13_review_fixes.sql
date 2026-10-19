-- M13 independent review fixes (docs/verification/M13_INDEPENDENT_REVIEW.md). Additive and forward-only.
--   M13R-01  an acknowledged rejection did not stop an activation whose REPEATABLE READ transaction began earlier:
--            reject_release and activate_release now both write one control.release_fences row per hash, so the
--            transaction with the older snapshot aborts with 40001 (a lock-only fence does not work under REPEATABLE READ).
--   M13R-02  the rollback class gate read only the current release: it now covers the current release and every
--            history entry newer than the target's latest entry (control.release_rollback_class).
--   M13R-05  rejection notes may not carry U+061C, U+200B, U+200E, U+200F or U+FEFF (port check and table constraint;
--            rows written before this migration are not re-validated). U+200C and U+200D stay legal (emoji, scripts).
-- Replaces: control.reject_release(text,text,text,text), control.activate_release(text,text,text,bigint) and
-- control.rollback_release(text,text,text,bigint,text,boolean), all from 00049 (no later migration replaced them).
-- Adds: control.release_fences, control.release_rollback_class(uuid,text,text).
-- +goose Up
CREATE TABLE control.release_fences(
 tenant_id uuid NOT NULL,app_id text NOT NULL,artifact_hash text NOT NULL CHECK(artifact_hash ~ '^[0-9a-f]{64}$'),
 writes bigint NOT NULL DEFAULT 1 CHECK(writes>=1),
 PRIMARY KEY(tenant_id,app_id,artifact_hash),
 FOREIGN KEY(tenant_id,app_id,artifact_hash) REFERENCES control.release_bundles(tenant_id,app_id,artifact_hash)
);
SELECT runtime.install_tenant_rls('control.release_fences');
REVOKE ALL ON control.release_fences FROM framework_app;
CREATE POLICY worker_no_generic ON control.release_fences AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER retain_release_fences BEFORE DELETE OR TRUNCATE ON control.release_fences FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
ALTER TABLE control.release_rejections DROP CONSTRAINT release_rejections_note_check;
ALTER TABLE control.release_rejections ADD CONSTRAINT release_rejections_note_check
 CHECK(char_length(note)<=280 AND note !~ '[\u0001-\u001f\u007f-\u009f\u061c\u200b\u200e\u200f\u2028\u2029\u202a-\u202e\u2066-\u2069\ufeff]') NOT VALID;
-- +goose StatementBegin
-- Risk.Rollback of a stored 2.0.0 body; NULL for a 1.0.0 body, a missing or non-string value or an unreadable body.
CREATE FUNCTION control.release_rollback_class(t uuid,p_app text,p_hash text) RETURNS text LANGUAGE plpgsql STABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE raw bytea;doc jsonb;
BEGIN
 SELECT b.body INTO raw FROM control.release_bundles b WHERE b.tenant_id=t AND b.app_id=p_app AND b.artifact_hash=p_hash;
 BEGIN doc:=convert_from(raw,'UTF8')::jsonb;EXCEPTION WHEN others THEN RETURN NULL;END;
 IF jsonb_typeof(doc)='object' AND jsonb_typeof(doc->'Version')='string' AND doc->>'Version'='2.0.0' AND jsonb_typeof(doc->'Risk')='object' AND jsonb_typeof(doc->'Risk'->'Rollback')='string' THEN
  RETURN doc->'Risk'->>'Rollback';END IF;
 RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION control.reject_release(p_app text,p_hash text,p_reason text,p_note text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();v_note text:=coalesce(p_note,'');n bigint;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release approver required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' OR p_reason IS NULL
  OR p_reason NOT IN('REQUIREMENTS_NOT_MET','RISK_UNACCEPTABLE','EVIDENCE_INSUFFICIENT','SUPERSEDED','WITHDRAWN','OTHER')
  OR char_length(v_note)>280 OR v_note ~ '[\u0001-\u001f\u007f-\u009f\u061c\u200b\u200e\u200f\u2028\u2029\u202a-\u202e\u2066-\u2069\ufeff]' THEN
  RAISE EXCEPTION 'invalid rejection request' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM control.release_candidates r WHERE r.tenant_id=t AND r.app_id=p_app AND r.artifact_hash=p_hash) THEN
  RAISE EXCEPTION 'release candidate not found' USING ERRCODE='P0002';END IF;
 -- A rejection and an activation of the same hash write this row, so whichever transaction started its snapshot
 -- earlier aborts with 40001 instead of overtaking the other (M13R-01).
 INSERT INTO control.release_fences AS f(tenant_id,app_id,artifact_hash) VALUES(t,p_app,p_hash) ON CONFLICT(tenant_id,app_id,artifact_hash) DO UPDATE SET writes=f.writes+1;
 INSERT INTO control.release_rejections(tenant_id,app_id,artifact_hash,actor_id,reason,note) VALUES(t,p_app,p_hash,actor,p_reason,v_note) ON CONFLICT DO NOTHING;
 GET DIAGNOSTICS n=ROW_COUNT;
 RETURN n=1;
END $$;
CREATE OR REPLACE FUNCTION control.activate_release(p_app text,p_hash text,p_expected_base text,p_expected_generation bigint) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();c record;cur record;has_cur boolean;need integer;raw bytea;doc jsonb;cls text;n bigint;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release activator required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' OR p_expected_base IS NULL OR (p_expected_base<>'' AND p_expected_base !~ '^[0-9a-f]{64}$')
  OR p_expected_generation IS NULL OR p_expected_generation<0 OR p_expected_generation>=9223372036854775807 THEN
  RAISE EXCEPTION 'invalid activation request' USING ERRCODE='22023';END IF;
 SELECT a.artifact_hash,a.generation INTO cur FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app FOR UPDATE;
 has_cur:=FOUND;
 SELECT r.risk_level,r.base_release,r.base_generation INTO c FROM control.release_candidates r WHERE r.tenant_id=t AND r.app_id=p_app AND r.artifact_hash=p_hash;
 IF NOT FOUND THEN RAISE EXCEPTION 'release candidate not found' USING ERRCODE='P0002';END IF;
 -- Written before the rejection is read: a rejection that committed after this transaction's snapshot makes this write
 -- fail with 40001, so a veto acknowledged to its author is never overtaken by an activation that started earlier (M13R-01).
 INSERT INTO control.release_fences AS f(tenant_id,app_id,artifact_hash) VALUES(t,p_app,p_hash) ON CONFLICT(tenant_id,app_id,artifact_hash) DO UPDATE SET writes=f.writes+1;
 IF EXISTS(SELECT 1 FROM control.release_rejections x WHERE x.tenant_id=t AND x.app_id=p_app AND x.artifact_hash=p_hash) THEN
  RAISE EXCEPTION 'release candidate was rejected' USING ERRCODE='55000';END IF;
 IF p_expected_base<>c.base_release OR p_expected_generation<>c.base_generation THEN
  RAISE EXCEPTION 'expected base is not the candidate base' USING ERRCODE='55000';END IF;
 IF (has_cur AND (cur.artifact_hash<>p_expected_base OR cur.generation<>p_expected_generation)) OR (NOT has_cur AND (p_expected_base<>'' OR p_expected_generation<>0)) THEN
  RAISE EXCEPTION 'candidate is stale' USING ERRCODE='55000';END IF;
 -- Hold the approvers' memberships until commit, so a concurrent demotion
 -- either waits or fails this transaction; then count under that lock.
 PERFORM 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id IN(SELECT a.actor_id FROM control.release_approvals a WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_hash) FOR SHARE OF m;
 need:=CASE WHEN c.risk_level='CRITICAL' THEN 2 ELSE 1 END;
 IF control.release_approval_count(t,p_app,p_hash)<need THEN
  RAISE EXCEPTION 'approval quorum not met' USING ERRCODE='55000';END IF;
 SELECT b.body INTO raw FROM control.release_bundles b WHERE b.tenant_id=t AND b.app_id=p_app AND b.artifact_hash=p_hash;
 BEGIN doc:=convert_from(raw,'UTF8')::jsonb;EXCEPTION WHEN others THEN doc:=NULL;END;
 IF jsonb_typeof(doc)='object' AND jsonb_typeof(doc->'Storage')='object' AND jsonb_typeof(doc->'Storage'->'class')='string' THEN cls:=doc->'Storage'->>'class';END IF;
 IF cls IS NULL OR cls NOT IN('EQUAL','ADDITIVE','TRANSFORM','BREAKING') THEN
  RAISE EXCEPTION 'candidate storage class is unreadable' USING ERRCODE='55000';END IF;
 IF has_cur THEN
  UPDATE control.active_releases a SET artifact_hash=p_hash,generation=a.generation+1 WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_expected_base AND a.generation=p_expected_generation;
 ELSE
  INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES(t,p_app,p_hash,1) ON CONFLICT DO NOTHING;
 END IF;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'the active release changed concurrently' USING ERRCODE='55000';END IF;
 INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,actor_id,risk_level,storage_class)
  SELECT t,p_app,coalesce(max(h.seq),0)+1,'ACTIVATE',p_hash,p_expected_base,p_expected_generation+1,actor,c.risk_level,cls FROM control.release_history h WHERE h.tenant_id=t AND h.app_id=p_app;
 RETURN p_expected_generation+1;
END $$;
CREATE OR REPLACE FUNCTION control.rollback_release(p_app text,p_target text,p_expected_current text,p_expected_generation bigint,p_plan_hash text,p_plan_ack boolean) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();plan text:=nullif(p_plan_hash,'');cur record;tgt record;n bigint;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release operator required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_target IS NULL OR p_target !~ '^[0-9a-f]{64}$' OR p_expected_current IS NULL OR p_expected_current !~ '^[0-9a-f]{64}$'
  OR p_expected_generation IS NULL OR p_expected_generation<1 OR p_expected_generation>=9223372036854775807 OR (plan IS NOT NULL AND plan !~ '^[0-9a-f]{64}$') THEN
  RAISE EXCEPTION 'invalid rollback request' USING ERRCODE='22023';END IF;
 SELECT a.artifact_hash,a.generation INTO cur FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app FOR UPDATE;
 IF NOT FOUND OR cur.artifact_hash<>p_expected_current OR cur.generation<>p_expected_generation THEN
  RAISE EXCEPTION 'the active release changed' USING ERRCODE='55000';END IF;
 IF p_target=cur.artifact_hash THEN RAISE EXCEPTION 'the rollback target is the active release' USING ERRCODE='55000';END IF;
 SELECT h.seq,h.risk_level,h.storage_class INTO tgt FROM control.release_history h WHERE h.tenant_id=t AND h.app_id=p_app AND h.artifact_hash=p_target ORDER BY h.seq DESC LIMIT 1;
 IF NOT FOUND THEN RAISE EXCEPTION 'the rollback target was never activated' USING ERRCODE='55000';END IF;
 -- A rejection is a permanent veto bound to the hash: a release that was rejected
 -- after it ran is no longer a valid place to return to.
 IF EXISTS(SELECT 1 FROM control.release_rejections x WHERE x.tenant_id=t AND x.app_id=p_app AND x.artifact_hash=p_target) THEN
  RAISE EXCEPTION 'the rollback target was rejected' USING ERRCODE='55000';END IF;
 -- The gate covers every release the pointer moves across: the current one and each history entry newer than the
 -- target's latest entry. Anything but exactly POINTER_SAFE among them needs an acknowledged plan (M13R-02).
 IF EXISTS(SELECT 1 FROM (SELECT cur.artifact_hash AS h UNION SELECT x.artifact_hash FROM control.release_history x WHERE x.tenant_id=t AND x.app_id=p_app AND x.seq>tgt.seq) s
   WHERE control.release_rollback_class(t,p_app,s.h) IS DISTINCT FROM 'POINTER_SAFE') AND (p_plan_ack IS NOT TRUE OR plan IS NULL) THEN
  RAISE EXCEPTION 'rolling back this release requires an acknowledged plan' USING ERRCODE='55000';END IF;
 UPDATE control.active_releases a SET artifact_hash=p_target,generation=a.generation+1 WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_expected_current AND a.generation=p_expected_generation;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'the active release changed concurrently' USING ERRCODE='55000';END IF;
 INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,actor_id,risk_level,storage_class,plan_hash)
  SELECT t,p_app,coalesce(max(h.seq),0)+1,'ROLLBACK',p_target,p_expected_current,p_expected_generation+1,actor,tgt.risk_level,tgt.storage_class,plan FROM control.release_history h WHERE h.tenant_id=t AND h.app_id=p_app;
 RETURN p_expected_generation+1;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control'
  AND p.proname IN('release_rollback_class','reject_release','activate_release','rollback_release') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.reject_release(text,text,text,text),control.activate_release(text,text,text,bigint),control.rollback_release(text,text,text,bigint,text,boolean) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain release approvals, rejections and history; forward recovery only';END $$;
-- +goose StatementEnd
