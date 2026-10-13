// Package config validates process configuration without logging credential values.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

var brokerUserPattern = regexp.MustCompile(`^event_[a-z][a-z0-9_]{0,62}$`)

type Config struct {
	Environment           string
	HTTPAddr              string
	DatabaseURL           string
	WorkerDatabaseURL     string
	DispatcherDatabaseURL string
	EventsEnabled         bool
	WorkflowsEnabled      bool
	EventRedisURL         string
	RedisURL              string
	CORSOrigins           []string
	OTelEndpoint          string
	LogLevel              slog.Level
	DependencyTimeout     time.Duration
	ShutdownTimeout       time.Duration
	AuthIssuer            string
	AuthAudience          string
	AuthJWKSFile          string
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{Environment: value(getenv, "APP_ENV", "development"), HTTPAddr: value(getenv, "HTTP_ADDR", "127.0.0.1:8080"), DatabaseURL: getenv("DATABASE_URL"), WorkerDatabaseURL: getenv("WORKER_DATABASE_URL"), RedisURL: getenv("REDIS_URL"), OTelEndpoint: getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), DependencyTimeout: 2 * time.Second, ShutdownTimeout: 10 * time.Second}
	c.DispatcherDatabaseURL = getenv("DISPATCHER_DATABASE_URL")
	c.EventRedisURL = getenv("EVENT_REDIS_URL")
	switch getenv("WORKFLOWS_ENABLED") {
	case "":
		c.WorkflowsEnabled = c.Environment == "development" || c.Environment == "test"
	case "false":
	case "true":
		c.WorkflowsEnabled = true
	default:
		return c, fmt.Errorf("WORKFLOWS_ENABLED must be true or false")
	}
	if c.WorkflowsEnabled && c.Environment == "production" {
		return c, fmt.Errorf("M9 preview workflows require development/test; production requires approved release activation")
	}
	switch getenv("EVENTS_ENABLED") {
	case "", "false":
	case "true":
		c.EventsEnabled = true
	default:
		return c, fmt.Errorf("EVENTS_ENABLED must be true or false")
	}
	if c.EventsEnabled && c.Environment == "production" {
		return c, fmt.Errorf("M8 preview event delivery requires development/test; production consumers require release activation")
	}
	if c.EventRedisURL != "" {
		options, err := redis.ParseURL(c.EventRedisURL)
		u, urlErr := url.Parse(c.EventRedisURL)
		if err != nil || urlErr != nil || u.Hostname() == "" || u.User == nil || u.RawQuery != "" || u.Fragment != "" || options.Password == "" || !brokerUserPattern.MatchString(options.Username) || options.Username == "event_operator" || (u.Scheme != "redis" && u.Scheme != "rediss") {
			return c, fmt.Errorf("EVENT_REDIS_URL requires a dedicated unambiguous sender/receiver binding")
		}
		if c.Environment == "production" && (options.TLSConfig == nil || options.TLSConfig.InsecureSkipVerify) {
			return c, fmt.Errorf("EVENT_REDIS_URL requires verified TLS in production")
		}
	}
	switch c.Environment {
	case "development", "test", "production":
	default:
		return c, fmt.Errorf("APP_ENV must be development, test, or production")
	}
	c.AuthIssuer = getenv("AUTH_ISSUER")
	c.AuthAudience = getenv("AUTH_AUDIENCE")
	c.AuthJWKSFile = getenv("AUTH_JWKS_FILE")
	if c.Environment != "production" && c.AuthIssuer == "" && c.AuthAudience == "" && c.AuthJWKSFile == "" {
		c.AuthIssuer = "http://localhost:8080/dev-issuer"
		c.AuthAudience = "framework-api"
		c.AuthJWKSFile = ".local/identity/jwks.json"
	}
	issuer, authErr := url.Parse(c.AuthIssuer)
	if authErr != nil || issuer.Hostname() == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || (issuer.Scheme != "http" && issuer.Scheme != "https") || c.AuthAudience == "" || len(c.AuthAudience) > 200 || c.AuthJWKSFile == "" {
		return c, fmt.Errorf("AUTH_ISSUER, AUTH_AUDIENCE and AUTH_JWKS_FILE require a valid explicit authentication configuration")
	}
	if c.Environment == "production" && (issuer.Scheme != "https" || c.AuthIssuer == "http://localhost:8080/dev-issuer" || strings.Contains(c.AuthJWKSFile, ".local/identity")) {
		return c, fmt.Errorf("production requires an HTTPS issuer and an operator-managed public key set")
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return c, fmt.Errorf("HTTP_ADDR must be host:port")
	}
	if err := c.LogLevel.UnmarshalText([]byte(value(getenv, "LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("LOG_LEVEL is invalid")
	}
	connections := []struct {
		name, raw string
		schemes   []string
	}{{"DATABASE_URL", c.DatabaseURL, []string{"postgres", "postgresql"}}, {"REDIS_URL", c.RedisURL, []string{"redis", "rediss"}}}
	if c.WorkerDatabaseURL != "" {
		connections = append(connections, struct {
			name, raw string
			schemes   []string
		}{"WORKER_DATABASE_URL", c.WorkerDatabaseURL, []string{"postgres", "postgresql"}})
	}
	if raw := getenv("DISPATCHER_DATABASE_URL"); raw != "" {
		connections = append(connections, struct {
			name, raw string
			schemes   []string
		}{"DISPATCHER_DATABASE_URL", raw, []string{"postgres", "postgresql"}})
	}
	for _, entry := range connections {
		u, err := url.Parse(entry.raw)
		if err != nil || u.Hostname() == "" || !contains(entry.schemes, u.Scheme) {
			return c, fmt.Errorf("%s must be a valid connection URL", entry.name)
		}
		if c.Environment == "production" {
			if u.User == nil {
				return c, fmt.Errorf("%s requires credentials in production", entry.name)
			}
			pass, ok := u.User.Password()
			if !ok || strings.TrimSpace(pass) == "" {
				return c, fmt.Errorf("%s requires credentials in production", entry.name)
			}
			if entry.name != "REDIS_URL" {
				if err := verifyPostgres(entry.raw, u); err != nil {
					return c, fmt.Errorf("DATABASE_URL %s", err)
				}
			}
			if entry.name == "REDIS_URL" {
				options, err := redis.ParseURL(entry.raw)
				if err != nil || u.Scheme != "rediss" || options.TLSConfig == nil || options.TLSConfig.InsecureSkipVerify || options.Password != pass {
					return c, fmt.Errorf("REDIS_URL requires verified TLS and unambiguous credentials in production")
				}
			}
		}
	}
	for _, raw := range strings.Split(getenv("CORS_ORIGINS"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || strings.Contains(raw, "*") {
			return c, fmt.Errorf("CORS_ORIGINS must contain exact origins")
		}
		if c.Environment == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("production CORS origins require HTTPS")
		}
		c.CORSOrigins = append(c.CORSOrigins, raw)
	}
	if c.OTelEndpoint != "" {
		u, err := url.Parse(c.OTelEndpoint)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be an HTTP(S) origin")
		}
		if c.Environment == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("production telemetry requires HTTPS")
		}
	}
	return c, nil
}

