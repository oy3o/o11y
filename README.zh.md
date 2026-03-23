# o11y: 标准化可观测性 Go 工具包

[![Go Report Card](https://goreportcard.com/badge/github.com/oy3o/o11y)](https://goreportcard.com/report/github.com/oy3o/o11y)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[中文](./README.zh.md) | [English](./README.md)

`o11y` 是一个零配置、约定优于配置的 Go 可观测性（Observability）框架。它的目标是通过提供一个极其简单的 API，为所有 Go 服务提供统一的日志、追踪和指标解决方案。

**我们的哲学：用一行代码，为一段业务逻辑赋予完整的可观测性。**

## 特性

- **旗舰级 `o11y.Run()` 函数**: 将任意业务逻辑包裹起来，自动获得日志、追踪和指标能力。
- **无感知的日志增强**: 在 `o11y.Run` 内部，你现有的 `zerolog` 代码将自动获得 `trace_id` 和 `span_id` 关联。
- **全栈自动埋点**:
  - **HTTP**: 提供服务端中间件 (`Handler`) 和 客户端封装 (`NewHTTPClient`)。
  - **gRPC**: 提供服务端和客户端拦截器，自动处理上下文传播和 Panic 恢复。
  - **Database**: 通过 `o11y.OpenSQL` 替换 `sql.Open`，内置 SQLCommenter 和性能追踪。
- **自动化的追踪与指标**: 自动为被包裹的代码块创建 Trace Span，并记录延迟、调用次数和错误率等核心指标。
- **上下文传播 (Baggage)**: 通过 `State` 对象轻松在微服务间透传元数据（如 Tenant ID）。
- **配置驱动**: 所有功能均通过一个简单的 YAML 文件进行配置。

## 快速开始

### 1. 添加配置文件 (`config.yaml`)

```yaml
o11y:
  enabled: true
  service: "order-service"
  version: "1.2.0"
  environment: "production"
  instrumentation_scope: "o11y"

  log:
    level: "info"
    console: false # 生产环境通常禁用控制台日志
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

### 2. 在 `main.go` 中初始化

```go
package main

import (
    "context"
    "net/http"
    "github.com/oy3o/o11y"
)

func main() {
    // 1. 加载配置
    cfg, _ := LoadConfig[Config]("config.yaml")

    // 2. 初始化 o11y
    shutdown,_ := o11y.Init(cfg)
    defer shutdown(context.Background()) 

    // 3. 设置 HTTP 路由并应用 o11y 中间件
    mux := http.NewServeMux()
    mux.HandleFunc("/orders", orderHandler)
    
    handler := o11y.Handler(cfg)(mux)

    http.ListenAndServe(":8080", handler)
}
```


### 3. 全栈插桩（Instrumentation）

`o11y` 现已覆盖您的整个技术栈：

#### 数据库 (SQL)
`sql.Open` 的无缝替换方案。自动添加链路追踪（包含 SQL 参数）和指标监控。

```go
// 使用 o11y.OpenSQL 替代 sql.Open
db, err := o11y.OpenSQL("postgres", "dsn...")

// 或者使用 Connector（例如用于 pgx）
db := o11y.OpenDBWithConnector("pgx", connector)

// 注册连接池指标
o11y.RegisterDBStatsMetrics(db, "primary-db")
```

#### gRPC
包含 Panic 恢复和上下文传播功能的客户端与服务端拦截器。

```go
// 服务端
s := grpc.NewServer(o11y.GRPCServerOptions()...)

// 客户端
conn, err := grpc.Dial(target, o11y.WithGRPCClientInstrumentation()...)
```

#### HTTP
标准中间件和客户端封装。

```go
// 服务端中间件
mux = o11y.Handler(cfg)(mux)

// 客户端
client := o11y.NewHTTPClient(nil)
```

### 4. 使用 `o11y.Run()` 赋能业务逻辑

这是 `o11y` 的核心。包裹你的业务代码，即可免费获得所有可观测性能力。

```go
func processOrder(ctx context.Context, orderID string) error {
    // 使用 o11y.Run 包裹逻辑
    // "process_order" 将作为 Span 名称和指标中的 operation 标签
    return o11y.Run(ctx, "process_order", func(ctx context.Context, s o11y.State) error {
        // s.Log 自动包含 trace_id 和 span_id
        log := s.Log
        log.Info().Str("order_id", orderID).Msg("Starting to process order")

        // 模拟数据库调用 (o11y.OpenSQL 自动追踪)
        if err := db.ExecContext(ctx, "UPDATE orders SET status = ?", "PROCESSING"); err != nil {
            return err // o11y 自动记录错误并更新 Span 状态
        }

        // 成功后手动记录一个业务指标
        s.IncCounter("orders_processed_total")
        log.Info().Msg("Order processed successfully")
        return nil
    })
}
```

---

## 进阶用法：`State` 对象

`State` 对象允许你添加丰富的业务上下文并传播 Baggage。

```go
o11y.Run(ctx, "find_product", func(ctx context.Context, s o11y.State) error {
    // 1. 为当前 Span 添加属性 (方便在 Jaeger/Grafana 中搜索)
    s.SetAttributes(attribute.String("product_id", productID))
    
    // 2. 传播上下文 (Baggage)
    // 这会将 "tier=gold" 添加到请求头中，传递给下游服务。
    // 重要：必须使用返回的 'ctx' 进行后续调用。
    ctx = s.SetBaggage(ctx, "user_tier", "gold")
    
    // 3. 记录直方图指标
    s.RecordHistogram("custom.operation.latency", 0.123)
    
    // 4. 添加时间线事件 (Span Event)
    s.AddEvent("cache_miss")
    
    return nil
})
```

---

## 📈 开箱即用的指标

`o11y` 自动采集以下标准指标：

#### **HTTP 服务器**
- `http.server.request.total`: 请求总数 (标签: method, route, status_code)。
- `http.server.request.duration`: 请求延迟分布。
- `http.server.active_requests`: 当前活动请求数。

#### **数据库**
- `db.client.query.duration`: 数据库查询耗时分布。
- `sql.db.stats.connections.open`: 当前打开的连接总数。
- `sql.db.stats.connections.idle`: 空闲连接数。
- `sql.db.stats.connections.in_use`: 正在使用的连接数。

#### **业务逻辑 (`o11y.Run`)**
- `biz.operation.duration`: 业务逻辑块的执行时长。
- `biz.operation.error.total`: 业务逻辑块的错误总数。

## 整体架构

`o11y` 负责**产生**数据。我们推荐使用 **OpenTelemetry Collector** 采集数据，存储到 **Prometheus** (指标), **Loki** (日志), 和 **Jaeger/Tempo** (追踪)，并使用 **Grafana** 进行可视化。
