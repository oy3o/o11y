package o11y

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

// initHostMetrics verifies that host metrics are initialized based on configuration.
func TestInitHostMetrics(t *testing.T) {
	tests := []struct {
		name              string
		enableHostMetrics bool
		expectLog         bool
	}{
		{
			name:              "Host metrics enabled",
			enableHostMetrics: true,
			expectLog:         true,
		},
		{
			name:              "Host metrics disabled",
			enableHostMetrics: false,
			expectLog:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuffer bytes.Buffer
			mockSetupLogging := func(cfg LogConfig) (zerolog.Logger, ShutdownFunc) {
				return zerolog.New(&logBuffer), func(ctx context.Context) error { return nil }
			}
			mockSetupTracing := func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc) {
				return nil, func(ctx context.Context) error { return nil }
			}
			mockSetupMetrics := func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc) {
				return noop.NewMeterProvider(), func(ctx context.Context) error { return nil }
			}

			cfg := Config{
				Enabled:     true,
				Service:     "test-service",
				Version:     "1.0.0",
				Environment: "test",
				Log: LogConfig{
					Level: "info",
				},
				Metric: MetricConfig{
					Enabled:           true,
					EnableHostMetrics: tt.enableHostMetrics,
					Exporter:          "none",
				},
			}

			shutdown := initialization(cfg, mockSetupLogging, mockSetupTracing, mockSetupMetrics)
			defer func() {
				assert.NoError(t, shutdown(context.Background()))
			}()

			logOutput := logBuffer.String()
			if tt.expectLog {
				assert.Contains(t, logOutput, "Initializing host metrics collection.", "Expected log message for host metrics initialization")
			} else {
				assert.NotContains(t, logOutput, "Initializing host metrics collection.", "Did not expect log message for host metrics initialization")
			}
		})
	}
}

// initDisabledGlobally verifies that nothing is initialized when o11y is globally disabled.
func TestInitDisabledGlobally(t *testing.T) {
	var logBuffer bytes.Buffer
	mockSetupLogging := func(cfg LogConfig) (zerolog.Logger, ShutdownFunc) {
		return zerolog.New(&logBuffer), func(ctx context.Context) error { return nil }
	}
	mockSetupTracing := func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc) {
		return nil, func(ctx context.Context) error { return nil }
	}
	mockSetupMetrics := func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc) {
		return noop.NewMeterProvider(), func(ctx context.Context) error { return nil }
	}

	cfg := Config{
		Enabled:     false,
		Service:     "test-service",
		Version:     "1.0.0",
		Environment: "test",
		Log: LogConfig{
			Level: "info",
		},
		Metric: MetricConfig{
			Enabled:           true,
			EnableHostMetrics: true,
			Exporter:          "none",
		},
	}

	shutdown := initialization(cfg, mockSetupLogging, mockSetupTracing, mockSetupMetrics)
	defer func() {
		assert.NoError(t, shutdown(context.Background()))
	}()

	logOutput := logBuffer.String()
	assert.Empty(t, logOutput, "Expected no log output when o11y is globally disabled")
}

// initMetricsDisabled verifies that host and runtime metrics are not initialized when metrics are disabled.
func TestInitMetricsDisabled(t *testing.T) {
	var logBuffer bytes.Buffer
	mockSetupLogging := func(cfg LogConfig) (zerolog.Logger, ShutdownFunc) {
		return zerolog.New(&logBuffer), func(ctx context.Context) error { return nil }
	}
	mockSetupTracing := func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc) {
		return nil, func(ctx context.Context) error { return nil }
	}
	mockSetupMetrics := func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc) {
		return noop.NewMeterProvider(), func(ctx context.Context) error { return nil }
	}

	cfg := Config{
		Enabled:     true,
		Service:     "test-service",
		Version:     "1.0.0",
		Environment: "test",
		Log: LogConfig{
			Level: "info",
		},
		Metric: MetricConfig{
			Enabled:           false, // Metrics disabled
			EnableHostMetrics: true,  // Host metrics enabled in config, but should be ignored
			Exporter:          "none",
		},
	}

	shutdown := initialization(cfg, mockSetupLogging, mockSetupTracing, mockSetupMetrics)
	defer func() {
		assert.NoError(t, shutdown(context.Background()))
	}()

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "Metrics disabled by config, skipping standard and runtime metric initialization.", "Expected log message for metrics disabled")
	assert.NotContains(t, logOutput, "Initializing host metrics collection.", "Did not expect host metrics log when metrics are disabled")
	assert.NotContains(t, logOutput, "Initializing Go runtime metrics collection.", "Did not expect runtime metrics log when metrics are disabled")
}

// initStandardMetrics verifies that standard metrics are initialized when metrics are enabled.
func TestInitStandardMetrics(t *testing.T) {
	var logBuffer bytes.Buffer
	mockSetupLogging := func(cfg LogConfig) (zerolog.Logger, ShutdownFunc) {
		return zerolog.New(&logBuffer), func(ctx context.Context) error { return nil }
	}
	mockSetupTracing := func(cfg TraceConfig, res *resource.Resource) (trace.TracerProvider, ShutdownFunc) {
		return nil, func(ctx context.Context) error { return nil }
	}
	mockSetupMetrics := func(cfg MetricConfig, res *resource.Resource) (metric.MeterProvider, ShutdownFunc) {
		return noop.NewMeterProvider(), func(ctx context.Context) error { return nil }
	}

	cfg := Config{
		Enabled:     true,
		Service:     "test-service",
		Version:     "1.0.0",
		Environment: "test",
		Log: LogConfig{
			Level: "info",
		},
		Metric: MetricConfig{
			Enabled:           true,
			EnableHostMetrics: false, // Disable host metrics to focus on standard metrics
			Exporter:          "none",
		},
	}

	shutdown := initialization(cfg, mockSetupLogging, mockSetupTracing, mockSetupMetrics)
	defer func() {
		assert.NoError(t, shutdown(context.Background()))
	}()

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "Metrics initialized.", "Expected metrics initialization log")
	assert.Contains(t, logOutput, "Initializing Go runtime metrics collection.", "Expected runtime metrics initialization log")
	assert.NotContains(t, logOutput, "Initializing host metrics collection.", "Did not expect host metrics log")
}
