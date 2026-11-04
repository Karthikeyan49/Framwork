-- Independent review fix F-05 (docs/verification/M14_AND_FIXES_INDEPENDENT_REVIEW.md). Additive and forward-only.
-- M13R-01 made reject_release and activate_release write one control.release_fences row per hash, and 00091 added rollback_release, so that a
-- transaction whose REPEATABLE READ snapshot is older than a committed conflicting change aborts with 40001 instead of acting on a stale view.
-- approve_release was left out: an approval whose transaction began before a committed rejection was recorded on the rejected hash (S3), and an
-- approval whose transaction began before an activation or a rollback moved the pointer was recorded on the superseded candidate (S4, S5). The
-- approval cannot move the pointer (activation is fenced and compare-and-set), but the port's own refusals ("was rejected", "is stale") were
-- overtaken and the approvals that promotion shows and counts then included one made after a veto.
-- approve_release now (1) locks the active pointer row FOR SHARE first, the lock order of activate_release and rollback_release, so a pointer
-- that moved after the snapshot raises 40001 and approvers do not wait for each other, and (2) writes the fence row with ON CONFLICT DO NOTHING
-- before it reads the rejections: under REPEATABLE READ that statement raises 40001 when the row was updated after the snapshot, and changes
-- nothing when it is unchanged, so concurrent approvers never conflict on an existing row.
-- For (2) the row has to exist before anybody's snapshot, or the first approval of a candidate would create it and a veto racing that
-- moment would be the one to abort (a veto must stay possible): every candidate gets its fence row when it is staged (an AFTER INSERT trigger
-- on control.release_candidates) and the candidates that exist now get theirs here.
-- Replaces control.approve_release(text,text) from its only definition (00084), byte for byte except for those two statements and the order in
-- which the pointer is read. Owner, ACL and every refusal are unchanged; the migration fails if the owner or the ACL moved.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
 IF (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname='approve_release')<>1 THEN
  RAISE EXCEPTION 'control.approve_release must exist exactly once before it is replaced';END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION control.release_candidate_fence() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 INSERT INTO control.release_fences(tenant_id,app_id,artifact_hash) VALUES(NEW.tenant_id,NEW.app_id,NEW.artifact_hash) ON CONFLICT DO NOTHING;
 RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION control.release_candidate_fence() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.release_candidate_fence() FROM PUBLIC,framework_app,framework_worker,framework_dispatcher;
