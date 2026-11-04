// Package postgres owns database infrastructure, not business repositories.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func Open(raw string) (*sql.DB, error) {
	c, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	db := stdlib.OpenDB(*c)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}

// Ping records no DSN, SQL text, parameters, or driver error payload.
func Ping(ctx context.Context, db *sql.DB) error {
	ctx, span := otel.Tracer("framework/postgres").Start(ctx, "postgres.ping")
	defer span.End()
	span.SetAttributes(attribute.String("db.system.name", "postgresql"))
	if err := db.PingContext(ctx); err != nil {
		span.SetStatus(codes.Error, "dependency unavailable")
		return errors.New("PostgreSQL unavailable")
	}
	if err := checkRuntime(ctx, db); err != nil {
		span.SetStatus(codes.Error, "application database role is unsafe")
		return err
	}
	return nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func checkRuntime(ctx context.Context, db queryer) error {
	var privileged bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication
 OR current_user<>session_user OR pg_catalog.current_setting('row_security')<>'on'
 OR (session_user IN('framework_app','framework_worker','framework_dispatcher') AND EXISTS(SELECT 1 FROM pg_catalog.pg_roles other WHERE other.rolname IN('framework_app','framework_worker','framework_dispatcher','framework_audit_writer') AND other.rolname<>session_user AND pg_has_role(session_user,other.oid,'MEMBER')))
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_roles danger WHERE
   (danger.rolsuper OR danger.rolbypassrls OR danger.rolcreatedb OR danger.rolcreaterole OR danger.rolreplication OR danger.rolname IN ('framework_migrator','framework_maintenance','pg_read_all_data','pg_write_all_data','pg_read_server_files','pg_write_server_files','pg_execute_server_program'))
   AND pg_catalog.pg_has_role(session_user,danger.oid,'MEMBER'))
 OR EXISTS (SELECT 1 FROM pg_catalog.pg_namespace n WHERE n.nspname IN ('iam','audit','control','runtime','public')
   AND (n.nspowner=r.oid OR pg_catalog.pg_has_role(session_user,n.nspowner,'MEMBER') OR pg_catalog.has_schema_privilege(n.oid,'CREATE')))
 OR EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
   WHERE n.nspname IN ('iam','audit','control','runtime') AND c.relkind IN ('r','p') AND
   (c.relowner=r.oid OR pg_catalog.pg_has_role(session_user,c.relowner,'MEMBER') OR (n.nspname='iam' AND (pg_catalog.has_table_privilege(c.oid,'INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(c.oid,'INSERT,UPDATE')))
    OR (n.nspname='audit' AND (pg_catalog.has_table_privilege(c.oid,'UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(c.oid,'UPDATE')))
    OR (n.nspname IN ('control','runtime') AND (pg_catalog.has_table_privilege(c.oid,'SELECT,INSERT,UPDATE,DELETE') OR pg_catalog.has_any_column_privilege(c.oid,'SELECT,INSERT,UPDATE')) AND NOT(c.relrowsecurity AND c.relforcerowsecurity))
    OR (n.nspname IN ('iam','audit') AND c.relname<>'roles' AND NOT(c.relrowsecurity AND c.relforcerowsecurity))
    OR (n.nspname='iam' AND c.relname='roles' AND to_regclass('control.role_heads') IS NOT NULL AND pg_catalog.has_any_column_privilege(c.oid,'SELECT'))
    OR (n.nspname IN ('control','runtime') AND (pg_catalog.has_table_privilege(c.oid,'TRUNCATE,REFERENCES,TRIGGER') OR pg_catalog.has_any_column_privilege(c.oid,'REFERENCES')))))

 OR EXISTS(SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname IN ('iam','audit','runtime','control') AND
  (p.proowner=r.oid OR pg_catalog.pg_has_role(session_user,p.proowner,'MEMBER')))
 FROM pg_catalog.pg_roles r WHERE rolname=current_user`).Scan(&privileged); err != nil || privileged {
		return errors.New("application database role is unsafe")
	}
	return nil
}
