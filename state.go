package o11y

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// State is the primary interaction object provided within the o11y.Run closure.
// It encapsulates logging, tracing, and metric functionalities tied to the current operation,
// providing a simplified and consistent API for all observability needs.
type State struct {
	//
	ctx context.Context

	// Log is a zerolog.Logger instance pre-configured with the correct trace_id and span_id.
	// Developers should use this for all logging within the o11y.Run block to ensure
	// logs are automatically correlated with traces.
	Log zerolog.Logger

	// span is the active OpenTelemetry trace span for the current o11y.Run block.
	// It is kept private to encourage interaction via the simplified helper methods.
	span trace.Span

	// meter is the OpenTelemetry meter used to record metrics.
	// It is also kept private.
	meter metric.Meter
}

// SetAttributes adds key-value attributes to the current trace span.
// This is equivalent to adding a "tag" or "label" to the span, which is invaluable
// for filtering, searching, and analyzing traces in backends like Jaeger or Tempo.
// The value can be of any standard type (string, int, bool, float, etc.).
//
// Example:
//
//	s.SetAttributes(attribute.Int("user_id", 12345), attribute.Int("http.request.content_length", 512))
func (s State) SetAttributes(attributes ...attribute.KeyValue) {
	s.span.SetAttributes(attributes...)
}

// SetBaggage adds a key-value pair to the OpenTelemetry Baggage.
// Baggage is used to propagate context across process boundaries (e.g., to downstream services).
//
// IMPORTANT: Unlike SetAttributes, Baggage is stored in the Context.
// This method returns a NEW Context containing the updated Baggage.
// You MUST use the returned Context for subsequent calls (like HTTP requests)
// if you want the baggage to propagate.
//
// Example:
//
//	ctx = s.SetBaggage(ctx, "tenant_id", "1001")
//	http.NewRequestWithContext(ctx, ...)
func (s State) SetBaggage(ctx context.Context, key, value string) context.Context {
	m, err := baggage.NewMember(key, value)
	if err != nil {
		s.Log.Warn().Err(err).Str("key", key).Msg("Failed to create baggage member")
		return ctx
	}

	b, err := baggage.FromContext(ctx).SetMember(m)
	if err != nil {
		s.Log.Warn().Err(err).Str("key", key).Msg("Failed to set baggage member")
		return ctx
	}

	return baggage.ContextWithBaggage(ctx, b)
}

// AddEvent records a timestamped event on the current span's timeline.
func (s State) AddEvent(name string, attributes ...attribute.KeyValue) {
	s.span.AddEvent(name, trace.WithAttributes(attributes...))
}

// IncCounter increments a pre-registered counter metric by 1.
// This is the standard way to count occurrences of an event, such as a cache hit or a login attempt.
// The metric name must correspond to a counter pre-registered in the metric_registry.
//
// Example:
//
//	s.IncCounter("app.cache.events.total", attribute.String("result", "hit"))
func (s State) IncCounter(name string, attributes ...attribute.KeyValue) {
	AddToIntCounter(s.ctx, name, 1, attributes...)
}

// RecordHistogram records a value in a pre-registered histogram metric.
// This is ideal for measuring the distribution of values, most commonly for timing and latency.
// The value is typically a duration converted to a float64.
// The metric name must correspond to a histogram pre-registered in the metric_registry.
//
// Example:
//
//	startTime := time.Now()
//	// ... perform a database operation ...
//	duration := time.Since(startTime).Seconds()
//	s.RecordHistogram("db.client.duration", duration, attribute.String("db.table", "users"))
func (s State) RecordHistogram(name string, value float64, attributes ...attribute.KeyValue) {
	RecordInFloat64Histogram(s.ctx, name, value, attributes...)
}
