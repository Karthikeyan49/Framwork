// Package observability initializes process-local telemetry; it does not gate readiness.
package observability

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Providers struct {
	Traces  *trace.TracerProvider
	Metrics *metric.MeterProvider
	logger  *slog.Logger
}

func Start(ctx context.Context, service, environment, endpoint string, logger *slog.Logger) (*Providers, error) {
	res := resource.NewSchemaless(semconv.ServiceName(service), semconv.DeploymentEnvironmentName(environment))
	traces := []trace.TracerProviderOption{trace.WithResource(res), trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(0.1)))}
	metrics := []metric.Option{metric.WithResource(res)}
	if endpoint != "" {
		// Explicit signal URLs fix scheme/path and override INSECURE environment
		// defaults. HTTPS configuration must never silently export over plaintext.
		tOpts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint + "/v1/traces"), otlptracehttp.WithTimeout(time.Second)}
		mOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(endpoint + "/v1/metrics"), otlpmetrichttp.WithTimeout(time.Second)}
		exporter, err := otlptracehttp.New(ctx, tOpts...)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace.WithBatcher(exporter, trace.WithExportTimeout(time.Second)))
		mExporter, err := otlpmetrichttp.New(ctx, mOpts...)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric.WithReader(metric.NewPeriodicReader(mExporter, metric.WithInterval(10*time.Second), metric.WithTimeout(time.Second))))
	}
	providers := &Providers{Traces: trace.NewTracerProvider(traces...), Metrics: metric.NewMeterProvider(metrics...), logger: logger}
	otel.SetTracerProvider(providers.Traces)
	otel.SetMeterProvider(providers.Metrics)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	// Exporter errors are redacted; transport errors may contain credential-bearing URLs.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { logger.Warn("telemetry export unavailable") }))
	if err := runtime.Start(runtime.WithMeterProvider(providers.Metrics)); err != nil {
		logger.Warn("runtime metrics unavailable")
	}
	return providers, nil
}

func (p *Providers) Shutdown(ctx context.Context) {
	if p.Traces.Shutdown(ctx) != nil {
		p.logger.Warn("trace shutdown incomplete")
	}
	if p.Metrics.Shutdown(ctx) != nil {
		p.logger.Warn("metric shutdown incomplete")
	}
}
