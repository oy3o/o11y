package o11y

// Config is the only configuration struct in the o11y package.
// It aggregates all configurable items for logs, traces, and metrics, and provides global metadata.
type Config struct {
	// Enabled is a global switch. If set to false, o11y.Init will immediately return a no-operation shutdown function,
	// and will not initialize any logs, traces, or metrics components. This is very useful in local development or testing environments.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Service is your service name (e.g., "user-service", "order-api").
	// This name will serve as the core identifier for all telemetry data (logs, traces, metrics).
	// It is recommended to use the `service.name` format from the OpenTelemetry semantics convention.
	Service string `yaml:"service" mapstructure:"service"`

	// Version is the current version number of your service (e.g., "v1.2.3", "2025.11.18").
	// This will be appended to the telemetry data to track performance and bugs across different versions.
	Version string `yaml:"version" mapstructure:"version"`

	// Environment is the environment in which the service runs (e.g., "development", "staging", "production").
	// This tag helps filter and isolate data from different environments in the backend system.
	Environment string `yaml:"environment" mapstructure:"environment"`

	// InstrumentationScope is the name of the tracer and meter used by the library.
	// It's a logical unit of instrumentation. Defaults to "o11y".
	InstrumentationScope string `yaml:"instrumentation_scope" mapstructure:"instrumentation_scope"`

	// Log contains all configurations related to logging.
	Log LogConfig `yaml:"log" mapstructure:"log"`

	// Trace contains all configurations related to distributed tracing.
	Trace TraceConfig `yaml:"trace" mapstructure:"trace"`

	// Metric contains all configurations related to metric statistics.
	Metric MetricConfig `yaml:"metric" mapstructure:"metric"`
}

// LogConfig defines the detailed behavior of logging.
type LogConfig struct {
	// Level defines the global minimum log level.
	// Optional values are "debug", "info", "warn", "error", "fatal", "panic".
	// If set to empty or invalid value, it will default to "info".
	Level string `yaml:"level" mapstructure:"level"`

	// TimePrecision defines the format and precision of the timestamps in the log.
	// Optional values:
	// "s": Unix timestamp in seconds (e.g., 1678886400)
	// "ms": Unix timestamp in milliseconds (e.g., 1678886400123)
	// "us": Unix timestamp in microseconds (e.g., 1678886400123456)
	// "ns": Unix timestamp in nanoseconds (e.g., 1678886400123456789)
	// Defaults to "ms", which is a good balance between performance and precision.
	TimePrecision string `yaml:"time_precision" mapstructure:"time_precision"`

	// EnableCaller controls whether the caller's filename and line number are included in log entries.
	// Enabling this option incurs a slight performance overhead; it is recommended to enable it in development environments for debugging purposes.
	EnableCaller bool `yaml:"caller" mapstructure:"caller"`

	// EnableConsole controls whether logs are output to standard output (stdout).
	// Logs output to the console are typically colored and in a human-readable format.
	EnableConsole bool `yaml:"console" mapstructure:"console"`

	// EnableFile controls whether logs are output to a file.
	// Logs output to a file are always in JSON format for easy machine parsing.
	EnableFile bool `yaml:"file" mapstructure:"file"`

	// FileRotation defines the log file rotation strategy; it only takes effect when EnableFile is true.
	FileRotation FileRotationConfig `yaml:"rotation" mapstructure:"rotation"`

	// StackFilters is a list of string prefixes used to filter out irrelevant stack frames in a panic hook.
	// This helps clean up panic logs, allowing developers to focus on the application code itself.
	// For example: "runtime/", "net/http".
	StackFilters []string `yaml:"stack_filters" mapstructure:"stack_filters"`
}

// FileRotationConfig defines the file rotation configuration for the Lumberjack library.
type FileRotationConfig struct {
	// Filename is the full path to the log file to be written.
	Filename string `yaml:"filename" mapstructure:"filename"`

	// MaxSize is the maximum size of a single log file before rotation, in MB.
	MaxSize int `yaml:"max_size" mapstructure:"max_size"`

	// MaxBackups is the maximum number of old log files to retain.
	MaxBackups int `yaml:"max_backups" mapstructure:"max_backups"`

	// MaxAge is the maximum number of days old log files are retained before deletion.
	MaxAge int `yaml:"max_age" mapstructure:"max_age"`

	// Compress controls whether to use gzip compression for rotated old log files.
	Compress bool `yaml:"compress" mapstructure:"compress"`
}

// TraceConfig defines the configuration for distributed tracing.
type TraceConfig struct {
	// Enabled controls whether distributed tracing is enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Exporter defines where to send tracing data.
	// Optional values:
	// "otlp-grpc": Sends data to the OpenTelemetry Collector via gRPC (recommended).
	// "stdout": Prints tracing data to standard output in a human-readable format for debugging.
	// "none": Enables the tracing API but discards all data for testing.
	Exporter string `yaml:"exporter" mapstructure:"exporter"`

	// Endpoint is the target address of the OTLP Exporter, used only when the Exporter is "otlp-grpc".
	// The format is usually "hostname:port", for example, "otel-collector:4317".
	Endpoint string `yaml:"endpoint" mapstructure:"endpoint"`

	// OtlpInsecure controls whether the OTLP gRPC client connection should be insecure.
	// Set to true for local development when TLS is not available. Defaults to false.
	OtlpInsecure bool `yaml:"otlp_insecure" mapstructure:"otlp_insecure"`

	// SampleRatio defines the sampling rate of the traces, with values between 0.0 and 1.0.
	// 1.0 means sampling all traces.
	// 0.5 means sampling 50% of the traces.
	// 0.0 means not sampling any traces.
	SampleRatio float64 `yaml:"sample_ratio" mapstructure:"sample_ratio"`
}

// MetricConfig defines the configuration for metric statistics.
type MetricConfig struct {
	// Enabled controls whether metric statistics are enabled.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Exporter defines the method for exporting metrics.
	// Optional values:
	// "prometheus": Exposes an HTTP endpoint for the Prometheus service to pull data (recommended).
	// "none": Enables the metrics API but discards all data.
	Exporter string `yaml:"exporter" mapstructure:"exporter"`

	// PrometheusPath is the HTTP path exposed by the Prometheus Exporter, used only when the Exporter is "prometheus".
	// The default and common value is "/metrics".
	PrometheusPath string `yaml:"prometheus_path" mapstructure:"prometheus_path"`

	// PrometheusAddr is the address (host:port) on which the Prometheus metrics server will listen.
	// Defaults to ":2222".
	PrometheusAddr string `yaml:"prometheus_addr" mapstructure:"prometheus_addr"`

	// EnableHostMetrics controls whether to automatically collect host metrics (e.g., CPU, memory).
	// If true, the library will start a collector for host metrics upon initialization.
	EnableHostMetrics bool `yaml:"enable_host_metrics" mapstructure:"enable_host_metrics"`
}
