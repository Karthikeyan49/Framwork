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
	var privileged bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole
 OR EXISTS (SELECT 1 FROM pg_namespace n WHERE n.nspname IN ('iam','audit','control','runtime','public')
   AND (n.nspowner=r.oid OR has_schema_privilege(n.oid,'CREATE')))
 OR EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
   WHERE n.nspname IN ('iam','audit','control','runtime') AND
   (c.relowner=r.oid OR (n.nspname='iam' AND has_table_privilege(c.oid,'INSERT,UPDATE,DELETE,TRUNCATE'))
    OR (n.nspname='audit' AND has_table_privilege(c.oid,'UPDATE,DELETE,TRUNCATE'))))
 FROM pg_roles r WHERE rolname=current_user`).Scan(&privileged); err != nil || privileged {
		span.SetStatus(codes.Error, "application database role is unsafe")
		return errors.New("application database role is unsafe")
	}
	return nil
}
