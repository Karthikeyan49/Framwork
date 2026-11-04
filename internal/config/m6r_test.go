package config

import (
	"testing"
)

func TestIndependentM6RWorkerProductionTrust(t *testing.T) {
	for _, key := range []string{"WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		for _, url := range []string{"postgres://worker:synthetic@db.example/db?sslmode=disable", "postgres://worker:synthetic@db.example/db?sslmode=verify-full&sslmode=disable", "postgres://worker:synthetic@db.example/db?sslmode=verify-full&ssl=true", "postgres://worker:synthetic@db.example/db?sslmode=verify-full&password=", "postgres://worker@db.example/db?sslmode=verify-full"} {
			values := map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgres://app:synthetic@db.example/db?sslmode=verify-full", "REDIS_URL": "rediss://:synthetic@cache.example:6379/0", "AUTH_ISSUER": "https://issuer.example", "AUTH_AUDIENCE": "api", "AUTH_JWKS_FILE": "/run/auth/public.json", key: url}
			if _, e := Load(func(k string) string { return values[k] }); e == nil {
				t.Fatal("dedicated role transport trust override accepted", key)
			}
		}
	}
	t.Log("worker+dispatcher production transport/credential overrides rejected")
}
