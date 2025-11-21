# o11y: A Standardized Observability Toolkit for Go

[![Go Report Card](https://goreportcard.com/badge/github.com/oy3o/o11y)](https://goreportcard.com/report/github.com/oy3o/o11y)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[中文](./README.zh.md) | [English](./README.md)

`o11y` is a zero-configuration, convention-over-configuration observability framework for Go. It aims to provide a unified solution for logs, traces, and metrics for all Go services through an extremely simple API, minimizing cognitive load for developers.

**Our Philosophy: Achieve complete observability for any piece of business logic with a single line of code.**

## Features

- **Flagship `o11y.Run()` Function**: Wrap any business logic to automatically gain logging, tracing, and metrics capabilities.
- **Seamless Log Enhancement**: Inside `o11y.Run`, your existing `zerolog` code will automatically be enriched with `trace_id` and `span_id`.
- **Full Stack Instrumentation**:
  - **HTTP**: Middleware for Server (`http.Handler`) and Client (`o11y.NewHTTPClient`).
  - **gRPC**: Interceptors for Server and Client with panic recovery and context propagation.
  - **Database**: Drop-in replacement for `sql.Open` with `o11y.OpenSQL` (powered by `otelsql` and SQLCommenter).
- **Automated Tracing & Metrics**: Automatically creates a Trace Span for the wrapped code block and records core metrics like latency, call count, and error rate.
- **Context Propagation (Baggage)**: Easily propagate metadata (like Tenant ID) across microservice boundaries using the `State` object.
- **Configuration-Driven**: All features are configured via a simple YAML file.

## Quick Start

Integrating `o11y` into your project is extremely simple.

### 1. Add Configuration File (`config.yaml`)

Create a `config.yaml` file in your project's root directory.

```yaml
o11y:
  enabled: true
  service: "order-service"
  version: "1.2.0"
  environment: "production"
  instrumentation_scope: "o11y"

  log:
    level: "info"
    console: false # Console logging is typically disabled in production
    file: true
    rotation:
      filename: "logs/app.log"
      max_size: 100
      max_backups: 5
      max_age: 30
      compress: true

  trace:
    enabled: true
    exporter: "otlp-grpc"
    endpoint: "otel-collector:4317"
    sample_ratio: 1.0

  metric:
    enabled: true
    exporter: "prometheus"
    prometheus_path: "/metrics"
    enable_host_metrics: true
```

### 2. Initialize in `main.go`

Call `o11y.Init()` at startup and ensure `shutdown` is called before exit.

```go
package main

import (
	"context"
	"net/http"
	"github.com/oy3o/o11y"
	"github.com/oy3o/conf"
)

func main() {
	// 1. Load configuration
	cfg, _ := conf.Load[Config]("config.yaml")

	// 2. Initialize o11y
	shutdown := o11y.Init(cfg)
	defer shutdown(context.Background()) 

	// 3. Set up HTTP routes and apply the o11y middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", orderHandler)
	
    // Wrap the mux with observability middleware
	handler := o11y.Handler(cfg)(mux)

	http.ListenAndServe(":8080", handler)
}
```

### 3. Database & gRPC Integration

`o11y` provides wrappers to instrument other parts of your system easily.

**Database (SQL):**
```go
// Use o11y.OpenSQL instead of sql.Open
db, err := o11y.OpenSQL("postgres", "postgres://user:pass@localhost:5432/db")
if err != nil {
    log.Fatal().Err(err).Msg("failed to connect")
}
// Register connection pool stats (optional)
o11y.RegisterDBStatsMetrics(db, "primary-db")
```

**gRPC Server:**
```go
// Add o11y options to your gRPC server
s := grpc.NewServer(o11y.GRPCServerOptions()...)
```

**HTTP Client:**
```go
// Use the instrumented client for outbound requests
client := o11y.NewHTTPClient(nil)
```

### 4. Empower Your Business Logic with `o11y.Run()`

Wrap your business code to get all observability features for free.

```go
func processOrder(ctx context.Context, orderID string) error {
    // Wrap your logic with o11y.Run.
    // "process_order" becomes the Span name and Operation name for metrics.
    return o11y.Run(ctx, "process_order", func(ctx context.Context, s o11y.State) error {
        // s.Log already includes trace_id and span_id automatically
        log := s.Log

        // s.Log already includes trace_id and span_id automatically
        log.Info().Str("order_id", orderID).Msg("Starting to process order")

        // Simulate DB call (automatically traced via o11y.OpenSQL)
        if err := db.ExecContext(ctx, "UPDATE orders SET status = ?", "PROCESSING"); err != nil {
            return err // o11y auto-records error and updates Span status
        }

        // Manually record a custom business metric on success
        s.IncCounter("orders_processed_total")
        log.Info().Msg("Order processed successfully")
        return nil
    })
}
```

---

## Advanced Usage: `State` Object

The `State` object provided by `o11y.Run` allows you to add rich business context and propagate baggage.

```go
o11y.Run(ctx, "find_product", func(ctx context.Context, s o11y.State) error {
    // 1. Add attributes to the current Span (for Tracing search)
    s.SetAttributes(attribute.String("product_id", productID))
    
    // 2. Propagate Context (Baggage)
    // Adds "tier=gold" to the context, propagated to downstream services via HTTP/gRPC headers.
    // IMPORTANT: You must use the returned 'ctx' for subsequent calls.
    ctx = s.SetBaggage(ctx, "user_tier", "gold")
    
    // 3. Record Metrics (Histogram for duration/values)
    s.RecordHistogram("custom.operation.latency", 0.123)
    
    // 4. Add Timeline Event (Logs event to Trace)
    s.AddEvent("cache_miss")
    
    return nil
})
```

---

## 📈 Out-of-the-Box Metrics

`o11y` automatically collects the following standard metrics:

#### **System & Runtime**
- `system.cpu.utilization`: System CPU utilization.
- `system.memory.usage`: System memory usage.
- `go.goroutines`: The current number of Goroutines.
- `go.gc.pause_total`: The total duration of GC pauses.

#### **HTTP Server**
- `http.server.request.count`: Total number of requests (labels: method, route, status_code).
- `http.server.request.duration`: Request latency distribution.
- `http.server.active_requests`: Number of currently active requests.

#### **Database**
- `db.client.duration`: Duration of database queries.
- `sql.db.stats.connections.open`: Total number of open connections.
- `sql.db.stats.connections.idle`: Number of idle connections.
- `sql.db.stats.connections.in_use`: Number of connections currently in use.

#### **Business Logic (`o11y.Run`)**
- `app.operation.duration`: Execution duration of the business logic block.
- `app.operation.errors.total`: Total number of errors in the business logic block.

## Overall Architecture

`o11y` produces data. We recommend using the **OpenTelemetry Collector** to gather it, storing it in **Prometheus** (metrics), **Loki** (logs), and **Jaeger/Tempo** (traces), and visualizing it with **Grafana**.
