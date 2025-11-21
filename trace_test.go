package o11y

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
)

// TestSetupTracing_Propagator verifies that the TextMapPropagator is correctly registered.
func TestSetupTracing_Propagator(t *testing.T) {
	// Reset the global propagator before test to ensure a clean state
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator()) // Reset to empty/default

	cfg := TraceConfig{
		Enabled:     true,
		Exporter:    "none",
		SampleRatio: 1.0,
	}
	res := resource.Default()

	_, shutdown := setupTracing(cfg, res)
	defer shutdown(context.Background())

	// Check if the global propagator has been set.
	// otel.GetTextMapPropagator() returns the global propagator.
	p := otel.GetTextMapPropagator()

	// We expect a CompositeTextMapPropagator containing TraceContext and Baggage.
	// Since we can't easily inspect the internal fields of the composite propagator via public API,
	// we can verify its behavior by checking fields it injects.

	assert.NotNil(t, p, "Global TextMapPropagator should not be nil")

	// Verify TraceContext injection
	// TraceContext injects "traceparent"
	fields := p.Fields()
	assert.Contains(t, fields, "traceparent", "Propagator should support 'traceparent' (TraceContext)")
	assert.Contains(t, fields, "baggage", "Propagator should support 'baggage' (Baggage)")
}
