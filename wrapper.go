package o11y

import (
	"database/sql"
	"database/sql/driver"
	"net/http"

	"github.com/XSAM/otelsql"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"google.golang.org/grpc"
)

// OpenSQL is a drop-in replacement for `sql.Open` that is instrumented with OpenTelemetry.
// It internally calls `otelsql.Open`, automatically creating trace spans and metrics for all
// database operations (queries, executions, transactions, etc.).
// This is the easiest way to integrate o11y observability into a new database connection.
//
// `driverName` should be the name of the underlying driver (e.g., "postgres", "mysql").
// `dsn` is the data source name string.
//
// Usage:
//
//	db, err := o11y.OpenSQL("postgres", "user=... password=... dbname=... sslmode=disable")
//	if err != nil {
//	    log.Fatal().Err(err).Msg("Failed to connect to database")
//	}
func OpenSQL(driverName, dsn string) (*sql.DB, error) {
	// otelsql.AttributesFromDSN attempts to parse the host and port from the DSN.
	dsnAttrs := otelsql.AttributesFromDSN(dsn)

	// We combine the parsed attributes with the standard db.system attribute.
	allAttrs := append(dsnAttrs, semconv.DBSystemNameKey.String(driverName))

	// Call otelsql.Open, which is an instrumented wrapper for `sql.Open`.
	// We enable the SQLCommenter to facilitate trace propagation across databases.
	return otelsql.Open(driverName, dsn,
		otelsql.WithAttributes(allAttrs...),
		otelsql.WithSQLCommenter(true),
	)
}

// OpenDBWithConnector wraps a standard `driver.Connector` with OpenTelemetry instrumentation
// and returns an instrumented `*sql.DB`.
// This is the preferred method for integrating database observability in modern Go applications,
// especially when using connection pools. It automatically records trace spans, latency metrics,
// and connection pool status.
//
// `driverName` should be the name of the underlying driver (e.g., "pgx", "mysql") and is used
// to set telemetry attributes.
//
// Usage:
//
//	pgxConfig, _ := pgx.ParseConfig("...")
//	rawConnector := pgx.NewConnector(*pgxConfig)
//	db := o11y.OpenDBWithConnector("pgx", rawConnector)
func OpenDBWithConnector(driverName string, connector driver.Connector) *sql.DB {
	// `otelsql.OpenDB` is a drop-in replacement for `sql.OpenDB` that accepts a connector
	// and returns an instrumented *sql.DB.
	return otelsql.OpenDB(connector,
		otelsql.WithAttributes(
			// Add the database system type, which helps with filtering in Grafana/Jaeger.
			semconv.DBSystemNameKey.String(driverName),
		),
		otelsql.WithSQLCommenter(true), // Enables database-level trace correlation by injecting the trace context into SQL comments.
	)
}

// RegisterDBStatsMetrics registers callbacks to collect database connection pool statistics from a *sql.DB instance.
// It should be called once per database instance after it has been created.
// The `instanceName` is a logical identifier for the database instance (e.g., "primary-writer", "read-replica-1")
// and will be added as a "db.instance.id" attribute to the metrics for differentiation.
// This function uses the underlying otelsql.RegisterDBStatsMetrics functionality.
func RegisterDBStatsMetrics(db *sql.DB, instanceName string) {
	log.Info().Str("db_instance", instanceName).Msg("Registering database connection pool metrics.")

	// otelsql.RegisterDBStatsMetrics requires a set of options. We provide the instanceName
	// as a standard attribute to distinguish between different database pools.
	err := otelsql.RegisterDBStatsMetrics(db,
		otelsql.WithAttributes(
			attribute.String("db.instance.id", instanceName),
		),
	)
	if err != nil {
		// We log the error but don't panic, as the rest of the application
		// might still be able to function correctly. This is consistent with
		// how runtime/host metrics collection errors are handled.
		log.Error().Err(err).Str("db_instance", instanceName).Msg("Failed to register database connection pool metrics.")
	}
}

// WithGRPCClientInstrumentation returns a `grpc.DialOption` that enables OpenTelemetry
// instrumentation for a gRPC client via a `stats.Handler`. This is the standard,
// recommended method for the `otelgrpc` library.
//
// This option automatically handles tracing and metrics for all outbound gRPC calls.
//
// Usage:
//
//	conn, err := grpc.NewClient(
//	    target,
//	    o11y.WithGRPCClientInstrumentation(),
//	    grpc.WithTransportCredentials(insecure.NewCredentials()),
//	)
func WithGRPCClientInstrumentation() grpc.DialOption {
	// Use grpc.WithStatsHandler and pass in otelgrpc.NewClientHandler().
	// This is the simplest and most comprehensive integration method.
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler())
}

// GRPCClientOptions 返回一组推荐的 gRPC DialOption，用于客户端集成。
// 包含 OTel StatsHandler。
func GRPCClientOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
}

// NewHTTPClient returns a new `*http.Client` that is automatically instrumented for
// OpenTelemetry tracing. All requests made with this client will generate trace spans
// and automatically propagate the trace context.
//
// If the `transport` argument is nil, `http.DefaultTransport` will be used.
//
// Usage:
//
//	httpClient := o11y.NewHTTPClient(nil)
//	resp, err := httpClient.Get("https://api.example.com/v1/users")
func NewHTTPClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = http.DefaultTransport
	}

	// otelhttp.NewTransport wraps an existing http.RoundTripper.
	// It creates a client-side span for each outgoing request and injects the
	// W3C Trace-Context into the request headers.
	instrumentedTransport := otelhttp.NewTransport(transport)

	return &http.Client{
		Transport: instrumentedTransport,
	}
}
