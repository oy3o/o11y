package o11y

import (
	"context"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tc "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// setupTracing initializes and configures the global TracerProvider based on the TraceConfig.
// It determines which exporter to use, sets the sampling rate, and combines everything
// into a TracerProvider that is then set as the global default for the application.
// It returns the configured provider and its corresponding shutdown function.
func setupTracing(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc) {
	// 1. Handle the Enabled switch. If disabled, install a no-op provider and return.
	if !cfg.Enabled {
		tp := tc.NewTracerProvider(tc.WithResource(res))
		otel.SetTracerProvider(tp)
		// Return a no-op shutdown function.
		return tp, func(context.Context) error { return nil }
	}

	// 2. Create the appropriate SpanExporter based on the configuration.
	var exporter tc.SpanExporter
	var err error

	switch cfg.Exporter {
	case "otlp-grpc":
		log.Info().Msgf("Initializing OTLP gRPC trace exporter with endpoint: %s", cfg.Endpoint)

		// Prepare gRPC options based on config.
		grpcOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.OtlpInsecure {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
			log.Warn().Msg("OTLP trace exporter is using an insecure gRPC connection.")
		}

		exporter, err = otlptracegrpc.New(
			context.Background(),
			grpcOpts...,
		)
	case "stdout":
		// This exporter prints traces to the standard output. It's very useful for local debugging.
		log.Info().Msg("Initializing stdout trace exporter.")
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	default: // "none" or any other value
		// This exporter discards all traces. It's useful for enabling the tracing API
		// for testing purposes without actually exporting any data.
		log.Info().Msg("Initializing no-op trace exporter.")
		exporter = tracetest.NewNoopExporter()
	}

	if err != nil {
		// A failure to create an exporter is a critical configuration error.
		log.Fatal().Err(err).Msgf("Failed to create trace exporter: %s", cfg.Exporter)
	}

	// 3. Configure the sampler based on the specified ratio.
	// The sampler decides whether a trace should be recorded and exported.
	var sampler tc.Sampler
	if cfg.SampleRatio >= 1.0 {
		sampler = tc.AlwaysSample()
		log.Info().Msg("Trace sampling is enabled for all traces (SampleRatio >= 1.0).")
	} else if cfg.SampleRatio <= 0.0 {
		sampler = tc.NeverSample()
		log.Info().Msg("Trace sampling is disabled for all traces (SampleRatio <= 0.0).")
	} else {
		sampler = tc.TraceIDRatioBased(cfg.SampleRatio)
		log.Info().Msgf("Trace sampling is configured with a %.2f ratio.", cfg.SampleRatio)
	}

	// 4. Create the TracerProvider.
	// This is the core of the tracing SDK, which wires together the exporter, sampler, and resource.
	// We use a BatchSpanProcessor for performance, as it batches spans before sending them to the exporter.
	tp := tc.NewTracerProvider(
		tc.WithBatcher(exporter),
		tc.WithResource(res),
		tc.WithSampler(sampler),
	)

	// 5. Set the global TracerProvider.
	// This makes the configured provider available to the entire application via otel.GetTracerProvider().
	otel.SetTracerProvider(tp)

	// 6. Set the global TextMapPropagator.
	// This is crucial for distributed tracing. It enables the automatic injection and extraction
	// of Trace Context (TraceID, SpanID) and Baggage via HTTP/gRPC headers.
	// Without this, traces will be broken when crossing service boundaries.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 7. Return the provider and its shutdown function.
	// The shutdown function ensures that the batch processor is flushed before the application exits.
	return tp, tp.Shutdown
}
