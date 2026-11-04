-- R5c-b governed team administration. Teams/memberships stay operator-owned
-- tables; the runtime role changes them only through these audited CAS ports.
-- Hierarchy edits are serialized per tenant and re-validated as a whole so the
-- TEAM DataScope resolver (depth <=8, <=64 teams per user, no cycles) never
-- fails closed on administrator input. Existing rows and grants are preserved.
-- +goose Up
ALTER TABLE iam.teams ADD version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 1000000000);
-- +goose StatementBegin
CREATE FUNCTION control.team_manager(t uuid) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t) THEN
  RAISE EXCEPTION 'tenant team administrator required' USING ERRCODE='42501';END IF;
 -- Defense in depth: the trusted caller decides teams.manage; viewers never manage.
 IF NOT EXISTS(SELECT 1 FROM iam.memberships m JOIN iam.users u ON u.id=m.user_id AND u.active
  WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role<>'viewer') THEN
  RAISE EXCEPTION 'tenant team administrator required' USING ERRCODE='42501';END IF;
 -- A changed row forces concurrent REPEATABLE READ hierarchy editors to abort,
 -- so two individually valid reparents cannot together create a cycle.
 UPDATE iam.tenants SET name=name WHERE id=t;
END $$;
CREATE FUNCTION control.team_hierarchy_valid(t uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 WITH RECURSIVE walk(id,depth) AS (
  SELECT id,0 FROM iam.teams WHERE tenant_id=t AND parent_id IS NULL
  UNION ALL SELECT c.id,w.depth+1 FROM iam.teams c JOIN walk w ON c.parent_id=w.id WHERE c.tenant_id=t AND w.depth<9
 ), closure(member,team,depth) AS (
  SELECT m.user_id,m.team_id,0 FROM iam.team_memberships m WHERE m.tenant_id=t AND m.active
  UNION ALL SELECT c.member,x.id,c.depth+1 FROM iam.teams x JOIN closure c ON x.parent_id=c.team WHERE x.tenant_id=t AND c.depth<9
 )
 -- Every team is reachable from a root (no cycle), within depth 8, and every
 -- member resolves at most 64 teams.
 SELECT (SELECT count(DISTINCT id) FROM walk WHERE depth<=8)=(SELECT count(*) FROM iam.teams WHERE tenant_id=t)
  AND NOT EXISTS(SELECT 1 FROM walk WHERE depth>8)
  AND NOT EXISTS(SELECT 1 FROM closure GROUP BY member HAVING count(DISTINCT team)>64)
$$;
CREATE FUNCTION control.save_team(t uuid,team uuid,expected bigint,label text,parent uuid) RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE current_version bigint;
BEGIN
 PERFORM control.team_manager(t);
 IF team IS NULL OR expected IS NULL OR expected<0 OR expected>=1000000000 OR label IS NULL OR length(label) NOT BETWEEN 1 AND 120
 OR label<>btrim(label) OR label ~ '[[:cntrl:]]' OR parent IS NOT DISTINCT FROM team THEN
  RAISE EXCEPTION 'invalid team witness' USING ERRCODE='22023';END IF;
 IF parent IS NOT NULL AND NOT EXISTS(SELECT 1 FROM iam.teams x WHERE x.tenant_id=t AND x.id=parent) THEN
  RAISE EXCEPTION 'tenant parent team required' USING ERRCODE='22023';END IF;
 SELECT x.version INTO current_version FROM iam.teams x WHERE x.tenant_id=t AND x.id=team FOR UPDATE;
 IF expected=0 THEN
  IF FOUND THEN RAISE EXCEPTION 'team already exists' USING ERRCODE='55000';END IF;
  IF (SELECT count(*) FROM iam.teams x WHERE x.tenant_id=t)>=256 THEN RAISE EXCEPTION 'team budget exceeded' USING ERRCODE='22023';END IF;
  INSERT INTO iam.teams(tenant_id,id,parent_id,name,version) VALUES(t,team,parent,label,1);
 ELSE
  IF NOT FOUND OR current_version<>expected THEN RAISE EXCEPTION 'team version changed' USING ERRCODE='55000';END IF;
  UPDATE iam.teams x SET name=label,parent_id=parent,version=expected+1 WHERE x.tenant_id=t AND x.id=team;
 END IF;
 IF NOT control.team_hierarchy_valid(t) THEN RAISE EXCEPTION 'team hierarchy budget or cycle' USING ERRCODE='22023';END IF;
 RETURN expected+1;
END $$;
CREATE FUNCTION control.set_team_member(t uuid,team uuid,member uuid,enabled boolean,expected boolean) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE current_active boolean;
BEGIN
 PERFORM control.team_manager(t);
 IF team IS NULL OR member IS NULL OR enabled IS NULL THEN RAISE EXCEPTION 'invalid team membership witness' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM iam.teams x WHERE x.tenant_id=t AND x.id=team) THEN RAISE EXCEPTION 'tenant team required' USING ERRCODE='22023';END IF;
 -- Deactivation must work for departed users; activation needs a current member.
 IF enabled AND NOT EXISTS(SELECT 1 FROM iam.memberships m JOIN iam.users u ON u.id=m.user_id AND u.active
  WHERE m.tenant_id=t AND m.user_id=member AND m.active) THEN
  RAISE EXCEPTION 'active tenant member required' USING ERRCODE='22023';END IF;
 SELECT x.active INTO current_active FROM iam.team_memberships x WHERE x.tenant_id=t AND x.team_id=team AND x.user_id=member FOR UPDATE;
 IF current_active IS DISTINCT FROM expected THEN RAISE EXCEPTION 'team membership changed' USING ERRCODE='55000';END IF;
 IF current_active IS NOT DISTINCT FROM enabled OR (NOT FOUND AND NOT enabled) THEN RAISE EXCEPTION 'team membership unchanged' USING ERRCODE='22023';END IF;
 IF FOUND THEN
  UPDATE iam.team_memberships x SET active=enabled WHERE x.tenant_id=t AND x.team_id=team AND x.user_id=member;
 ELSE
  INSERT INTO iam.team_memberships(tenant_id,team_id,user_id,active) VALUES(t,team,member,true);
 END IF;
 IF NOT control.team_hierarchy_valid(t) THEN RAISE EXCEPTION 'team hierarchy budget or cycle' USING ERRCODE='22023';END IF;