CREATE TRIGGER release_candidate_fence AFTER INSERT ON control.release_candidates FOR EACH ROW EXECUTE FUNCTION control.release_candidate_fence();
-- The candidates that exist now. The fence table is owned by the schema owner with forced row-level security, so the rows are written as it.
-- +goose StatementBegin
DO $$ BEGIN
 SET LOCAL ROLE framework_schema_owner;
 INSERT INTO control.release_fences(tenant_id,app_id,artifact_hash) SELECT c.tenant_id,c.app_id,c.artifact_hash FROM control.release_candidates c ON CONFLICT DO NOTHING;
 RESET ROLE;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.approve_release(p_app text,p_hash text) RETURNS integer
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();c record;cur record;has_cur boolean;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release approver required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'invalid approval request' USING ERRCODE='22023';END IF;
 SELECT r.requester_id,r.acceptance_author,r.sandbox_frozen_by,r.risk_level,r.base_release,r.base_generation INTO c
  FROM control.release_candidates r WHERE r.tenant_id=t AND r.app_id=p_app AND r.artifact_hash=p_hash;
 IF NOT FOUND THEN RAISE EXCEPTION 'release candidate not found' USING ERRCODE='P0002';END IF;
 -- The pointer first, shared, in the lock order of activate_release and rollback_release: a pointer row that was updated after this
 -- transaction's snapshot makes the lock fail with 40001, so an approval is never recorded on a candidate whose base the pointer had
 -- already left (review F-05, S4 and S5). Approvers do not conflict with each other here.
 SELECT a.artifact_hash,a.generation INTO cur FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app FOR SHARE;
 has_cur:=FOUND;
 -- Written before the rejection is read, as in activate_release (M13R-01): a rejection that committed after this transaction's snapshot
 -- changed or created the row, which fails this statement with 40001, so a veto acknowledged to its author is never followed by an approval
 -- of the same hash from a transaction that started earlier (review F-05, S3). An unchanged row is left alone.
 INSERT INTO control.release_fences AS f(tenant_id,app_id,artifact_hash) VALUES(t,p_app,p_hash) ON CONFLICT(tenant_id,app_id,artifact_hash) DO NOTHING;
 IF EXISTS(SELECT 1 FROM control.release_rejections x WHERE x.tenant_id=t AND x.app_id=p_app AND x.artifact_hash=p_hash) THEN
  RAISE EXCEPTION 'release candidate was rejected' USING ERRCODE='55000';END IF;
 IF (has_cur AND (cur.artifact_hash<>c.base_release OR cur.generation<>c.base_generation)) OR (NOT has_cur AND (c.base_release<>'' OR c.base_generation<>0)) THEN
  RAISE EXCEPTION 'candidate is stale' USING ERRCODE='55000';END IF;
 IF c.risk_level IN('HIGH','CRITICAL') AND actor=c.requester_id THEN
  RAISE EXCEPTION 'separation of duties: the requester cannot approve this candidate' USING ERRCODE='42501',DETAIL='requester';END IF;
 IF c.risk_level IN('HIGH','CRITICAL') AND actor=c.acceptance_author THEN
  RAISE EXCEPTION 'separation of duties: the acceptance author cannot approve this candidate' USING ERRCODE='42501',DETAIL='acceptance_author';END IF;
 IF c.risk_level='CRITICAL' AND actor=c.sandbox_frozen_by THEN
  RAISE EXCEPTION 'separation of duties: the sandbox freezer cannot approve this candidate' USING ERRCODE='42501',DETAIL='sandbox_freezer';END IF;
 INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash,actor_id) VALUES(t,p_app,p_hash,actor) ON CONFLICT DO NOTHING;
 RETURN control.release_approval_count(t,p_app,p_hash);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f oid:='control.approve_release(text,text)'::regprocedure;
 owner_name name;
BEGIN
 SELECT pg_get_userbyid(proowner) INTO owner_name FROM pg_proc WHERE oid=f;
 IF owner_name<>'framework_schema_owner' THEN RAISE EXCEPTION 'approve_release changed owner';END IF;
 IF has_function_privilege('framework_worker',f,'EXECUTE') OR has_function_privilege('framework_dispatcher',f,'EXECUTE') OR NOT has_function_privilege('framework_app',f,'EXECUTE')
  OR EXISTS(SELECT 1 FROM pg_proc p,LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=f AND a.grantee=0) THEN
  RAISE EXCEPTION 'approve_release ACL drifted';END IF;
 IF position('release_fences' IN pg_get_functiondef(f))=0 OR position('FOR SHARE' IN pg_get_functiondef(f))=0 THEN RAISE EXCEPTION 'approve_release does not fence the approval';END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid='control.release_candidates'::regclass AND t.tgname='release_candidate_fence' AND NOT t.tgisinternal) THEN
  RAISE EXCEPTION 'release candidates do not get their fence row';END IF;
 IF (SELECT pg_get_userbyid(proowner) FROM pg_proc WHERE oid='control.release_candidate_fence()'::regprocedure)<>'framework_schema_owner'
  OR has_function_privilege('framework_app','control.release_candidate_fence()','EXECUTE') OR has_function_privilege('framework_worker','control.release_candidate_fence()','EXECUTE')
  OR has_function_privilege('framework_dispatcher','control.release_candidate_fence()','EXECUTE')
  OR EXISTS(SELECT 1 FROM pg_proc p,LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid='control.release_candidate_fence()'::regprocedure AND a.grantee=0) THEN
  RAISE EXCEPTION 'release_candidate_fence owner or ACL drifted';END IF;
 IF EXISTS(SELECT 1 FROM control.release_candidates c WHERE NOT EXISTS(SELECT 1 FROM control.release_fences f WHERE f.tenant_id=c.tenant_id AND f.app_id=c.app_id AND f.artifact_hash=c.artifact_hash)) THEN
  RAISE EXCEPTION 'a release candidate has no fence row';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain the release fence; forward recovery only';END $$;
-- +goose StatementEnd
