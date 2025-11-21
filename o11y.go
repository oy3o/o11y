package o11y

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

// ShutdownFunc is a function signature for gracefully shutting down an observability component.
// Applications should use `defer` to call the shutdown function returned by Init before main exits.
type ShutdownFunc func(ctx context.Context) error

var (
	// Tracer is the application-wide tracer, initialized by Init.
	Tracer trace.Tracer
	// Meter is the application-wide meter, initialized by Init.
	Meter metric.Meter
)

// GetTraceID extracts the TraceID of the OpenTelemetry from the Context.
// If there is no valid Span in the current Context, it returns an empty string.
func GetTraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// Init initializes all observability components (logging, tracing, metrics) based on the provided configuration.
// It is the primary entry point for the o11y library.
// It will panic on critical setup failures.
// It returns a single aggregate ShutdownFunc that must be called to ensure all components are closed gracefully.
func Init(cfg Config) ShutdownFunc {
	return initialization(cfg, setupLogging, setupTracing, setupMetrics)
}

func initialization(
	cfg Config,
	setupLogging func(cfg LogConfig) (zerolog.Logger, ShutdownFunc),
	setupTracing func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc),
	setupMetrics func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc),
) ShutdownFunc {
	// 1. Set sensible defaults for any unset configuration values.
	if cfg.InstrumentationScope == "" {
		cfg.InstrumentationScope = "o11y" // Default scope name
	}
	if cfg.Metric.PrometheusAddr == "" {
		cfg.Metric.PrometheusAddr = ":2222" // Default prometheus port
	}
	if cfg.Metric.PrometheusPath == "" {
		cfg.Metric.PrometheusPath = "/metrics"
	}

	// 2. Handle the global enabled switch.
	if !cfg.Enabled {
		log.Logger = zerolog.New(io.Discard)
		return func(ctx context.Context) error { return nil }
	}

	// 3. Create a shared OpenTelemetry Resource.
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
		panic(fmt.Errorf("failed to create OpenTelemetry resource: %w", err))
	}

	// 4. Set up logging.
	logger, logShutdown := setupLogging(cfg.Log)
	finalLogger := logger.With().
		Timestamp().
		Str("service", cfg.Service).
		Str("version", cfg.Version).
		Str("environment", cfg.Environment).
		Logger().
		Hook(PanicHook(cfg.Log.StackFilters))

	log.Logger = finalLogger
	log.Info().Msg("Logging initialized.")

	// 5. Set up tracing.
	_, traceShutdown := setupTracing(cfg.Trace, res)
	log.Info().Msg("Tracing initialized.")

	// 6. Set up metrics.
	mp, metricShutdown := setupMetrics(cfg.Metric, res)
	log.Info().Msg("Metrics initialized.")

	// 7. Initialize package-level tracer and meter for the library to use.
	Tracer = otel.Tracer(cfg.InstrumentationScope)
	Meter = mp.Meter(cfg.InstrumentationScope)

	if cfg.Metric.Enabled {
		// Initialize our pre-defined, standard metrics.
		InitStandardMetrics(Meter)

		// Start collecting Go runtime metrics.
		if err := StartRuntimeMetrics(); err != nil {
			log.Warn().Err(err).Msg("Could not start runtime metrics collection, but continuing initialization.")
		}

		// Start collecting host metrics if enabled.
		if cfg.Metric.EnableHostMetrics {
			if err := StartHostMetrics(); err != nil {
				log.Warn().Err(err).Msg("Could not start host metrics collection, but continuing initialization.")
			}
		}
	} else {
		log.Info().Msg("Metrics disabled by config, skipping standard and runtime metric initialization.")
	}

	// 8. Aggregate all shutdown functions.
	return func(ctx context.Context) error {
		log.Info().Msg("Shutting down o11y components...")

		var g errgroup.Group

		g.Go(func() error {
			log.Debug().Msg("Shutting down metrics provider...")
			if err := metricShutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to shutdown metrics provider")
				return err
			}
			return nil
		})

		g.Go(func() error {
			log.Debug().Msg("Shutting down tracer provider...")
			if err := traceShutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to shutdown tracer provider")
				return err
			}
			return nil
		})

		shutdownErr := g.Wait()

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
}
