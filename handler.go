package o11y

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/felixge/httpsnoop"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Handler is a factory function that creates a new o11y HTTP middleware.
// This single middleware wraps the provided handler with a complete suite of observability tools.
//
// Usage:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/", myHandler)
//	o11yMiddleware := o11y.Handler(cfg)
//	server := &http.Server{
//	    Addr:    ":8080",
//	    Handler: o11yMiddleware(mux),
//	}
func Handler(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// The inner handler contains our custom logic: panic recovery, metrics, and logger injection.
		innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Record active requests
			AddToInt64UpDownCounter(r.Context(), "http.server.active_requests", 1)
			defer AddToInt64UpDownCounter(r.Context(), "http.server.active_requests", -1)

			// 1. Contextual Logger Injection
			// We do this *before* metrics capture so the handler has the logger.
			span := trace.SpanFromContext(r.Context())
			parentLogger := GetLoggerFromContext(r.Context())

			var loggerWithTrace zerolog.Logger
			if span.SpanContext().IsValid() {
				loggerWithTrace = parentLogger.With().
					Str("trace_id", span.SpanContext().TraceID().String()).
					Str("span_id", span.SpanContext().SpanID().String()).
					Logger()
			} else {
				loggerWithTrace = *parentLogger
			}

			ctxWithLogger := loggerWithTrace.WithContext(r.Context())
			reqWithLogger := r.WithContext(ctxWithLogger)

			// 2. Metrics & Panic Recovery via httpsnoop
			// httpsnoop.CaptureMetrics executes the handler and captures status code & duration.
			// It automatically supports http.Flusher, http.Hijacker, etc.
			m := httpsnoop.CaptureMetrics(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				defer func() {
					if rcv := recover(); rcv != nil {
						err := fmt.Errorf("panic recovered: %v", rcv)

						// Record panic on Span
						span.RecordError(err, trace.WithStackTrace(true))
						span.SetStatus(codes.Error, "panic")

						// Log panic
						stack := FilterStackTrace(string(debug.Stack()), cfg.Log.StackFilters)
						GetLoggerFromContext(rr.Context()).Error().
							Interface("error", rcv).
							Str("stack", stack).
							Msg("HTTP request recovered from panic")

						// Write 500 error. This updates the httpsnoop writer state.
						http.Error(ww, "Internal Server Error", http.StatusInternalServerError)
					}
				}()

				next.ServeHTTP(ww, rr)
			}), w, reqWithLogger)

			// 3. Record Metrics
			route := r.URL.Path
			commonAttrs := []attribute.KeyValue{
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
				attribute.Int("http.status_code", m.Code),
			}

			AddToIntCounter(r.Context(), "http.server.request.count", 1, commonAttrs...)
			// m.Duration is time.Duration
			RecordInFloat64Histogram(r.Context(), "http.server.request.duration", m.Duration.Seconds(), commonAttrs...)
		})

		// Wrap with standard otelhttp to generate spans
		return otelhttp.NewHandler(innerHandler, cfg.Service)
	}
}
