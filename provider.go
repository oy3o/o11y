package o11y

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type Provider struct {
	Tracer trace.Tracer
	Meter  metric.Meter
	Logger zerolog.Logger

	shutdownFunc ShutdownFunc
}

func New(cfg Config,
	setupLogging func(cfg LogConfig) (zerolog.Logger, ShutdownFunc),
	setupTracing func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc, error),
	setupMetrics func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc, error),
) (*Provider, error) {
	// 1. Defaults
	if cfg.InstrumentationScope == "" {
		cfg.InstrumentationScope = "o11y"
	}
	if cfg.Metric.PrometheusAddr == "" {
		cfg.Metric.PrometheusAddr = ":2222" // Default prometheus port
	}
	if cfg.Metric.PrometheusPath == "" {
		cfg.Metric.PrometheusPath = "/metrics"
	}

	if !cfg.Enabled {
		return &Provider{
			Tracer:       otel.GetTracerProvider().Tracer(cfg.InstrumentationScope), // No-op
			Meter:        otel.GetMeterProvider().Meter(cfg.InstrumentationScope),   // No-op
			Logger:       zerolog.New(io.Discard),
			shutdownFunc: func(context.Context) error { return nil },
		}, nil
	}

	// 2. Resource
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.Service),
			semconv.ServiceVersion(cfg.Version),
			semconv.DeploymentEnvironmentName(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenTelemetry resource: %w", err)
	}

	// 3. Components Initialization
	// We must ensure proper cleanup if any step fails.

	// 3.1 Logging
	logger, logShutdown := setupLogging(cfg.Log)
	log := logger.With().
		Timestamp().
		Str("service", cfg.Service).
		Str("version", cfg.Version).
		Str("environment", cfg.Environment).
		Logger().
		Hook(PanicHook(cfg.Log.StackFilters))
	log.Info().Msg("Logging initialized.")

	// 3.2 Tracing
	tp, traceShutdown, err := setupTracing(cfg.Trace, res)
	if err != nil {
		// Rollback Logging
		logShutdown(context.Background())
		return nil, err
	}
	log.Info().Msg("Tracing initialized.")

	// 3.3 Metrics
	mp, metricShutdown, err := setupMetrics(cfg.Metric, res)
	if err != nil {
		// Rollback Tracing and Logging
		traceShutdown(context.Background())
		logShutdown(context.Background())
		return nil, err
	}
	log.Info().Msg("Metrics initialized.")

	// 4. Aggregate Shutdown
	shutdown := func(ctx context.Context) error {
		log.Info().Msg("Shutting down o11y components...")

		var g errgroup.Group

		// Shutdown Metrics (e.g. stop HTTP server)
		g.Go(func() error {
			log.Debug().Msg("Shutting down metrics provider...")
			if err := metricShutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to shutdown metrics provider")
				return err
			}
			return nil
		})

		// Shutdown Tracing (flush spans)
		g.Go(func() error {
			log.Debug().Msg("Shutting down tracer provider...")
			if err := traceShutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to shutdown tracer provider")
				return err
			}
			return nil
		})

		// Wait for metrics and tracing to close
		shutdownErr := g.Wait()

		// Shutdown Logging last
		if err := logShutdown(ctx); err != nil {
			fmt.Printf("error: failed to shutdown logger: %v\n", err)
			if shutdownErr != nil {
				shutdownErr = fmt.Errorf("multiple shutdown errors: %w; log shutdown error: %v", shutdownErr, err)
			} else {
				shutdownErr = err
			}
		}

		if shutdownErr == nil {
			log.Info().Msg("o11y shutdown complete.")
		}

		return shutdownErr
	}

	return &Provider{
		Tracer:       tp.Tracer(cfg.InstrumentationScope),
		Meter:        mp.Meter(cfg.InstrumentationScope),
		Logger:       log,
		shutdownFunc: shutdown,
	}, nil
}

// Shutdown 关闭 Provider
func (p *Provider) Shutdown(ctx context.Context) error {
	return p.shutdownFunc(ctx)
}
