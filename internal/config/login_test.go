package config

import (
	"testing"
	"time"
)

func TestBoundedClockToleranceAndProviderConfiguration(t *testing.T) {
	base := map[string]string{"APP_ENV": "test", "DATABASE_URL": "postgres://app:private@localhost/db?sslmode=disable", "REDIS_URL": "redis://:private@localhost/0"}
	for _, tc := range []struct {
		value string
		valid bool
	}{{"", true}, {"0s", true}, {"30s", true}, {"2m", true}, {"121s", false}, {"-1s", false}, {"0.5s", false}, {"tomorrow", false}} {
		t.Run("skew_"+tc.value, func(t *testing.T) {
			m := map[string]string{}
			for k, v := range base {
				m[k] = v
			}
			m["AUTH_CLOCK_SKEW"] = tc.value
			c, e := Load(func(k string) string { return m[k] })
			if (e == nil) != tc.valid {
				t.Fatal("clock configuration acceptance")
			}
			if e == nil && tc.value == "30s" && c.AuthClockSkew != 30*time.Second {
				t.Fatal("clock bound lost")
			}
		})
	}
	for _, tc := range []struct {
		name, key, value string
		valid            bool
	}{
		{"valid", "", "", true}, {"store missing", "SIGNIN_REDIS_URL", "", false}, {"wrong store role", "SIGNIN_REDIS_URL", "redis://event_operator:private@localhost/0", false}, {"client missing", "OIDC_CLIENT_ID", "", false}, {"partial", "OIDC_TOKEN_URL", "", false}, {"provider query", "OIDC_AUTHORIZATION_URL", "https://id.example/auth?client_id=other", false}, {"external plaintext", "OIDC_TOKEN_URL", "http://id.example/token", false}, {"redirect credentials", "OIDC_REDIRECT_URI", "https://user:private@web.example/auth/callback", false}, {"redirect query", "OIDC_REDIRECT_URI", "https://web.example/auth/callback?next=other", false}, {"redirect otherpath", "OIDC_REDIRECT_URI", "https://web.example/elsewhere", false}, {"origin mismatch", "CORS_ORIGINS", "https://other.example", false}, {"offline scope", "OIDC_SCOPES", "openid offline_access", false}, {"no openid", "OIDC_SCOPES", "profile", false}, {"duplicate scope", "OIDC_SCOPES", "openid openid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]string{}
			for k, v := range base {
				m[k] = v
			}
			for k, v := range map[string]string{"SIGNIN_REDIS_URL": "redis://framework_signin:private@localhost/0", "OIDC_CLIENT_ID": "web-client", "OIDC_AUTHORIZATION_URL": "https://id.example/auth", "OIDC_TOKEN_URL": "https://id.example/token", "OIDC_REDIRECT_URI": "https://web.example/auth/callback", "CORS_ORIGINS": "https://web.example"} {
				m[k] = v
			}
			if tc.key != "" {
				m[tc.key] = tc.value
			}
			c, e := Load(func(k string) string { return m[k] })
			if (e == nil) != tc.valid {
				t.Fatal("provider configuration acceptance")
			}
			if e == nil && (c.OIDC == nil || len(c.OIDC.Scopes) != 1 || c.OIDC.Scopes[0] != "openid") {
				t.Fatal("provider shape")
			}
		})
	}
}
