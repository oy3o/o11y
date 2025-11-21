package o11y

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Run is the flagship function of the o11y package.
// It wraps a block of business logic, automatically providing it with comprehensive
// observability: tracing, context-aware logging, and metrics for latency, calls, and errors.
func Run(
	ctx context.Context,
	name string, // e.g., "ProcessOrder", "ValidateUserCredentials"
	fn func(ctx context.Context, s State) error,
) error {
	// 1. Prepare Observability Objects
	parentLogger := GetLoggerFromContext(ctx)

	ctxWithSpan, span := Tracer.Start(ctx, name)
	defer span.End()

	// Create a new logger enriched with the span context.
	spanLogger := parentLogger.With().
		Str("trace_id", span.SpanContext().TraceID().String()).
		Str("span_id", span.SpanContext().SpanID().String()).
		Str("operation", name).
		Logger()

	// Inject the enriched logger back into the context so inner calls use it.
	ctxWithLogger := spanLogger.WithContext(ctxWithSpan)

	s := State{
		Log:   spanLogger,
		span:  span,
		meter: Meter,
	}

	// 2. Automatic Panic Handling
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic recovered in o11y.Run: %v", r)

			span.RecordError(err, trace.WithStackTrace(true))
			span.SetStatus(codes.Error, "panic occurred")

			s.Log.Panic().Msgf("Panic during operation: %v", r)
			panic(r)
		}
	}()

	// 3. Automatic Latency and Call Count Metrics
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		operationAttr := attribute.String("operation", name)
		s.RecordHistogram("app.operation.duration", duration, operationAttr)
	}()

	// 4. Execute business logic
	err := fn(ctxWithLogger, s)

	// 5. Result Handling
	operationAttr := attribute.String("operation", name)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.IncCounter("app.operation.errors.total", operationAttr)
	} else {
		span.SetStatus(codes.Ok, "success")
		// No more MetricOptions handling here.
		// Users should call s.IncCounter inside fn if they want custom success metrics.
	}

	return err
}

// GetLoggerFromContext is a helper function to safely retrieve a zerolog.Logger from a context.
// If no logger is found in the context, it returns the global default logger.
func GetLoggerFromContext(ctx context.Context) *zerolog.Logger {
	// zerolog.Ctx(ctx) handles the case where no logger is in the context
	// by returning a disabled logger. We'll check its output writer and if it's
	// a disabled logger, we return the global logger instead.
	l := zerolog.Ctx(ctx)
	if l.GetLevel() == zerolog.Disabled {
		return &log.Logger
	}
	return l
}
