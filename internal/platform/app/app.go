// Package app is the composition root for API and worker entrypoints.
package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/observability"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/cache"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func Run(ctx context.Context, service string, c config.Config, logger *slog.Logger) error {
	telemetry, err := observability.Start(ctx, service, c.Environment, c.OTelEndpoint, logger)
	if err != nil {
		return errors.New("telemetry initialization failed")
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		telemetry.Shutdown(shutdown)
	}()
	databaseURL := c.DatabaseURL
	if service == "framework-worker" {
		if c.WorkerDatabaseURL == "" {
			return errors.New("dedicated worker database credential required")
		}
		databaseURL = c.WorkerDatabaseURL
	}
	db, err := postgres.Open(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	redis, err := cache.Open(c.RedisURL)
	if err != nil {
		return err
	}
	defer redis.Close()
	if service == "framework-worker" {
		probe, cancel := context.WithTimeout(ctx, c.DependencyTimeout)
		defer cancel()
		if jobs.CheckRole(probe, db, "framework_worker") != nil || redis.Ping(probe) != nil {
			return errors.New("worker dependencies unavailable")
		}
		logger.Info("worker started", "service", service, "environment", c.Environment)
		// Intentionally idle in M0. No workflows, queue, business state or jobs.
		<-ctx.Done()
		logger.Info("worker stopped")
		return nil
	}
	keyFile, err := os.Open(c.AuthJWKSFile)
	if err != nil {
		return errors.New("authentication key set unavailable")
	}
	keys, err := io.ReadAll(io.LimitReader(keyFile, 65537))
	_ = keyFile.Close()
	if err != nil {
		return errors.New("authentication key set unavailable")
	}
	auth, err := identity.NewVerifier(c.AuthIssuer, c.AuthAudience, keys)
	if err != nil {
		return err
	}
	registry, err := metadata.NewRegistry()
	if err != nil {
		return errors.New("metadata schema initialization failed")
	}
	accessAPI := access.API{DB: db, Auth: auth, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: registry, PreviewEnabled: c.Environment == "development" || c.Environment == "test"}
	identityProbe := func(ctx context.Context) error {
		return (identity.Store{}).Ready(ctx, db)
	}
	handler := httpserver.Handler(httpserver.Options{Logger: logger, Origins: c.CORSOrigins, Register: accessAPI.Register, AllowPOST: accessAPI.AcceptsPOST, ProbeTimeout: c.DependencyTimeout, Probes: map[string]httpserver.Probe{"postgres": func(ctx context.Context) error { return postgres.Ping(ctx, db) }, "redis": redis.Ping, "identity": identityProbe, "tenant": func(ctx context.Context) error { return (tenant.Store{}).Ready(ctx, db) }, "audit": func(ctx context.Context) error { return (audit.Postgres{}).Ready(ctx, db) }, "data": func(ctx context.Context) error { return (data.Store{}).Ready(ctx, db) }, "metadata": func(ctx context.Context) error { return registry.Ready(ctx, db) }}})
	server := httpserver.New(c.HTTPAddr, handler)
	listener, err := net.Listen("tcp", c.HTTPAddr)
	if err != nil {
		return errors.New("HTTP listener could not start")
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	logger.Info("API started", "service", service, "environment", c.Environment, "address", listener.Addr().String())
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server stopped unexpectedly")
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), c.ShutdownTimeout)
		defer cancel()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
			return errors.New("HTTP graceful shutdown timed out")
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP shutdown failed")
		}
	}
	logger.Info("API stopped")
	return nil
}

func Main(ctx context.Context, service string) int {
	c, err := config.Load(os.Getenv)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogLevel})).With("service", service)
	if err != nil {
		logger.Error("configuration invalid", "error", err.Error())
		return 1
	}
	if Run(ctx, service, c, logger) != nil {
		logger.Error("service failed; inspect configuration and dependency status")
		return 1
	}
	return 0
}
