package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

// newAI builds the AI builder from AI_PROVIDER_FILE (ADR040 A11): the single place
// that turns operator configuration into a provider. With no file the builder stays
// unconfigured: every AI route answers 503 ai_unavailable, the readiness probe is
// trivially ok and nothing else changes. A configured but invalid file stops startup
// like a bad tool catalog. The scripted provider exists for development and tests only
// and is refused outside them. Nothing about the endpoint, the credential file or the
// model is logged beyond the model label and the file's hash.
func newAI(c config.Config, registry *metadata.Registry, db *sql.DB, logger *slog.Logger) (access.AIConfig, httpserver.Probe, error) {
	if c.AIProviderFile == "" {
		return access.AIConfig{}, func(context.Context) error { return nil }, nil
	}
	cfg, hash, err := ai.LoadProviderConfig(c.AIProviderFile)
	if err != nil {
		return access.AIConfig{}, nil, errors.New("AI provider configuration invalid")
	}
	if cfg.Kind == ai.KindScripted && c.Environment != "development" && c.Environment != "test" {
		return access.AIConfig{}, nil, errors.New("the scripted AI provider is restricted to development/test")
	}
	provider, err := ai.NewProvider(cfg)
	if err != nil {
		return access.AIConfig{}, nil, errors.New("AI provider unavailable")
	}
	service := ai.NewService(ai.NewGateway(provider, ai.Options{MaxAttempts: cfg.MaxAttempts}), registry, ai.LimitsOf(cfg))
	logger.Info("AI builder enabled", "kind", cfg.Kind, "model", cfg.Model, "config_sha256", hash, "mode", "preview")
	return access.AIConfig{Service: service}, func(ctx context.Context) error { return (ai.Store{}).Ready(ctx, db) }, nil
}
