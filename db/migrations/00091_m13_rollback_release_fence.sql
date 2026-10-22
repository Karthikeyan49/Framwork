-- Independent review fix IR-06 / M14R-04 (docs/verification/INTEGRATION_INDEPENDENT_REVIEW.md, M14_INDEPENDENT_REVIEW.md). Additive and forward-only.
-- M13R-01 made reject_release and activate_release write one control.release_fences row per hash, so that a transaction whose
-- REPEATABLE READ snapshot is older than a committed rejection aborts with 40001 instead of acting on a hash that was vetoed in the
-- meantime. rollback_release is the third writer of the active pointer and was left out: a rollback that began before a committed
-- rejection of its target moved the pointer onto the rejected release. It now writes the same fence before it reads the rejections.
-- Replaces control.rollback_release(text,text,text,bigint,text,boolean) from its latest definition (00088; the function is defined in 00084 and 00088), byte for byte except for
-- that statement. Owner, ACL and every guard are unchanged; the migration fails if the owner or the ACL moved.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
 IF (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname='rollback_release')<>1 THEN
  RAISE EXCEPTION 'control.rollback_release must exist exactly once before it is replaced';END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
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
 -- Written before the rejection is read, exactly as activate_release does (M13R-01): a rejection that committed after this
 -- transaction's snapshot makes this write fail with 40001, so a veto that was acknowledged to its author is never overtaken by a
 -- rollback that started earlier (independent reviews IR-06 and M14R-04).
 INSERT INTO control.release_fences AS f(tenant_id,app_id,artifact_hash) VALUES(t,p_app,p_target) ON CONFLICT(tenant_id,app_id,artifact_hash) DO UPDATE SET writes=f.writes+1;
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
DO $$ DECLARE f oid:='control.rollback_release(text,text,text,bigint,text,boolean)'::regprocedure;
 owner_name name;
BEGIN
 SELECT pg_get_userbyid(proowner) INTO owner_name FROM pg_proc WHERE oid=f;
 IF owner_name<>'framework_schema_owner' THEN RAISE EXCEPTION 'rollback_release changed owner';END IF;
 IF has_function_privilege('framework_worker',f,'EXECUTE') OR has_function_privilege('framework_dispatcher',f,'EXECUTE') OR NOT has_function_privilege('framework_app',f,'EXECUTE')
  OR EXISTS(SELECT 1 FROM pg_proc p,LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=f AND a.grantee=0) THEN
  RAISE EXCEPTION 'rollback_release ACL drifted';END IF;
 IF position('release_fences' IN pg_get_functiondef(f))=0 THEN RAISE EXCEPTION 'rollback_release does not write the release fence';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain the release fence; forward recovery only';END $$;
-- +goose StatementEnd
