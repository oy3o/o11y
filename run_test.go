package o11y

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

func TestRun_Success(t *testing.T) {
	// Setup: Initialize with no-op exporters
	cfg := Config{
		Enabled: true,
		Metric:  MetricConfig{Enabled: true, Exporter: "none"},
		Trace:   TraceConfig{Enabled: true, Exporter: "none"},
	}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	// Test
	err := Run(context.Background(), "test_success", func(ctx context.Context, s State) error {
		s.Log.Info().Msg("Running inside success")
		s.SetAttributes(attribute.String("test.attr", "value"))
		s.AddEvent("test_event")
		s.IncCounter("test.counter") // Will log warning if not registered, but shouldn't panic
		return nil
	})

	assert.NoError(t, err)
}

func TestRun_Error(t *testing.T) {
	cfg := Config{Enabled: true, Trace: TraceConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	expectedErr := errors.New("business error")

	// Test
	err := Run(context.Background(), "test_error", func(ctx context.Context, s State) error {
		return expectedErr
	})

	assert.ErrorIs(t, err, expectedErr)
}

func TestRun_Panic(t *testing.T) {
	cfg := Config{Enabled: true, Trace: TraceConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	err := Run(context.Background(), "test_panic", func(ctx context.Context, s State) error {
		panic("oops")
	})

	// Test: o11y.Run catch panic then return a error
	assert.Error(t, err)
}

func TestState_Baggage(t *testing.T) {
	cfg := Config{Enabled: true, Trace: TraceConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	_ = Run(context.Background(), "test_baggage", func(ctx context.Context, s State) error {
		// Set baggage
		newCtx := s.SetBaggage(ctx, "tenant_id", "1001")

		// Verify baggage is in the new context
		b := baggage.FromContext(newCtx)
		m := b.Member("tenant_id")
		assert.Equal(t, "1001", m.Value())

		// Verify original context is unchanged (baggage is immutable)
		bOld := baggage.FromContext(ctx)
		assert.Empty(t, bOld.Member("tenant_id").Value())

		return nil
	})
}
