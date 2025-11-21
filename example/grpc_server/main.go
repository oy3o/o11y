package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/oy3o/o11y"
	// 注意：实际项目中这里应该导入你的 protoc 生成的代码
	// pb "your-project/api/helloworld"
)

// --- 模拟 Protobuf 生成的代码 (为了让示例可直接编译运行) ---

// GreeterServer 模拟生成的服务接口
type GreeterServer interface {
	SayHello(context.Context, *HelloRequest) (*HelloReply, error)
}

type HelloRequest struct {
	Name string
}

type HelloReply struct {
	Message string
}

// RegisterGreeterServer 模拟注册函数
func RegisterGreeterServer(s grpc.ServiceRegistrar, srv GreeterServer) {
	// 在真实场景中，这里由 protoc 生成
	desc := &grpc.ServiceDesc{
		ServiceName: "helloworld.Greeter",
		HandlerType: (*GreeterServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "SayHello",
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					in := new(HelloRequest)
					if err := dec(in); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(GreeterServer).SayHello(ctx, in)
					}
					info := &grpc.UnaryServerInfo{
						Server:     srv,
						FullMethod: "/helloworld.Greeter/SayHello",
					}
					handler := func(ctx context.Context, req interface{}) (interface{}, error) {
						return srv.(GreeterServer).SayHello(ctx, req.(*HelloRequest))
					}
					return interceptor(ctx, in, info, handler)
				},
			},
		},
		Streams: []grpc.StreamDesc{},
	}
	s.RegisterService(desc, srv)
}

// --- 业务代码开始 ---

type AppConfig struct {
	O11y o11y.Config `yaml:"o11y"`
	Port int         `yaml:"port"`
}

// server 实现了 GreeterServer 接口
type server struct{}

// SayHello 实现业务逻辑
func (s *server) SayHello(ctx context.Context, in *HelloRequest) (*HelloReply, error) {
	// 虽然 gRPC 拦截器已经处理了 Trace 和 Panic，
	// 但使用 o11y.Run 仍是推荐做法，因为它能：
	// 1. 自动记录业务层面的延迟和错误指标 (app.operation.*)
	// 2. 提供便捷的 State 对象用于记录业务 Counter 和 Log
	var reply *HelloReply

	err := o11y.Run(ctx, "SayHelloLogic", func(ctx context.Context, s o11y.State) error {
		// s.Log 自动带有 TraceID, SpanID 和 RPC Method 信息
		s.Log.Info().Str("name", in.Name).Msg("Received RPC request")

		if in.Name == "panic" {
			panic("triggered intentional panic via gRPC")
		}

		if in.Name == "error" {
			return fmt.Errorf("simulated business error")
		}

		// 模拟业务处理耗时
		time.Sleep(50 * time.Millisecond)

		// 记录自定义业务指标
		s.IncCounter("business.greetings.total", attribute.String("type", "polite"))

		reply = &HelloReply{Message: "Hello " + in.Name}
		s.Log.Info().Str("reply", reply.Message).Msg("Sending response")
		return nil
	})

	return reply, err
}

func main() {
	// 1. 加载配置
	cfg := loadConfig()

	// 2. 初始化 o11y (日志、Trace、Metrics)
	// 提示：确保在 metric_registry.go 中注册了 "rpc.server.panics" 和 "business.greetings.total"
	shutdown := o11y.Init(cfg.O11y)
	defer shutdown(context.Background())

	// 手动注册示例中用到的自定义指标 (通常放在 init 或 metric_registry.go 中)
	o11y.RegisterInt64Counter("rpc.server.panics", "gRPC Panics", "{panic}")
	o11y.RegisterInt64Counter("business.greetings.total", "Total greetings sent", "{count}")

	// 3. 启动监听
	addr := fmt.Sprintf(":%d", cfg.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to listen")
	}

	// 4. 创建 gRPC Server
	// 使用 o11y.GRPCServerOptions() 注入全套可观测性拦截器
	s := grpc.NewServer(o11y.GRPCServerOptions()...)

	// 注册服务
	RegisterGreeterServer(s, &server{})

	// 启用 gRPC Server Reflection (方便调试工具如 grpcurl)
	reflection.Register(s)

	// 5. 启动服务
	go func() {
		log.Info().Msgf("gRPC server listening at %v", lis.Addr())
		if err := s.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("Failed to serve")
		}
	}()

	// 6. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Warn().Msg("Shutting down gRPC server...")

	s.GracefulStop()
	log.Info().Msg("Server exited")
}

func loadConfig() AppConfig {
	// 简单模拟配置加载，实际应读取文件
	return AppConfig{
		Port: 50051,
		O11y: o11y.Config{
			Enabled:     true,
			Service:     "grpc-example",
			Version:     "v0.1.0",
			Environment: "dev",
			Log: o11y.LogConfig{
				Level:         "debug",
				EnableConsole: true,
			},
			Trace: o11y.TraceConfig{
				Enabled:     true,
				Exporter:    "stdout", // 本地开发打印到控制台查看 Trace
				SampleRatio: 1.0,
			},
			Metric: o11y.MetricConfig{
				Enabled:           true,
				Exporter:          "prometheus",
				PrometheusAddr:    ":9090",
				EnableHostMetrics: true,
			},
		},
	}
}
