-- R5c record access administration. One audited, CAS-fenced port changes a
-- record's owner/team or one explicit share. Callers authorize the record via
-- live DataScope first; this port re-checks tenant scope, the exact live record
-- version, recipient membership/team and the dataset write fence. It never
-- accepts SQL, never changes business values and never broadens table grants.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION runtime.administer_record_access(t uuid,a text,rev bigint,h text,e text,rid uuid,expected bigint,op text,target_user uuid,target_team uuid,share_write boolean)
RETURNS bigint LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d record;live record;canonical boolean:=false;changed integer;shares integer;advanced bigint;
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t) THEN
  RAISE EXCEPTION 'tenant record administrator required' USING ERRCODE='42501';END IF;
 IF a IS NULL OR rev IS NULL OR h IS NULL OR e IS NULL OR rid IS NULL OR expected IS NULL OR expected<1 OR expected>=9007199254740991
 OR op IS NULL OR op NOT IN('owner','share','unshare')
 OR (op='owner' AND (target_user IS NULL OR share_write IS NOT NULL))
 OR (op='share' AND (target_user IS NULL OR target_team IS NOT NULL OR share_write IS NULL))
 OR (op='unshare' AND (target_user IS NULL OR target_team IS NOT NULL OR share_write IS NOT NULL)) THEN
  RAISE EXCEPTION 'invalid record access witness' USING ERRCODE='22023';END IF;
 -- Recipients must be current members; token/request claims are never trusted.
 IF NOT EXISTS(SELECT 1 FROM iam.memberships m JOIN iam.users u ON u.id=m.user_id AND u.active JOIN iam.tenants x ON x.id=m.tenant_id AND x.active
  WHERE m.tenant_id=t AND m.user_id=target_user AND m.active) THEN
  RAISE EXCEPTION 'active tenant member required' USING ERRCODE='55000';END IF;
 IF target_team IS NOT NULL AND NOT EXISTS(SELECT 1 FROM iam.teams x WHERE x.tenant_id=t AND x.id=target_team) THEN
  RAISE EXCEPTION 'tenant team required' USING ERRCODE='55000';END IF;
 SELECT x.canonical_access,x.id dataset_id INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id
  WHERE g.tenant_id=t AND g.app_id=a AND g.anchor_revision=rev AND g.anchor_hash=h;
 IF FOUND THEN canonical:=d.canonical_access;END IF;
 -- Lock the exact live version. A concurrent writer aborts this RR transaction.
 SELECT r.row_version INTO live FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=rev AND r.ir_hash=h AND r.entity_id=e AND r.id=rid FOR UPDATE;
 IF NOT FOUND OR live.row_version<>expected THEN RAISE EXCEPTION 'record version changed' USING ERRCODE='55000';END IF;
 IF canonical AND NOT EXISTS(SELECT 1 FROM runtime.record_identity c WHERE c.tenant_id=t AND c.app_id=a AND c.dataset_id=d.dataset_id AND c.entity_id=e AND c.id=rid AND NOT c.tombstone) THEN
  RAISE EXCEPTION 'live record identity required' USING ERRCODE='55000';END IF;
 IF op='owner' THEN
  IF canonical THEN
   UPDATE runtime.record_identity c SET owner_user_id=target_user,owner_team_id=target_team WHERE c.tenant_id=t AND c.app_id=a AND c.dataset_id=d.dataset_id AND c.entity_id=e AND c.id=rid;
  END IF;
 ELSIF op='share' THEN
  IF canonical THEN
   SELECT count(*) INTO shares FROM runtime.identity_shares s WHERE s.tenant_id=t AND s.app_id=a AND s.dataset_id=d.dataset_id AND s.entity_id=e AND s.id=rid AND s.user_id<>target_user;
  ELSE
   SELECT count(*) INTO shares FROM runtime.record_shares s WHERE s.tenant_id=t AND s.app_id=a AND s.metadata_revision=rev AND s.ir_hash=h AND s.entity_id=e AND s.id=rid AND s.user_id<>target_user;
  END IF;
  IF shares>=64 THEN RAISE EXCEPTION 'record share budget exceeded' USING ERRCODE='22023';END IF;
  IF canonical THEN
   INSERT INTO runtime.identity_shares VALUES(t,a,d.dataset_id,e,rid,target_user,share_write)
    ON CONFLICT(tenant_id,app_id,dataset_id,entity_id,id,user_id) DO UPDATE SET can_write=excluded.can_write;
  ELSE
   -- The mirror trigger maintains a shadow identity share before cutover.
   INSERT INTO runtime.record_shares VALUES(t,a,rev,h,e,rid,target_user,share_write)
    ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) DO UPDATE SET can_write=excluded.can_write;
  END IF;
 ELSE
  IF canonical THEN
   DELETE FROM runtime.identity_shares s WHERE s.tenant_id=t AND s.app_id=a AND s.dataset_id=d.dataset_id AND s.entity_id=e AND s.id=rid AND s.user_id=target_user;
  ELSE
   DELETE FROM runtime.record_shares s WHERE s.tenant_id=t AND s.app_id=a AND s.metadata_revision=rev AND s.ir_hash=h AND s.entity_id=e AND s.id=rid AND s.user_id=target_user;
  END IF;
  GET DIAGNOSTICS changed=ROW_COUNT;
  IF changed<>1 THEN RAISE EXCEPTION 'record share absent' USING ERRCODE='55000';END IF;
 END IF;
 -- The record version is the per-record conflict witness for business and
 -- access changes. The dataset fence trigger re-validates binding/state; in
 -- canonical mode it copies the just-updated identity owner into this row.
 IF op='owner' THEN
  UPDATE runtime.records r SET owner_user_id=target_user,owner_team_id=target_team,row_version=row_version+1,updated_at=clock_timestamp(),updated_by=iam.authenticated_actor()
   WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=rev AND r.ir_hash=h AND r.entity_id=e AND r.id=rid AND r.row_version=expected RETURNING r.row_version INTO advanced;
 ELSE
  UPDATE runtime.records r SET row_version=row_version+1,updated_at=clock_timestamp(),updated_by=iam.authenticated_actor()
   WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=rev AND r.ir_hash=h AND r.entity_id=e AND r.id=rid AND r.row_version=expected RETURNING r.row_version INTO advanced;
 END IF;
 IF advanced IS NULL THEN RAISE EXCEPTION 'record version changed' USING ERRCODE='55000';END IF;
 RETURN advanced;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.administer_record_access(uuid,text,bigint,text,text,uuid,bigint,text,uuid,uuid,boolean) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.administer_record_access(uuid,text,bigint,text,text,uuid,bigint,text,uuid,uuid,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.administer_record_access(uuid,text,bigint,text,text,uuid,bigint,text,uuid,uuid,boolean) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain record access history; compatible forward fix only';END $$;
-- +goose StatementEnd
