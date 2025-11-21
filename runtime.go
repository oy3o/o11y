package o11y

import (
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

// StartRuntimeMetrics initializes the collection of Go runtime metrics.
// It starts a background goroutine that periodically scrapes metrics like
// goroutine count, GC stats, and memory usage, and reports them via the
// globally configured MeterProvider.
//
// This function should be called once during application startup after the
// global MeterProvider has been configured. It is non-blocking.
func StartRuntimeMetrics() error {
	log.Info().Msg("Initializing Go runtime metrics collection.")

	// runtime.Start() is the magic function from the OpenTelemetry contrib library.
	// It handles the collection asynchronously by using the global MeterProvider.
	err := runtime.Start()
	if err != nil {
		// We log the error but don't panic, as the rest of the application
		// might still be able to function correctly.
		log.Error().Err(err).Msg("Failed to start Go runtime metrics collection.")
		return err
	}

	return nil
}

// StartHostMetrics initializes the collection of host metrics.
// It starts a background goroutine that periodically scrapes metrics like
// CPU utilization and memory usage, reporting them via the globally configured
// MeterProvider.
//
// This function should be called once during application startup. It is non-blocking.
func StartHostMetrics() error {
	log.Info().Msg("Initializing host metrics collection.")

	// host.Start() is the function from the OpenTelemetry contrib library.
	// It handles the collection asynchronously.
	err := host.Start()
	if err != nil {
		log.Error().Err(err).Msg("Failed to start host metrics collection.")
		return err
	}

	return nil
}
