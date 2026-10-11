package config

import "testing"

func TestProductionIdentityConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, issuer, audience, file string
		invalid                      bool
	}{
		{"explicit", "https://issuer.example", "api", "/run/auth/public.json", false},
		{"missing", "", "", "", true},
		{"local issuer", "http://localhost:8080/dev-issuer", "api", "/run/auth/public.json", true},
		{"local keys", "https://issuer.example", "api", ".local/identity/jwks.json", true},
		{"missing audience", "https://issuer.example", "", "/run/auth/public.json", true},
		{"issuer credentials", "https://user:secret@issuer.example", "api", "/run/auth/public.json", true},
		{"issuer query", "https://issuer.example?token=secret", "api", "/run/auth/public.json", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgres://app:private@db/db?sslmode=verify-full", "REDIS_URL": "rediss://:private@cache/0", "AUTH_ISSUER": tc.issuer, "AUTH_AUDIENCE": tc.audience, "AUTH_JWKS_FILE": tc.file}
			_, err := Load(func(key string) string { return values[key] })
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
		})
	}
}

func TestPartialDevelopmentAuthenticationFailsClosed(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://app:private@localhost/db?sslmode=disable", "REDIS_URL": "redis://:private@localhost/0", "AUTH_ISSUER": "https://external.example"}
	if _, err := Load(func(key string) string { return values[key] }); err == nil {
		t.Fatal("partial external authentication silently used local defaults")
	}
}
