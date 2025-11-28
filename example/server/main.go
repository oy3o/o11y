package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"gopkg.in/yaml.v3"

	"github.com/oy3o/o11y"
)

// AppConfig is a wrapper struct to match the YAML structure.
type AppConfig struct {
	O11y o11y.Config `yaml:"o11y"`
}

// Global variable to hold our instrumented HTTP client.
var instrumentedClient *http.Client

func main() {
	// --- 1. Load Configuration ---
	// In a real app, you might use flags or env vars to find the config file.
	cfg, err := loadConfig("example/config.yaml")
	if err != nil {
		// Using fmt here because our logger isn't configured yet.
		fmt.Printf("fatal: failed to load config: %v\n", err)
		os.Exit(1)
	}

	// --- 2. Initialize o11y Library ---
	// This single line sets up logging, tracing, and metrics!
	shutdown, err := o11y.Init(cfg.O11y)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize o11y")
	}
	// Defer the shutdown function to ensure all telemetry data is flushed before exit.
	defer shutdown(context.Background())

	// Initialize our instrumented HTTP client for making outbound requests.
	instrumentedClient = o11y.NewHTTPClient(nil)

	// --- 3. Set Up HTTP Server and Routes ---
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", okHandler)
	mux.HandleFunc("/error", errorHandler)
	mux.HandleFunc("/user", userHandler)
	mux.HandleFunc("/panic", panicHandler)

	// Wrap the entire mux with the o11y middleware. This automatically
	// handles tracing, panic recovery, and logger injection for all routes.
	o11yMiddleware := o11y.Handler(cfg.O11y)
	wrappedMux := o11yMiddleware(mux)

	server := &http.Server{
		Addr:    ":8080",
		Handler: wrappedMux,
	}

	// --- 4. Start Server and Handle Graceful Shutdown ---
	go func() {
		// log is the globally configured zerolog instance.
		log.Info().Msgf("Server starting on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Warn().Msg("Shutdown signal received, starting graceful shutdown...")

	// Create a context with a timeout for the shutdown process.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown failed")
	}

	log.Info().Msg("Server gracefully stopped.")
}

// okHandler demonstrates a simple, successful request.
func okHandler(w http.ResponseWriter, r *http.Request) {
	// We can get the trace-aware logger directly from the request context.
	logger := o11y.GetLoggerFromContext(r.Context())
	logger.Info().Msg("Handling a successful request.")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Everything is A-OK!"))
}

// errorHandler demonstrates a request that results in a server error.
func errorHandler(w http.ResponseWriter, r *http.Request) {
	logger := o11y.GetLoggerFromContext(r.Context())
	logger.Error().Msg("This is a simulated error.")
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}

// panicHandler demonstrates the middleware's panic recovery.
func panicHandler(w http.ResponseWriter, r *http.Request) {
	panic("this is a deliberate panic to test the recovery middleware!")
}

// userHandler demonstrates a more complex business logic flow using o11y.Run.
func userHandler(w http.ResponseWriter, r *http.Request) {
	logger := o11y.GetLoggerFromContext(r.Context())
	userID := "user-12345"

	// Call our business logic function.
	userData, err := fetchUserData(r.Context(), userID)
	if err != nil {
		logger.Error().Err(err).Str("user_id", userID).Msg("Failed to fetch user data")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(userData))
}

// fetchUserData is our core business logic, wrapped with o11y.Run.
func fetchUserData(ctx context.Context, userID string) (string, error) {
	var userData string

	err := o11y.Run(ctx, "fetchUserData",
		func(ctx context.Context, s o11y.State) error {
			s.Log.Info().Str("user_id", userID).Msg("Starting to fetch user data")

			// 1. Example of using SetBaggage
			// We must capture the new context to propagate "tier" to the external service.
			ctx = s.SetBaggage(ctx, "user.tier", "gold")

			s.SetAttributes(
				attribute.String("user.id", userID),
				attribute.Bool("user.is_admin", false),
			)

			time.Sleep(10 * time.Millisecond)
			s.IncCounter("app.cache.events.total", attribute.String("result", "miss"))
			s.AddEvent("cache_miss")

			s.Log.Debug().Msg("Calling external user data service...")
			// Uses the context with Baggage
			req, _ := http.NewRequestWithContext(ctx, "GET", "https://httpbin.org/get", nil)
			resp, httpErr := instrumentedClient.Do(req)
			if httpErr != nil {
				return httpErr
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			userData = string(body)

			// 2. Custom success metric is now recorded manually here
			s.IncCounter("app.login.events.total", attribute.String("method", "session"))

			s.Log.Info().Msg("Successfully fetched user data.")
			return nil
		},
	)

	return userData, err
}

// loadConfig reads a YAML file and unmarshals it into the AppConfig struct.
func loadConfig(path string) (*AppConfig, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg AppConfig
	err = yaml.Unmarshal(f, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