func value(getenv func(string) string, key, fallback string) string {
	if s := getenv(key); s != "" {
		return s
	}
	return fallback
}

// EventBinding verifies a process-specific sender/receiver identity before it
// can consume delivery attempts. A successful Redis PING is not that proof.
func (c Config) EventBinding(service string) error {
	u, err := url.Parse(c.EventRedisURL)
	expected := "event_worker"
	if service == "framework-dispatcher" {
		expected = "event_dispatcher"
	} else if service != "framework-worker" {
		return fmt.Errorf("unknown event process")
	} else if c.WorkerDatabaseURL != "" {
		binding, err := pgx.ParseConfig(c.WorkerDatabaseURL)
		if err != nil {
			return fmt.Errorf("worker audience binding required")
		}
		if contains([]string{"operator", "dispatcher", "worker"}, binding.User) {
			return fmt.Errorf("reserved worker audience name")
		}
		if binding.User != "framework_worker" {
			expected = "event_" + binding.User
		}
	}
	if err != nil || u.User == nil || u.User.Username() != expected {
		return fmt.Errorf("dedicated event process binding required")
	}
	return nil
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// MigrationURL uses a separate credential binding and the same production trust
// rules as runtime connections. Migration privileges are never a runtime fallback.
func MigrationURL(getenv func(string) string) (string, error) {
	raw := getenv("MIGRATION_DATABASE_URL")
	if raw == "" {
		return "", fmt.Errorf("MIGRATION_DATABASE_URL is required; never use the app role for migrations")
	}
	environment := value(getenv, "APP_ENV", "development")
	if !contains([]string{"development", "test", "production"}, environment) {
		return "", fmt.Errorf("APP_ENV must be development, test, or production")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || !contains([]string{"postgres", "postgresql"}, u.Scheme) {
		return "", fmt.Errorf("MIGRATION_DATABASE_URL must be a valid PostgreSQL URL")
	}
	if environment == "production" {
		if u.User == nil {
			return "", fmt.Errorf("MIGRATION_DATABASE_URL requires credentials in production")
		}
		password, ok := u.User.Password()
		if !ok || strings.TrimSpace(password) == "" {
			return "", fmt.Errorf("MIGRATION_DATABASE_URL requires credentials in production")
		}
		if err := verifyPostgres(raw, u); err != nil {
			return "", fmt.Errorf("MIGRATION_DATABASE_URL %s", err)
		}
	}
	return raw, nil
}

// Check the actual driver configuration, not only net/url's first query value.
// pgx uses last-value/alias semantics and may inherit libpq environment settings.
func verifyPostgres(raw string, u *url.URL) error {
	failure := fmt.Errorf("requires sslmode=verify-full, verified TLS, and unambiguous connection options in production")
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || query.Get("sslmode") != "verify-full" {
		return failure
	}
	for _, values := range query {
		if len(values) != 1 {
			return failure
		}
	}
	for _, key := range []string{"ssl", "host", "hostaddr", "port", "user", "password", "dbname", "database", "service", "servicefile", "passfile"} {
		if _, exists := query[key]; exists {
			return failure
		}
	}
	parsed, err := pgx.ParseConfig(raw)
	if err != nil || parsed.TLSConfig == nil || parsed.TLSConfig.InsecureSkipVerify || parsed.TLSConfig.ServerName != parsed.Host {
		return failure
	}
	password, _ := u.User.Password()
	if parsed.Host != u.Hostname() || parsed.User != u.User.Username() || parsed.Password != password {
		return failure
	}
	for _, fallback := range parsed.Fallbacks {
		if fallback.TLSConfig == nil || fallback.TLSConfig.InsecureSkipVerify || fallback.TLSConfig.ServerName != fallback.Host {
			return failure
		}
	}
	return nil
}
