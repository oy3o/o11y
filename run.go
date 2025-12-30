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
) (err error) {
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
		ctx:   ctxWithLogger,
		Log:   spanLogger,
		span:  span,
		meter: Meter,
	}

	// 2. Automatic Panic Handling
	defer func() {
		if r := recover(); r != nil {
			// 捕获 Panic 并转换为 Error。
			// 这样上层调用者可以像处理普通错误一样处理 Panic（例如返回 500 响应），
			// 同时也保证了 Span 和 Metrics 的正确记录。
			panicErr := fmt.Errorf("panic recovered in o11y.Run: %v", r)

			// 记录到 Span
			span.RecordError(panicErr, trace.WithStackTrace(true))
			span.SetStatus(codes.Error, "panic occurred")

			// 记录到 Log (使用 PanicLevel 可能会导致 os.Exit，视 zerolog 配置而定，这里改用 Error 级别更安全)
			s.Log.Error().Msgf("Panic recovered during operation: %v", r)

			// 记录 Metrics (因为正常的 return err 路径会被跳过，所以这里要手动记)
			operationAttr := attribute.String("operation", name)
			s.IncCounter("biz.operation.error.total", operationAttr)

			// 将 panic 错误赋值给返回变量
			err = panicErr
		}
	}()

	// 3. Automatic Latency and Call Count Metrics
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		operationAttr := attribute.String("operation", name)
		s.RecordHistogram("biz.operation.duration", duration, operationAttr)
	}()

	// 4. Execute business logic
	err = fn(ctxWithLogger, s)

	// 5. Result Handling
	operationAttr := attribute.String("operation", name)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.IncCounter("biz.operation.error.total", operationAttr)
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
