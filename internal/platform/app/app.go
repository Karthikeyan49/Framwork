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
	"sync"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/observability"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/cache"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/platform/signin"
	"github.com/Karthikeyan49/Framwork/internal/platform/traffic"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
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
	if service == "framework-dispatcher" {
		if c.DispatcherDatabaseURL == "" {
			return errors.New("dedicated dispatcher database credential required")
		}
		databaseURL = c.DispatcherDatabaseURL
	}
	db, err := postgres.Open(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if service == "framework-worker" || service == "framework-dispatcher" {
		role := "framework_worker"
		if service == "framework-dispatcher" {
			role = "framework_dispatcher"
		}
		probe, cancel := context.WithTimeout(ctx, c.DependencyTimeout)
		err := jobs.CheckRole(probe, db, role)
		cancel()
		if err != nil {
			return errors.New("event role unavailable")
		}
		if !c.EventsEnabled && !c.WorkflowsEnabled {
			logger.Info("event delivery disabled", "service", service)
			<-ctx.Done()
			return nil
		}
		var wg sync.WaitGroup
		jobCtx, stopJobs := context.WithCancel(ctx)
		defer func() { stopJobs(); wg.Wait() }()
		if service == "framework-dispatcher" && c.WorkflowsEnabled {
			var valid bool
			if e := db.QueryRowContext(ctx, `SELECT has_function_privilege('control.claim_workflow(uuid)','EXECUTE') AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='runtime' AND c.relname='records' AND has_table_privilege(c.oid,'SELECT'))`).Scan(&valid); e != nil || !valid {
				return errors.New("workflow dispatch contract unavailable")
			}
			wg.Go(func() { jobs.RunWorkflowDispatcher(jobCtx, db, logger) })
		}
		if service == "framework-worker" && c.WorkflowsEnabled {
			probe, cancel = context.WithTimeout(ctx, c.DependencyTimeout)
			err = workflow.Ready(probe, db, "framework_worker")
			if err == nil {
				err = (policy.RoleStore{}).Ready(probe, db, true)
			}
			if err == nil {
				err = (audit.Postgres{}).Ready(probe, db)
			}
			cancel()
			if err != nil {
				return errors.New("workflow storage contract unavailable")
			}
			store := access.WorkerWorkflowStore(policy.Runtime{Worker: true}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
				var id string
				if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&id); e != nil {
					return identity.User{}, e
				}
				return (identity.Store{}).ResolveWorker(ctx, q, id)
			})
			logger.Info("workflow execution started", "mode", "preview")
			wg.Go(func() { (workflow.Worker{DB: db, Store: store}).Run(jobCtx, logger) })
		}
		if !c.EventsEnabled {
			wg.Wait()
			logger.Info("workflow execution stopped")
			return nil
		}
		if c.Environment == "production" || c.EventRedisURL == "" {
			return errors.New("preview event delivery configuration unavailable")
		}
		if err := c.EventBinding(service); err != nil {
			return err
		}
		probe, cancel = context.WithTimeout(ctx, c.DependencyTimeout)
		err = events.Ready(probe, db, role)
		if err == nil && service == "framework-worker" {
			err = (policy.RoleStore{}).Ready(probe, db, true)
		}
		if err == nil && service == "framework-worker" {
			err = (audit.Postgres{}).Ready(probe, db)
		}
		cancel()
		if err != nil {
			return errors.New("event storage contract unavailable")
		}
		var principal string
		if err = db.QueryRowContext(ctx, `SELECT session_user`).Scan(&principal); err != nil {
			return errors.New("event audience unavailable")
		}
		broker, err := events.OpenRedis(c.EventRedisURL, principal)
		if err != nil {
			return err
		}
		defer broker.Close()
		probe, cancel = context.WithTimeout(ctx, c.DependencyTimeout)
		err = broker.Ping(probe)
		cancel()
		if err != nil {
			return errors.New("event transport unavailable")
		}
		logger.Info("event delivery started", "service", service, "mode", "preview")
		if service == "framework-dispatcher" {
			events.RunDispatcher(ctx, events.Dispatcher{DB: db, Transport: broker}, logger)
		} else {
			events.RunConsumer(ctx, events.Consumer{DB: db, Policy: policy.Runtime{Worker: true}, Audit: audit.Postgres{}}, broker, logger)
		}
		logger.Info("event delivery stopped", "service", service)
		wg.Wait()
		return nil
	}
	redis, err := cache.Open(c.RedisURL)
	if err != nil {
		return err
	}
	defer redis.Close()
	keyFile, err := os.Open(c.AuthJWKSFile)
	if err != nil {
		return errors.New("authentication key set unavailable")
	}
	keys, err := io.ReadAll(io.LimitReader(keyFile, 65537))
	_ = keyFile.Close()
	if err != nil {
		return errors.New("authentication key set unavailable")
	}
	auth, err := identity.NewVerifierWithClockSkew(c.AuthIssuer, c.AuthAudience, keys, c.AuthClockSkew)
	if err != nil {
		return err
	}
	registry, err := metadata.NewRegistry()
	if err != nil {
		return errors.New("metadata schema initialization failed")
	}
	concurrency := c.APIConcurrency
	if concurrency == 0 {
		concurrency = 4 // Preserve programmatic zero-valued configuration.
	}
	if concurrency < 1 || concurrency > 10 {
		return errors.New("API database concurrency is outside supported bounds")
	}
	admission, err := postgres.NewAdmission(concurrency, 128)
	if err != nil {
		return err
	}
	requestLimits := c.APIRequestLimits
	if requestLimits == (traffic.Limits{}) {
		requestLimits = traffic.DefaultLimits()
	}
	trafficGate, err := traffic.New(requestLimits, nil)
	if err != nil {
		return err
	}
	accessAPI := access.API{DB: db, Admission: admission, Traffic: trafficGate, Auth: auth, Policy: policy.Native{}, PolicyLoader: (policy.RoleStore{}).Load, Audit: audit.Postgres{}, Registry: registry, PreviewEnabled: c.Environment == "development" || c.Environment == "test", WorkflowsEnabled: c.WorkflowsEnabled}
	signIn := signin.API{Development: c.Environment != "production"}
	var signInStore *cache.Client
	if c.OIDC != nil {
		signInStore, err = cache.Open(c.SignInRedisURL)
		if err != nil {
			return errors.New("sign-in storage unavailable")
		}
		defer signInStore.Close()
		signIn.Login, err = identity.NewBrowserLogin(*c.OIDC, auth, signInStore)
		if err != nil {
			return errors.New("sign-in configuration unavailable")
		}
		defer signIn.Login.Close()
	}
	identityProbe := func(ctx context.Context) error {
		return (identity.Store{}).Ready(ctx, db)
	}
	handler := httpserver.Handler(httpserver.Options{Logger: logger, Origins: c.CORSOrigins, Register: func(mux *http.ServeMux) { accessAPI.Register(mux); signIn.Register(mux) }, AllowPOST: func(path string) bool { return accessAPI.AcceptsPOST(path) || signIn.AcceptsPOST(path) }, ProbeTimeout: c.DependencyTimeout, Probes: map[string]httpserver.Probe{"postgres": func(ctx context.Context) error { return postgres.Ping(ctx, db) }, "redis": redis.Ping, "workflows": func(ctx context.Context) error {
		if !c.WorkflowsEnabled {
			return nil
		}
		return workflow.Ready(ctx, db, "framework_app")
	}, "signin": func(ctx context.Context) error {
		if signInStore == nil {
			return nil
		}
		return signInStore.ReadyLoginFlows(ctx)
	}, "policy": func(ctx context.Context) error { return (policy.RoleStore{}).Ready(ctx, db, false) }, "events": func(ctx context.Context) error { return events.Ready(ctx, db, "framework_app") }, "identity": identityProbe, "tenant": func(ctx context.Context) error { return (tenant.Store{}).Ready(ctx, db) }, "audit": func(ctx context.Context) error { return (audit.Postgres{}).Ready(ctx, db) }, "data": func(ctx context.Context) error { return (data.Store{}).Ready(ctx, db) }, "metadata": func(ctx context.Context) error { return registry.Ready(ctx, db) }}})
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