END $$;
-- Runtime RLS only shows a user's own memberships; administrators list a team.
CREATE FUNCTION control.team_members(t uuid,team uuid) RETURNS TABLE(user_id uuid,active boolean) LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t) THEN
  RAISE EXCEPTION 'tenant team reader required' USING ERRCODE='42501';END IF;
 RETURN QUERY SELECT m.user_id,m.active FROM iam.team_memberships m WHERE m.tenant_id=t AND m.team_id=team ORDER BY m.user_id LIMIT 1001;
END $$;
-- +goose StatementEnd
ALTER FUNCTION control.team_manager(uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION control.team_hierarchy_valid(uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION control.save_team(uuid,uuid,bigint,text,uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION control.set_team_member(uuid,uuid,uuid,boolean,boolean) OWNER TO framework_schema_owner;
ALTER FUNCTION control.team_members(uuid,uuid) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.team_manager(uuid),control.team_hierarchy_valid(uuid),control.save_team(uuid,uuid,bigint,text,uuid),control.set_team_member(uuid,uuid,uuid,boolean,boolean),control.team_members(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.save_team(uuid,uuid,bigint,text,uuid),control.set_team_member(uuid,uuid,uuid,boolean,boolean),control.team_members(uuid,uuid) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain team history and assignments; forward recovery only';END $$;
-- +goose StatementEnd
