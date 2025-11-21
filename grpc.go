package o11y

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	gcodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServerOptions 返回一组推荐的 gRPC ServerOption。
// 包含：
// 1. OpenTelemetry StatsHandler (处理 Tracing 和 Metrics)
// 2. Unary & Stream Interceptors (处理 Logger 注入、Panic 恢复和访问日志)
//
// 用法:
//
//	s := grpc.NewServer(o11y.GRPCServerOptions()...)
func GRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		// 1. OTel 官方集成：负责 Context 传播、Span 创建和标准 RPC 指标
		grpc.StatsHandler(otelgrpc.NewServerHandler()),

		// 2. 自定义拦截器链
		grpc.ChainUnaryInterceptor(unaryServerInterceptor()),
		grpc.ChainStreamInterceptor(streamServerInterceptor()),
	}
}

// unaryServerInterceptor 处理单次调用 (Request-Response)
func unaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		// 1. 准备 Logger 和 Context
		// otelgrpc 已经运行，Context 中已有 Span
		startTime := time.Now()
		ctx = injectLogger(ctx, info.FullMethod)

		// 获取刚才注入的 logger，用于后续记录
		logger := GetLoggerFromContext(ctx)

		// 2. Panic 恢复
		defer func() {
			if r := recover(); r != nil {
				// 记录堆栈
				stack := FilterStackTrace(string(debug.Stack()), DefaultLogIgnore)
				logger.Error().
					Interface("panic", r).
					Str("stack", stack).
					Msg("gRPC server panic recovered")

				// 标记 Span 为 Error
				span := trace.SpanFromContext(ctx)
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, fmt.Sprintf("panic: %v", r))

				// 记录 Panic 指标
				AddToIntCounter(ctx, "rpc.server.panics", 1, attribute.String("method", info.FullMethod))

				// 返回 Internal 错误给客户端
				err = status.Errorf(gcodes.Internal, "Internal Server Error")
			}
		}()

		// 3. 执行业务逻辑
		resp, err = handler(ctx, req)

		// 4. 记录访问日志或错误日志
		// 只有错误发生时才打印 Error 日志，正常请求可根据 Level 决定是否打印 Info
		duration := time.Since(startTime)
		if err != nil {
			// 忽略客户端取消导致的错误日志，避免刷屏
			if status.Code(err) != gcodes.Canceled {
				logger.Error().Err(err).Dur("dur", duration).Msg("gRPC execution failed")
			}
		} else {
			logger.Debug().Dur("dur", duration).Msg("gRPC execution success")
		}

		return resp, err
	}
}

// streamServerInterceptor 处理流式调用
func streamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) { // 1. 使用命名返回值 err
		// 1. 准备 Logger
		ctx := injectLogger(ss.Context(), info.FullMethod)
		logger := GetLoggerFromContext(ctx)

		// 包装 ServerStream 以便 Handler 能拿到新的 Context
		wrappedStream := &wrappedServerStream{
			ServerStream: ss,
			ctx:          ctx,
		}

		// 2. Panic 恢复
		defer func() {
			if r := recover(); r != nil {
				stack := FilterStackTrace(string(debug.Stack()), DefaultLogIgnore)
				logger.Error().Interface("panic", r).Str("stack", stack).Msg("gRPC stream panic recovered")

				span := trace.SpanFromContext(ctx)
				errParams := fmt.Errorf("panic: %v", r)
				span.RecordError(errParams)
				span.SetStatus(codes.Error, errParams.Error())

				AddToIntCounter(ctx, "rpc.server.panics", 1, attribute.String("method", info.FullMethod))

				// 3. 将 Panic 转换为 gRPC 错误返回，而不是导致进程崩溃
				err = status.Errorf(gcodes.Internal, "Internal Server Error: %v", r)
			}
		}()

		return handler(srv, wrappedStream)
	}
}

// injectLogger 辅助函数：将 TraceID 注入 Logger 并放入 Context
func injectLogger(ctx context.Context, method string) context.Context {
	span := trace.SpanFromContext(ctx)
	parentLogger := GetLoggerFromContext(ctx)

	// 如果有 Trace，注入 trace_id 和 span_id
	if span.SpanContext().IsValid() {
		l := parentLogger.With().
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", span.SpanContext().SpanID().String()).
			Str("rpc_method", method).
			Logger()
		return l.WithContext(ctx)
	}

	// 即使没有 Trace，也注入 method 字段方便检索
	l := parentLogger.With().Str("rpc_method", method).Logger()
	return l.WithContext(ctx)
}

// wrappedServerStream 用于在 Stream 拦截器中传递修改后的 Context
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}
