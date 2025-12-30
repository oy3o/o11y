package o11y

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

// --- Mocks for metric functions ---
var (
	mu sync.Mutex

	addToInt64UpDownCounterCalls []struct {
		Name       string
		Value      int64
		Attributes []attribute.KeyValue
	}
	addToIntCounterCalls []struct {
		Name       string
		Value      int64
		Attributes []attribute.KeyValue
	}
	recordInFloat64HistogramCalls []struct {
		Name       string
		Value      float64
		Attributes []attribute.KeyValue
	}
)

func resetMetricMocks() {
	mu.Lock()
	defer mu.Unlock()
	addToInt64UpDownCounterCalls = nil
	addToIntCounterCalls = nil
	recordInFloat64HistogramCalls = nil
	resetMetricFuncs() // Reset the actual functions in o11y package
}

// --- Test cases for Handler middleware ---

func TestHandlerMiddleware(t *testing.T) {
	resetMetricMocks()

	addToInt64UpDownCounterFunc = func(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()
		addToInt64UpDownCounterCalls = append(addToInt64UpDownCounterCalls, struct {
			Name       string
			Value      int64
			Attributes []attribute.KeyValue
		}{Name: name, Value: value, Attributes: attributes})
	}
	addToIntCounterFunc = func(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()
		addToIntCounterCalls = append(addToIntCounterCalls, struct {
			Name       string
			Value      int64
			Attributes []attribute.KeyValue
		}{Name: name, Value: value, Attributes: attributes})
	}
	recordInFloat64HistogramFunc = func(ctx context.Context, name string, value float64, attributes ...attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()
		recordInFloat64HistogramCalls = append(recordInFloat64HistogramCalls, struct {
			Name       string
			Value      float64
			Attributes []attribute.KeyValue
		}{Name: name, Value: value, Attributes: attributes})
	}

	// Configure o11y for the test
	cfg := Config{
		Enabled: true,
		Service: "test-service",
		Log: LogConfig{
			Level: "info",
		},
		Metric: MetricConfig{
			Enabled: true,
		},
	}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	// Create a test handler that the middleware will wrap
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Apply the o11y middleware
	middleware := Handler(cfg)
	wrappedHandler := middleware(testHandler)

	// Create a test server
	ts := httptest.NewServer(wrappedHandler)
	defer ts.Close()

	// Make a request
	resp, err := http.Get(ts.URL + "/test-route")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Allow a small delay for metrics to be processed if asynchronous
	time.Sleep(10 * time.Millisecond)

	// Verify active requests
	assert.Len(t, addToInt64UpDownCounterCalls, 2)
	assert.Equal(t, "http.server.active_requests", addToInt64UpDownCounterCalls[0].Name)
	assert.Equal(t, int64(1), addToInt64UpDownCounterCalls[0].Value)
	assert.Equal(t, "http.server.active_requests", addToInt64UpDownCounterCalls[1].Name)
	assert.Equal(t, int64(-1), addToInt64UpDownCounterCalls[1].Value)

	// Verify request count
	assert.Len(t, addToIntCounterCalls, 1)
	assert.Equal(t, "http.server.request.total", addToIntCounterCalls[0].Name)
	assert.Equal(t, int64(1), addToIntCounterCalls[0].Value)
	assert.Contains(t, addToIntCounterCalls[0].Attributes, attribute.String("http.method", "GET"))
	assert.Contains(t, addToIntCounterCalls[0].Attributes, attribute.String("http.route", "/test-route"))
	assert.Contains(t, addToIntCounterCalls[0].Attributes, attribute.Int("http.status_code", http.StatusOK))

	// Verify request duration
	assert.Len(t, recordInFloat64HistogramCalls, 1)
	assert.Equal(t, "http.server.request.duration", recordInFloat64HistogramCalls[0].Name)
	assert.Greater(t, recordInFloat64HistogramCalls[0].Value, float64(0))
	assert.Contains(t, recordInFloat64HistogramCalls[0].Attributes, attribute.String("http.method", "GET"))
	assert.Contains(t, recordInFloat64HistogramCalls[0].Attributes, attribute.String("http.route", "/test-route"))
	assert.Contains(t, recordInFloat64HistogramCalls[0].Attributes, attribute.Int("http.status_code", http.StatusOK))
}

