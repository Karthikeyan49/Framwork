package config

import (
	"strings"
	"testing"
)

func TestConfiguration(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://app:private@localhost/db?sslmode=disable", "REDIS_URL": "redis://:private@localhost:6379/0", "AUTH_ISSUER": "https://issuer.example", "AUTH_AUDIENCE": "api", "AUTH_JWKS_FILE": "/run/auth/public.json"}
	cases := []struct {
		name      string
		updates   map[string]string
		wantError bool
	}{
		{"development", nil, false}, {"test", map[string]string{"APP_ENV": "test"}, false},
		{"unknown environment", map[string]string{"APP_ENV": "staging"}, true},
		{"missing dependency", map[string]string{"REDIS_URL": ""}, true},
		{"malformed URL", map[string]string{"DATABASE_URL": "postgres://%"}, true},
		{"production insecure", map[string]string{"APP_ENV": "production"}, true},
		{"production secure", map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgres://app:private@db/db?sslmode=verify-full", "REDIS_URL": "rediss://:private@cache:6379/0"}, false},
		{"production no password", map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgres://app@db/db?sslmode=verify-full"}, true},
		{"wildcard CORS", map[string]string{"CORS_ORIGINS": "*"}, true},
		{"CORS path", map[string]string{"CORS_ORIGINS": "https://example.com/path"}, true},
		{"exact CORS", map[string]string{"CORS_ORIGINS": "http://localhost:5173"}, false},
		{"bad address", map[string]string{"HTTP_ADDR": "invalid"}, true},
		{"invalid log level", map[string]string{"LOG_LEVEL": "verbose"}, true},
		{"invalid telemetry", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://user:secret@collector"}, true},
		{"bad event flag", map[string]string{"EVENTS_ENABLED": "maybe"}, true},
		{"production preview events", map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgres://app:private@db/db?sslmode=verify-full", "REDIS_URL": "rediss://:private@cache:6379/0", "EVENTS_ENABLED": "true"}, true},
		{"dedicated broker", map[string]string{"EVENT_REDIS_URL": "redis://event_worker:private@localhost:6379/0"}, false},
		{"ambient broker admin", map[string]string{"EVENT_REDIS_URL": "redis://:private@localhost:6379/0"}, true},
		{"broker credential alias", map[string]string{"EVENT_REDIS_URL": "redis://event_worker:private@localhost:6379/0?password=override"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			for k, v := range base {
				values[k] = v
			}
			for k, v := range tc.updates {
				values[k] = v
			}
			_, err := Load(func(k string) string { return values[k] })
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("credential in error")
			}
		})
	}
}

func TestMigrationConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, environment, raw string
		invalid                bool
	}{
		{"missing", "development", "", true},
		{"development", "development", "postgres://migrator:private@localhost/db?sslmode=disable", false},
		{"production TLS required", "production", "postgres://migrator:private@db/db?sslmode=disable", true},
		{"production credentials required", "production", "postgres://migrator@db/db?sslmode=verify-full", true},
		{"production secure", "production", "postgres://migrator:private@db/db?sslmode=verify-full", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MigrationURL(func(key string) string {
				if key == "APP_ENV" {
					return tc.environment
				}
				if key == "MIGRATION_DATABASE_URL" {
					return tc.raw
				}
				return ""
			})
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("credential in migration error")
			}
		})
	}
}

func TestProductionDriverTrustCannotBeOverridden(t *testing.T) {
	secureDB := "postgres://app:private@db.example/db?sslmode=verify-full"
	secureRedis := "rediss://:private@cache.example:6379/0"
	for _, suffix := range []string{"&sslmode=disable", "&ssl=true", "&password=", "&host=other.example", "&sslmode=verify-full", "&sslmode=verify-ca", "&sslmode=require"} {
		t.Run("postgres "+suffix, func(t *testing.T) {
			values := map[string]string{"APP_ENV": "production", "DATABASE_URL": secureDB + suffix, "REDIS_URL": secureRedis, "MIGRATION_DATABASE_URL": secureDB + suffix, "AUTH_ISSUER": "https://issuer.example", "AUTH_AUDIENCE": "api", "AUTH_JWKS_FILE": "/run/auth/public.json"}
			getenv := func(key string) string { return values[key] }
			if _, err := Load(getenv); err == nil {
				t.Fatal("runtime accepted PostgreSQL trust override")
			}
			if _, err := MigrationURL(getenv); err == nil {
				t.Fatal("migrations accepted PostgreSQL trust override")
			}
		})
	}
	for _, suffix := range []string{"?skip_verify=true", "?skip_verify=false&skip_verify=true", "?password="} {
		t.Run("redis "+suffix, func(t *testing.T) {
			values := map[string]string{"APP_ENV": "production", "DATABASE_URL": secureDB, "REDIS_URL": secureRedis + suffix, "AUTH_ISSUER": "https://issuer.example", "AUTH_AUDIENCE": "api", "AUTH_JWKS_FILE": "/run/auth/public.json"}
			if _, err := Load(func(key string) string { return values[key] }); err == nil {
				t.Fatal("runtime accepted Redis trust override")
			}
		})
	}
}
