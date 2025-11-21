package o11y

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// DefaultLogIgnore defines a list of common function/file path prefixes
// to be filtered out from panic stack traces. This significantly reduces noise,
// allowing developers to focus on their application's code.
var DefaultLogIgnore = []string{
	"runtime/panic.go",
	"runtime/debug/stack.go",
	"github.com/rs/zerolog.",
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp.",
	"net/http/server.go",
	// Filter our own middleware and hooks to avoid clutter.
	"o11y.(*Middleware).serveHTTP", // This is a forward reference to a future file, which is fine.
	"o11y.initialization.PanicHook",
}

// setupLogging configures and creates the primary zerolog.Logger instance based on LogConfig.
// It returns the configured logger (before global fields are added) and a shutdown function
// responsible for closing any open file handles.
func setupLogging(cfg LogConfig) (zerolog.Logger, ShutdownFunc) {
	// 1. Parse the configured log level string.
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil || cfg.Level == "" {
		level = zerolog.InfoLevel
		// Use a temporary, simple logger to warn about the invalid configuration.
		log.Warn().Msgf("Invalid or empty log level '%s', defaulting to 'info'", cfg.Level)
	}
	zerolog.SetGlobalLevel(level)

	// 2. Set the global time field format for performance.
	// Using Unix timestamps is generally faster and produces smaller log entries.
	switch cfg.TimePrecision {
	case "s":
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	case "ms":
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	case "us":
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMicro
	case "ns":
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
	default:
		// Default to Unix milliseconds as a good balance between precision and size.
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	}

	var writers []io.Writer
	var closers []io.Closer

	// 3. Configure file output and rotation using lumberjack.
	if cfg.EnableFile {
		if cfg.FileRotation.Filename == "" {
			log.Error().Msg("Log file is enabled but no filename is provided in config. Disabling file logging.")
		} else {
			fileWriter := &lumberjack.Logger{
				Filename:   cfg.FileRotation.Filename,
				MaxSize:    cfg.FileRotation.MaxSize,
				MaxBackups: cfg.FileRotation.MaxBackups,
				MaxAge:     cfg.FileRotation.MaxAge,
				Compress:   cfg.FileRotation.Compress,
			}
			writers = append(writers, fileWriter)
			closers = append(closers, fileWriter) // lumberjack.Logger implements io.Closer
		}
	}

	// 4. Configure console output.
	// To prevent accidental loss of logs, we default to console output if no other writer is configured.
	if cfg.EnableConsole || len(writers) == 0 {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339, // Human-friendly time format for console.
		})
	}

	// 5. Create the logger instance with all configured writers.
	// MultiLevelWriter sends logs to all writers in the slice.
	multiWriter := zerolog.MultiLevelWriter(writers...)
	logger := zerolog.New(multiWriter)

	// 6. Add caller information if enabled.
	// This adds a slight performance overhead, so it's best used during development.
	if cfg.EnableCaller {
		// Optimize the caller output to be just "file:line", removing the long path.
		// This improves readability in console logs.
		zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
			// Simple basename implementation to avoid importing path/filepath
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			return short + ":" + strconv.Itoa(line)
		}
		logger = logger.With().Caller().Logger()
	}

	// 7. Create the shutdown function.
	// This function will be called by the aggregate shutdown function in Init.
	shutdown := func(ctx context.Context) error {
		var errs error
		for _, c := range closers {
			if err := c.Close(); err != nil {
				// Collect all errors instead of returning on the first one.
				errs = errors.Join(errs, err)
			}
		}
		return errs
	}

	return logger, shutdown
}

// PanicHook creates a zerolog.Hook that, when a panic-level event is logged,
// captures the current goroutine's stack trace, filters it for clarity,
// and adds it to the log event under the "stack" key.
func PanicHook(ignore []string) zerolog.Hook {
	// If no custom filters are provided, use the sensible defaults.
	if len(ignore) == 0 {
		ignore = DefaultLogIgnore
	}
	return zerolog.HookFunc(func(e *zerolog.Event, level zerolog.Level, msg string) {
		if level == zerolog.PanicLevel {
			stack := FilterStackTrace(string(debug.Stack()), ignore)
			e.Str("stack", stack)
		}
	})
}

// FilterStackTrace cleans a raw stack trace string by removing irrelevant frames.
// It takes the raw stack and a slice of prefixes to ignore.
// It works by processing the stack trace in pairs of lines (function call and file path).
func FilterStackTrace(stack string, ignore []string) string {
	// If no custom filters are provided, use the sensible defaults.
	if len(ignore) == 0 {
		ignore = DefaultLogIgnore
	}

	lines := strings.Split(stack, "\n")
	if len(lines) < 2 {
		return stack // Not a valid stack trace, return as is.
	}

	var result strings.Builder
	// The first line is always "goroutine X [running]:", which we keep.
	result.WriteString(lines[0] + "\n")

	// Stack frames appear in pairs: the function call line, then the file:line path.
	// We iterate through these pairs.
	for i := 1; i+1 < len(lines); i += 2 {
		funcLine := lines[i]
		fileLine := strings.TrimSpace(lines[i+1])

		isIgnored := false
		for _, prefix := range ignore {
			// Check if either line in the pair matches an ignore prefix.
			if strings.HasPrefix(funcLine, prefix) || strings.Contains(fileLine, prefix) {
				isIgnored = true
				break
			}
		}

		if !isIgnored {
			// If the frame is relevant, add it to our result.
			result.WriteString(funcLine + "\n")
			result.WriteString(fileLine + "\n")
		}
	}

	return result.String()
}
