#!/bin/sh
set -eu
# psql literal quoting prevents password interpolation into SQL syntax.
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set=ON_ERROR_STOP=1 \
  --set=migrator_password="$MIGRATOR_PASSWORD" --set=app_password="$APP_DB_PASSWORD" <<'SQL'
CREATE ROLE framework_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'migrator_password';
CREATE ROLE framework_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'app_password';
REVOKE ALL ON DATABASE framework FROM PUBLIC;
GRANT CONNECT, CREATE ON DATABASE framework TO framework_migrator;
GRANT CONNECT ON DATABASE framework TO framework_app;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO framework_migrator;
SQL
