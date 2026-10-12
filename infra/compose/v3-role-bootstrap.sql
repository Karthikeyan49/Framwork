-- Operator-only role provisioning. No passwords/default credentials in source.
DO $$ DECLARE role_name text; BEGIN
 FOREACH role_name IN ARRAY ARRAY['framework_schema_owner','framework_worker','framework_dispatcher','framework_audit_writer','framework_maintenance'] LOOP
  IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=role_name) THEN
   EXECUTE format('CREATE ROLE %I NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION',role_name);
  END IF;
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=role_name AND (rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication OR (role_name IN('framework_schema_owner','framework_audit_writer','framework_maintenance') AND rolcanlogin))) THEN
   RAISE EXCEPTION 'unsafe V3 database role drift';
  END IF;
  IF role_name IN('framework_worker','framework_dispatcher') AND EXISTS(SELECT 1 FROM pg_roles privileged WHERE privileged.rolname<>role_name AND (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreatedb OR privileged.rolcreaterole OR privileged.rolreplication OR privileged.rolname IN('framework_app','framework_worker','framework_dispatcher','framework_audit_writer','framework_migrator','framework_schema_owner','framework_maintenance','pg_read_all_data','pg_write_all_data')) AND pg_has_role(role_name,privileged.oid,'MEMBER')) THEN
   RAISE EXCEPTION 'unsafe V3 runtime role membership';
  END IF;
 END LOOP;
END $$;
GRANT framework_schema_owner TO framework_migrator;
GRANT CONNECT,CREATE ON DATABASE framework TO framework_schema_owner;
GRANT CONNECT ON DATABASE framework TO framework_worker,framework_dispatcher;
