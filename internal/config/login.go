package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/redis/go-redis/v9"
)

func loadLogin(getenv func(string) string, c *Config) error {
	c.SignInRedisURL = getenv("SIGNIN_REDIS_URL")
	if c.SignInRedisURL != "" {
		u, err := url.Parse(c.SignInRedisURL)
		options, parseErr := redis.ParseURL(c.SignInRedisURL)
		if err != nil || parseErr != nil || u.Hostname() == "" || u.User == nil || u.User.Username() != "framework_signin" || options.Password == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/0") || (u.Scheme != "redis" && u.Scheme != "rediss") {
			return fmt.Errorf("SIGNIN_REDIS_URL requires a dedicated sign-in binding")
		}
		if c.Environment == "production" && (options.TLSConfig == nil || options.TLSConfig.InsecureSkipVerify) {
			return fmt.Errorf("SIGNIN_REDIS_URL requires verified TLS in production")
		}
	}
	if raw := getenv("AUTH_CLOCK_SKEW"); raw != "" {
		skew, err := time.ParseDuration(raw)
		if err != nil || skew < 0 || skew > 2*time.Minute || skew%time.Second != 0 {
			return fmt.Errorf("AUTH_CLOCK_SKEW must be whole seconds from 0s to 120s")
		}
		c.AuthClockSkew = skew
	}
	p := identity.Provider{ClientID: getenv("OIDC_CLIENT_ID"), AuthorizationURL: getenv("OIDC_AUTHORIZATION_URL"), TokenURL: getenv("OIDC_TOKEN_URL"), RedirectURI: getenv("OIDC_REDIRECT_URI"), Development: c.Environment != "production"}
	scopes := getenv("OIDC_SCOPES")
	if p.ClientID == "" && p.AuthorizationURL == "" && p.TokenURL == "" && p.RedirectURI == "" && scopes == "" {
		return nil
	}
	if scopes == "" {
		scopes = "openid"
	}
	p.Scopes = strings.Fields(scopes)
	if len(scopes) > 1024 {
		return fmt.Errorf("OIDC_SCOPES exceeds its bound")
	}
	if err := identity.ValidateProvider(p); err != nil {
		return fmt.Errorf("invalid explicit OIDC provider configuration")
	}
	if c.SignInRedisURL == "" {
		return fmt.Errorf("OIDC requires SIGNIN_REDIS_URL")
	}
	// HTTP middleware must allow the same exact origin as the login gateway.
	redirect, _ := url.Parse(p.RedirectURI)
	origin := redirect.Scheme + "://" + redirect.Host
	allowed := false
	for _, candidate := range c.CORSOrigins {
		if candidate == origin {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("OIDC redirect origin must be an exact CORS_ORIGINS entry")
	}
	c.OIDC = &p
	return nil
}