func TestHandlerMiddlewarePanicRecovery(t *testing.T) {
	resetMetricMocks()

	addToInt64UpDownCounterFunc = func(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()
		addToInt64UpDownCounterCalls = append(addToInt64UpDownCounterCalls, struct {
			Name       string
			Value      int64
			Attributes []attribute.KeyValue
		}{Name: name, Value: value, Attributes: attributes})
	}
	addToIntCounterFunc = func(ctx context.Context, name string, value int64, attributes ...attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()
		addToIntCounterCalls = append(addToIntCounterCalls, struct {
			Name       string
			Value      int64
			Attributes []attribute.KeyValue
		}{Name: name, Value: value, Attributes: attributes})
	}
	recordInFloat64HistogramFunc = func(ctx context.Context, name string, value float64, attributes ...attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()
		recordInFloat64HistogramCalls = append(recordInFloat64HistogramCalls, struct {
			Name       string
			Value      float64
			Attributes []attribute.KeyValue
		}{Name: name, Value: value, Attributes: attributes})
	}

	// Configure o11y for the test
	cfg := Config{
		Enabled: true,
		Service: "test-service",
		Log: LogConfig{
			Level: "info",
		},
		Metric: MetricConfig{
			Enabled: true,
		},
	}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	// Create a test handler that will panic
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Apply the o11y middleware
	middleware := Handler(cfg)
	wrappedHandler := middleware(panicHandler)

	// Create a test server
	ts := httptest.NewServer(wrappedHandler)
	defer ts.Close()

	// Make a request that causes a panic
	resp, _ := http.Get(ts.URL + "/panic-route")
	// We expect an error here because the server panics and closes the connection.
	// The important thing is that the panic is recovered and the metrics are recorded.
	if resp != nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	}

	// Allow a small delay for metrics to be processed if asynchronous
	time.Sleep(10 * time.Millisecond)

	// Verify active requests (should still be decremented)
	assert.Len(t, addToInt64UpDownCounterCalls, 2)
	assert.Equal(t, "http.server.active_requests", addToInt64UpDownCounterCalls[0].Name)
	assert.Equal(t, int64(1), addToInt64UpDownCounterCalls[0].Value)
	assert.Equal(t, "http.server.active_requests", addToInt64UpDownCounterCalls[1].Name)
	assert.Equal(t, int64(-1), addToInt64UpDownCounterCalls[1].Value)

	// Verify request count (should still be incremented, even on panic)
	assert.Len(t, addToIntCounterCalls, 1)
	assert.Equal(t, "http.server.request.total", addToIntCounterCalls[0].Name)
	assert.Equal(t, int64(1), addToIntCounterCalls[0].Value)
	assert.Contains(t, addToIntCounterCalls[0].Attributes, attribute.String("http.method", "GET"))
	assert.Contains(t, addToIntCounterCalls[0].Attributes, attribute.String("http.route", "/panic-route"))
	assert.Contains(t, addToIntCounterCalls[0].Attributes, attribute.Int("http.status_code", http.StatusInternalServerError))

	// Verify request duration
	assert.Len(t, recordInFloat64HistogramCalls, 1)
	assert.Equal(t, "http.server.request.duration", recordInFloat64HistogramCalls[0].Name)
	assert.Greater(t, recordInFloat64HistogramCalls[0].Value, float64(0))
	assert.Contains(t, recordInFloat64HistogramCalls[0].Attributes, attribute.String("http.method", "GET"))
	assert.Contains(t, recordInFloat64HistogramCalls[0].Attributes, attribute.String("http.route", "/panic-route"))
	assert.Contains(t, recordInFloat64HistogramCalls[0].Attributes, attribute.Int("http.status_code", http.StatusInternalServerError))
}
