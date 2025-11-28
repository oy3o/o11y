package o11y

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	mt "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// setupMetrics initializes and configures the global MeterProvider based on the MetricConfig.
// It sets up the appropriate metric reader (e.g., Prometheus) and makes the provider
// available globally for the application to create and record metrics.
// It returns the configured provider and its corresponding shutdown function.
func setupMetrics(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc, error) {
	// 1. Handle the Enabled switch. If disabled, install a no-op provider and return.
	if !cfg.Enabled {
		// A MeterProvider with no reader will effectively discard all metrics.
		mp := mt.NewMeterProvider(mt.WithResource(res))
		otel.SetMeterProvider(mp)
		// Return a no-op shutdown function.
		return mp, func(context.Context) error { return nil }, nil
	}

	// 2. Create the appropriate metric reader based on the configuration.
	// The reader is the component that collects metrics and makes them available to an exporter.
	var reader mt.Reader
	var err error
	var serverShutdown ShutdownFunc = func(ctx context.Context) error { return nil }

	switch cfg.Exporter {
	case "prometheus":
		// This exporter makes metrics available on an HTTP endpoint for a Prometheus server to scrape.
		log.Info().Msg("Initializing Prometheus metrics exporter.")

		// prometheus.New() creates a reader that collects metrics and serves them via the promhttp.Handler.
		reader, err = prometheus.New()
		if err == nil {
			// If the reader is created successfully, we must expose the HTTP endpoint.
			// This is done in a separate goroutine to prevent blocking the main application startup.
			serverShutdown = servePrometheusMetrics(cfg)
		}

	default: // "none" or any other value
		// A ManualReader is used when we want to enable the metrics API but not export the data.
		// It requires manual collection, which we won't do, so it effectively discards metrics.
		log.Info().Msg("Initializing no-op metrics exporter.")
		reader = mt.NewManualReader()
	}

	if err != nil {
		return nil, nil, fmt.Errorf("failed to create metric reader for exporter %s: %w", cfg.Exporter, err)
	}

	// 3. Create the MeterProvider.
	// It is configured with the shared resource and the selected reader.
	mp := mt.NewMeterProvider(
		mt.WithResource(res),
		mt.WithReader(reader),
	)

	// 4. Set the global MeterProvider.
	// This makes it accessible throughout the application via otel.GetMeterProvider().
	otel.SetMeterProvider(mp)

	// 5. Return the provider and its shutdown function.
	return mp, func(ctx context.Context) error {
		err1 := mp.Shutdown(ctx)
		err2 := serverShutdown(ctx)
		if err1 != nil {
			return err1
		}
		return err2
	}, nil
}

// servePrometheusMetrics starts a dedicated HTTP server to expose the /metrics endpoint.
func servePrometheusMetrics(cfg MetricConfig) ShutdownFunc {
	// Use a new ServeMux to avoid interfering with the main application's router
	// if it also uses the default ServeMux.
	mux := http.NewServeMux()
	mux.Handle(cfg.PrometheusPath, promhttp.Handler())

	server := &http.Server{
		Addr:    cfg.PrometheusAddr,
		Handler: mux,
	}

	log.Info().Str("path", cfg.PrometheusPath).Str("addr", cfg.PrometheusAddr).Msg("Prometheus metrics server starting.")

	// Start the server.
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Prometheus metrics server failed.")
		}
	}()

	return server.Shutdown
}
