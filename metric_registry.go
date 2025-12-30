package o11y

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricInstrument holds a generic OpenTelemetry instrument.
// Using a struct allows us to store different instrument types (Counter, Histogram, etc.)
// under a single map entry, providing type safety when we retrieve them.
type MetricInstrument struct {
	Int64Counter       metric.Int64Counter
	Float64Histogram   metric.Float64Histogram
	Int64UpDownCounter metric.Int64UpDownCounter
	// NOTE: More instrument types like Gauge or UpDownCounter can be added here as needed.
}

// registry stores all pre-registered standard metric instruments.
// We use atomic.Value to store map[string]MetricInstrument to achieve lock-free reads.
var (
	registry atomic.Value

	// registryMu protects the write operations to the registry (Copy-On-Write).
	registryMu sync.Mutex

	// registryOnce ensures InitStandardMetrics is called only once.
	registryOnce sync.Once

	// localValues stores the current values of counters for in-process querying.
	// Map key is the metric name. Value is *atomic.Int64.
	// We use sync.Map for thread-safe concurrent access.
	localValues = xsync.NewMap[string, *atomic.Int64]()
)

// InitStandardMetrics creates and registers all standard metrics that the o11y library provides.
// This function is called once by o11y.Init to populate the registry.
// {Namespace}.{Subsystem}.{Target}.{Suffix}
func InitStandardMetrics(meter metric.Meter) {
	registryOnce.Do(func() {
		log.Debug().Msg("Initializing standard metrics registry...")

		// Initialize with an empty map if nil
		if registry.Load() == nil {
			registry.Store(make(map[string]MetricInstrument))
		}

		// --- HTTP Server Metrics ---
		RegisterFloat64Histogram("http.server.request.duration", "Measures the duration of inbound HTTP requests.", "s")
		RegisterInt64Counter("http.server.request.total", "Counts the total number of inbound HTTP requests.", "{request}")
		RegisterInt64UpDownCounter("http.server.active_requests", "Measures the number of concurrent inbound HTTP requests that are currently in-flight.", "{request}")

		// --- RPC/gRPC Metrics ---
		// 注册 gRPC Panic 计数器
		RegisterInt64Counter("rpc.server.panic.total", "Counts the number of panics in gRPC handlers.", "{panic}")

		// --- Database Metrics ---
		RegisterFloat64Histogram("db.client.query.duration", "Measures the duration of database queries.", "s")

		// --- Application Operation Metrics ---
		RegisterFloat64Histogram("biz.operation.duration", "Measures the duration of a specific business logic operation.", "s")
		RegisterInt64Counter("biz.operation.error.total", "Counts the total number of errors for a specific business logic operation.", "{error}")

		// --- Manual/Business Metrics ---
		RegisterInt64Counter("cache.client.operation.total", "Counts cache hits and misses.", "{event}")

		log.Info().Msg("Standard metrics registry initialized.")
	})
}

