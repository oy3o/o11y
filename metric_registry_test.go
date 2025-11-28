package o11y

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetricRegistry_DynamicRegistration(t *testing.T) {
	cfg := Config{Enabled: true, Metric: MetricConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	name := "dynamic_counter"

	// 1. Register a new metric
	assert.NotPanics(t, func() {
		RegisterInt64Counter(name, "desc", "1")
	})

	// 2. Record value (should succeed)
	assert.NotPanics(t, func() {
		AddToIntCounter(context.Background(), name, 10)
	})

	// 3. Re-registering same metric should be safe (idempotent or warn)
	assert.NotPanics(t, func() {
		RegisterInt64Counter(name, "desc", "1")
	})
}

func TestMetricRegistry_MissingMetric(t *testing.T) {
	cfg := Config{Enabled: true, Metric: MetricConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	// Recording to a non-existent metric should not panic (it just logs debug/warn)
	assert.NotPanics(t, func() {
		AddToIntCounter(context.Background(), "non_existent_metric", 1)
	})

	assert.NotPanics(t, func() {
		RecordInFloat64Histogram(context.Background(), "non_existent_histogram", 123.45)
	})
}

func TestMetricRegistry_TypeMismatch(t *testing.T) {
	cfg := Config{Enabled: true, Metric: MetricConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	name := "mismatch_test"
	RegisterInt64Counter(name, "desc", "1")

	// Try to record as Histogram (should fail safely)
	assert.NotPanics(t, func() {
		RecordInFloat64Histogram(context.Background(), name, 10.5)
	})
}
