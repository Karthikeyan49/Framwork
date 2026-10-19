-- Preserve applied 43. Independent R5c-b review: bound one team's retained
-- memberships (active and inactive) at the listing budget. Owner/ACL unchanged.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.set_team_member(t uuid,team uuid,member uuid,enabled boolean,expected boolean) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
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
  -- Retained rows (including inactive history) are bounded so administrators
  -- can always list a team and read its membership witnesses.
  IF (SELECT count(*) FROM iam.team_memberships x WHERE x.tenant_id=t AND x.team_id=team)>=1000 THEN
   RAISE EXCEPTION 'team membership budget exceeded' USING ERRCODE='22023';END IF;
  INSERT INTO iam.team_memberships(tenant_id,team_id,user_id,active) VALUES(t,team,member,true);
 END IF;
 IF NOT control.team_hierarchy_valid(t) THEN RAISE EXCEPTION 'team hierarchy budget or cycle' USING ERRCODE='22023';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain team history and assignments; forward recovery only';END $$;
-- +goose StatementEnd