// RegisterInt64Counter creates and registers a new Int64Counter.
// It is safe to call this concurrently after o11y.Init.
func RegisterInt64Counter(name, description, unit string) {
	if Meter == nil {
		log.Error().Msg("o11y.Meter is nil. Call o11y.Init before registering metrics.")
		return
	}

	inst, err := Meter.Int64Counter(
		name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
	if err != nil {
		log.Error().Err(err).Str("name", name).Msg("Failed to create Int64Counter")
		return
	}

	register(name, MetricInstrument{Int64Counter: inst})
}

// RegisterFloat64Histogram creates and registers a new Float64Histogram.
func RegisterFloat64Histogram(name, description, unit string) {
	if Meter == nil {
		log.Error().Msg("o11y.Meter is nil. Call o11y.Init before registering metrics.")
		return
	}

	inst, err := Meter.Float64Histogram(
		name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
	if err != nil {
		log.Error().Err(err).Str("name", name).Msg("Failed to create Float64Histogram")
		return
	}

	register(name, MetricInstrument{Float64Histogram: inst})
}

// RegisterInt64UpDownCounter creates and registers a new Int64UpDownCounter.
func RegisterInt64UpDownCounter(name, description, unit string) {
	if Meter == nil {
		log.Error().Msg("o11y.Meter is nil. Call o11y.Init before registering metrics.")
		return
	}

	inst, err := Meter.Int64UpDownCounter(
		name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
	if err != nil {
		log.Error().Err(err).Str("name", name).Msg("Failed to create Int64UpDownCounter")
		return
	}

	register(name, MetricInstrument{Int64UpDownCounter: inst})
}

// register adds the instrument to the global registry using Copy-On-Write.
func register(name string, inst MetricInstrument) {
	registryMu.Lock()
	defer registryMu.Unlock()

	oldMap := getRegistryMap()
	newMap := make(map[string]MetricInstrument, len(oldMap)+1)

	for k, v := range oldMap {
		newMap[k] = v
	}

	if _, exists := newMap[name]; exists {
		log.Warn().Str("metric", name).Msg("Overwriting existing metric definition in registry")
	}

	newMap[name] = inst
	registry.Store(newMap)
}

// getRegistryMap safely retrieves the current registry map.
func getRegistryMap() map[string]MetricInstrument {
	val := registry.Load()
	if val == nil {
		return nil
	}
	return val.(map[string]MetricInstrument)
}

// --- Internal accessor functions for the Helper to use ---

// These variables hold the actual implementations of the metric recording functions.
// They can be swapped out in tests for mocking purposes.
var (
	addToIntCounterFunc          = addToIntCounterImpl
	addToInt64UpDownCounterFunc  = addToInt64UpDownCounterImpl
	recordInFloat64HistogramFunc = recordInFloat64HistogramImpl
)

// AddToIntCounter finds a pre-registered Int64Counter and adds a value to it.
// This is the underlying implementation for the Helper's `IncCounter`.
func AddToIntCounter(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
	addToIntCounterFunc(ctx, name, value, attributes...)
}

// addToIntCounterImpl is the default implementation of AddToIntCounter.
func addToIntCounterImpl(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
	reg := getRegistryMap()
	if reg == nil {
		return
	}

	instrument, ok := reg[name]
	if !ok {
		log.Debug().Str("metric_name", name).Msg("Metric not registered, skipping record")
		return
	}
	if instrument.Int64Counter == nil {
		log.Warn().Str("metric_name", name).Msg("Metric type mismatch: expected Int64Counter")
		return
	}

	instrument.Int64Counter.Add(ctx, value, metric.WithAttributes(attributes...))

	// Update local value for querying
	val, _ := localValues.LoadOrStore(name, &atomic.Int64{})
	val.Add(value)
}

// AddToInt64UpDownCounter finds a pre-registered Int64UpDownCounter and adds a value to it.
func AddToInt64UpDownCounter(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
	addToInt64UpDownCounterFunc(ctx, name, value, attributes...)
}

// addToInt64UpDownCounterImpl is the default implementation of AddToInt64UpDownCounter.
func addToInt64UpDownCounterImpl(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
	reg := getRegistryMap()
	if reg == nil {
		return
	}

	instrument, ok := reg[name]
	if !ok {
		log.Debug().Str("metric_name", name).Msg("Metric not registered, skipping record")
		return
	}
	if instrument.Int64UpDownCounter == nil {
		log.Warn().Str("metric_name", name).Msg("Metric type mismatch: expected Int64UpDownCounter")
		return
	}

	instrument.Int64UpDownCounter.Add(ctx, value, metric.WithAttributes(attributes...))

	// Update local value for querying
	val, _ := localValues.LoadOrStore(name, &atomic.Int64{})
	val.Add(value)
}

// RecordInFloat64Histogram finds a pre-registered Float64Histogram and records a value.
func RecordInFloat64Histogram(ctx context.Context, name string, value float64, attributes ...attribute.KeyValue) {
	recordInFloat64HistogramFunc(ctx, name, value, attributes...)
}

// recordInFloat64HistogramImpl is the default implementation of RecordInFloat64Histogram.
func recordInFloat64HistogramImpl(ctx context.Context, name string, value float64, attributes ...attribute.KeyValue) {
	reg := getRegistryMap()
	if reg == nil {
		return
	}

	instrument, ok := reg[name]
	if !ok {
		log.Debug().Str("metric_name", name).Msg("Metric not registered, skipping record")
		return
	}
	if instrument.Float64Histogram == nil {
		log.Warn().Str("metric_name", name).Msg("Metric type mismatch: expected Float64Histogram")
		return
	}

	instrument.Float64Histogram.Record(ctx, value, metric.WithAttributes(attributes...))
}

// resetMetricFuncs resets the metric recording functions to their default implementations.
// This is primarily used in tests to clean up mocks.
func resetMetricFuncs() {
	addToIntCounterFunc = addToIntCounterImpl
	addToInt64UpDownCounterFunc = addToInt64UpDownCounterImpl
	recordInFloat64HistogramFunc = recordInFloat64HistogramImpl
}

// GetMetricValue returns the current value of a registered counter.
// This is useful for internal dashboards/APIs that need to display current stats.
func GetMetricValue(name string) int64 {
	val, ok := localValues.Load(name)
	if !ok {
		return 0
	}
	return val.Load()
}
