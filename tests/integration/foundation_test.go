//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/platform/app"
	"github.com/Karthikeyan49/Framwork/internal/platform/cache"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/pressly/goose/v3"
)

func TestFoundationDependenciesAndMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	db, err := postgres.Open(c.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := postgres.Ping(ctx, db); err != nil {
		t.Fatal(err)
	}
	redis, err := cache.Open(c.RedisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer redis.Close()
	if err := redis.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var superuser, bypass, createdb, createrole bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper,rolbypassrls,rolcreatedb,rolcreaterole FROM pg_roles WHERE rolname=current_user`).Scan(&superuser, &bypass, &createdb, &createrole); err != nil {
		t.Fatal(err)
	}
	if superuser || bypass || createdb || createrole {
		t.Fatal("application role is privileged")
	}
	adminPassword := os.Getenv("POSTGRES_PASSWORD")
	if adminPassword == "" {
		t.Fatal("POSTGRES_PASSWORD required for local privileged-role rejection test")
	}
	adminURL, err := url.Parse(c.DatabaseURL)
	if err != nil {
		t.Fatal("invalid local database URL")
	}
	adminURL.User = url.UserPassword("postgres", adminPassword)
	adminDB, err := postgres.Open(adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatal("admin connection failed")
	}
	if postgres.Ping(ctx, adminDB) == nil {
		t.Fatal("runtime readiness accepted privileged database role")
	}
	migrationURL := os.Getenv("MIGRATION_DATABASE_URL")
	if migrationURL == "" {
		t.Fatal("MIGRATION_DATABASE_URL required")
	}
	migrationDB, err := postgres.Open(migrationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer migrationDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, migrationDB, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal("migration failed")
	}
	second, err := provider.Up(ctx)
	if err != nil || len(second) != 0 {
		t.Fatalf("migration not repeatable: count=%d", len(second))
	}
	var count int
	if err := migrationDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname IN ('iam','control','runtime','audit') AND nspowner=(SELECT oid FROM pg_roles WHERE rolname=current_user)`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("migration role does not own four schemas: count=%d", count)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE public.m0_privilege_probe (id integer)`); err == nil {
		_, _ = migrationDB.ExecContext(ctx, `DROP TABLE public.m0_privilege_probe`)
		t.Fatal("application role can create tables")
	}
	if _, err := db.ExecContext(ctx, `SELECT * FROM public.goose_db_version`); err == nil {
		t.Fatal("application role can access migration ledger")
	}
}

func TestAPIWithoutCollectorAndDependencyOutages(t *testing.T) {
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	// go test runs from this package; the local command defaults are relative to
	// the repository root. Preserve explicit absolute operator bindings.
	if !filepath.IsAbs(c.AuthJWKSFile) {
		c.AuthJWKSFile, err = filepath.Abs(filepath.Join("../..", c.AuthJWKSFile))
		if err != nil {
			t.Fatal(err)
		}
	}
	// A closed local endpoint simulates a temporarily unavailable collector.
	c.OTelEndpoint = "http://127.0.0.1:1"
	for _, offline := range []string{"none", "postgres", "redis"} {
		t.Run(offline, func(t *testing.T) {
			current := c
			if offline == "postgres" {
				current.DatabaseURL = "postgres://app:unused@127.0.0.1:1/db?sslmode=disable"
			}
			if offline == "redis" {
				current.RedisURL = "redis://127.0.0.1:1/0"
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			current.HTTPAddr = listener.Addr().String()
			_ = listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			go func() { done <- app.Run(ctx, "framework-api", current, logger) }()
			client := &http.Client{Timeout: 3 * time.Second}
			deadline := time.Now().Add(5 * time.Second)
			var response *http.Response
			for time.Now().Before(deadline) {
				response, err = client.Get("http://" + current.HTTPAddr + "/healthz")
				if err == nil {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if err != nil {
				t.Fatal("API startup failed with collector unavailable")
			}
			if response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
				t.Fatal("health contract failed")
			}
			_ = response.Body.Close()
			response, err = client.Get("http://" + current.HTTPAddr + "/readyz")
			if err != nil {
				t.Fatal(err)
			}
			want := 200
			if offline != "none" {
				want = 503
			}
			if response.StatusCode != want {
				t.Fatalf("readiness status=%d want=%d", response.StatusCode, want)
			}
			var body struct {
				Status       string            `json:"status"`
				Dependencies map[string]string `json:"dependencies"`
			}
			err = json.NewDecoder(response.Body).Decode(&body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if offline != "none" && body.Dependencies[offline] != "unavailable" {
				t.Fatal("failed dependency not reported")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(12 * time.Second):
				t.Fatal("graceful shutdown exceeded bound")
			}
		})
	}
}

func TestWorkerLifecycle(t *testing.T) {
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	c.OTelEndpoint = ""
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, "framework-worker", c, slog.New(slog.NewJSONHandler(io.Discard, nil))) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker shutdown timed out")
	}
}
