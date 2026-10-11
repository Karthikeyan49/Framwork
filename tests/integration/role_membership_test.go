//go:build integration

package integration

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

// Called only inside the uniquely named fresh-database fixture. Private probe
// roles avoid any mutation of the real framework_app role's memberships. Admin
// SET SESSION AUTHORIZATION simulates a role without distributing a new password.
func checkNonInheritedOwnerMembership(t *testing.T, ctx context.Context, adminURL *url.URL, database string) {
	t.Helper()
	binding := *adminURL
	binding.Path = "/" + database
	admin, err := postgres.Open(binding.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	suffix := strings.ReplaceAll(uuid(t), "-", "")
	ownerName, probeName := "m2_owner_"+suffix, "m2_probe_"+suffix
	owner, probe := pgx.Identifier{ownerName}.Sanitize(), pgx.Identifier{probeName}.Sanitize()
	setup := `CREATE ROLE ` + owner + ` NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION;
 CREATE ROLE ` + probe + ` NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION;
 GRANT framework_app TO ` + probe + `;
 GRANT ` + owner + ` TO ` + probe + ` WITH INHERIT FALSE, SET TRUE;
 GRANT USAGE ON SCHEMA runtime TO ` + owner + `;
 CREATE TABLE runtime.m2_owned (tenant_id uuid NOT NULL,id int,PRIMARY KEY(tenant_id,id));
 INSERT INTO runtime.m2_owned VALUES('11111111-1111-4111-8111-111111111111',1);
 ALTER TABLE runtime.m2_owned ENABLE ROW LEVEL SECURITY;
 ALTER TABLE runtime.m2_owned FORCE ROW LEVEL SECURITY;
 CREATE POLICY denied ON runtime.m2_owned TO framework_app USING(false);
 GRANT SELECT ON runtime.m2_owned TO framework_app;
 ALTER TABLE runtime.m2_owned OWNER TO ` + owner
	if _, err = admin.ExecContext(ctx, setup); err != nil {
		t.Fatal("private owner-membership fixture setup failed")
	}
	defer func() {
		// Restore only this disposable DB's schema; revoke private-role object grants.
		if _, err := admin.ExecContext(context.Background(), `ALTER SCHEMA runtime OWNER TO framework_migrator; DROP FUNCTION IF EXISTS runtime.m2_owner_func(); DROP TABLE runtime.m2_owned; DROP OWNED BY `+probe+`,`+owner+`; DROP ROLE `+probe+`; DROP ROLE `+owner); err != nil {
			t.Error("private role fixture cleanup failed")
		}
	}()
	for _, kind := range []string{"table", "function", "schema"} {
		t.Run("non-inherited "+kind+" owner", func(t *testing.T) {
			switch kind {
			case "function":
				if _, err := admin.ExecContext(ctx, `ALTER TABLE runtime.m2_owned OWNER TO framework_migrator; CREATE FUNCTION runtime.m2_owner_func() RETURNS boolean LANGUAGE sql AS 'SELECT true'; ALTER FUNCTION runtime.m2_owner_func() OWNER TO `+owner); err != nil {
					t.Fatal("function owner fixture failed")
				}
			case "schema":
				if _, err := admin.ExecContext(ctx, `DROP FUNCTION runtime.m2_owner_func(); ALTER SCHEMA runtime OWNER TO `+owner); err != nil {
					t.Fatal("schema owner fixture failed")
				}
			}
			db, err := postgres.Open(binding.String())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			if _, err = db.ExecContext(ctx, `SET SESSION AUTHORIZATION `+probe); err != nil {
				t.Fatal("private probe binding failed")
			}
			var inherited bool
			if err = db.QueryRowContext(ctx, `SELECT pg_catalog.pg_has_role(session_user,$1,'USAGE')`, ownerName).Scan(&inherited); err != nil || inherited {
				t.Fatal("owner privilege unexpectedly inherited")
			}
			if kind == "table" {
				// Prove the unsafe membership permits a real RLS bypass via SET ROLE. This
				// transaction rolls back DDL; production code must never execute such SQL.
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.ExecContext(ctx, `SET ROLE `+owner+`; ALTER TABLE runtime.m2_owned DISABLE ROW LEVEL SECURITY`); err != nil {
					tx.Rollback()
					t.Fatal("unsafe owner capability not reproduced")
				}
				var count int
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM runtime.m2_owned`).Scan(&count); err != nil || count != 1 {
					tx.Rollback()
					t.Fatal("owner bypass not reproduced")
				}
				if err = tx.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			err = postgres.WithRuntime(ctx, db, func(context.Context, postgres.Executor) error { called = true; return nil })
			if !errors.Is(err, postgres.ErrUnavailable) || called {
				t.Fatal("non-inherited owner membership entered runtime")
			}
		})
	}
}
