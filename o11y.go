package o11y

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
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
func Init(cfg Config) (ShutdownFunc, error) {
	return initialization(cfg, setupLogging, setupTracing, setupMetrics)
}

func initialization(
	cfg Config,
	setupLogging func(cfg LogConfig) (zerolog.Logger, ShutdownFunc),
	setupTracing func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc, error),
	setupMetrics func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc, error),
) (ShutdownFunc, error) {
	// Initialize package-level tracer and meter for the library to use.
	p, err := New(cfg, setupLogging, setupTracing, setupMetrics)
	if err != nil {
		return nil, err
	}

	Tracer = p.Tracer
	Meter = p.Meter
	log.Logger = p.Logger

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

	return p.Shutdown, nil
}
