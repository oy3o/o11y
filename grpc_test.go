package o11y

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestUnaryServerInterceptor_Success verifies normal execution
func TestUnaryServerInterceptor_Success(t *testing.T) {
	cfg := Config{Enabled: true, Trace: TraceConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	interceptor := unaryServerInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "reply", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	resp, err := interceptor(context.Background(), "req", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "reply", resp)
}

// TestUnaryServerInterceptor_Panic verifies panic is recovered and converted to error
func TestUnaryServerInterceptor_Panic(t *testing.T) {
	cfg := Config{Enabled: true, Metric: MetricConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	// Ensure the metric used in panic recovery is registered to avoid log noise/errors
	RegisterInt64Counter("rpc.server.panic.total", "test", "{panic}")

	interceptor := unaryServerInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("unexpected crash")
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	resp, err := interceptor(context.Background(), "req", info, handler)

	assert.Nil(t, resp)
	assert.Error(t, err)

	// Verify error code is Internal
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// TestStreamServerInterceptor_Panic verifies stream panic recovery
// Note: This depends on the fix suggested previously (using named return 'err')
func TestStreamServerInterceptor_Panic(t *testing.T) {
	cfg := Config{Enabled: true, Metric: MetricConfig{Enabled: true, Exporter: "none"}}
	shutdown, _ := Init(cfg)
	defer shutdown(context.Background())

	RegisterInt64Counter("rpc.server.panic.total", "test", "{panic}")

	interceptor := streamServerInterceptor()
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		panic("stream crash")
	}
	info := &grpc.StreamServerInfo{FullMethod: "/test/StreamMethod"}

	// Mock ServerStream
	mockStream := &mockServerStream{ctx: context.Background()}

	err := interceptor(nil, mockStream, info, handler)

	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	// Expect Internal error instead of process crash
	assert.Equal(t, codes.Internal, st.Code())
}

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}
