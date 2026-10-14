package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"loriss-prototype/internal/postgres"
	"loriss-prototype/internal/syncstate"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("PORT must be an integer between 1 and 65535")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := openDatabase(ctx)
	if err != nil {
		return err
	}
	if pool != nil {
		defer pool.Close()
		state := syncstate.New(postgres.New(pool))
		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := state.Ping(connectCtx); err != nil {
			log.Print("PostgreSQL connection unavailable; continuing HTTP service")
		} else {
			log.Print("connected to PostgreSQL")
		}
		cancel()
	}
	server := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("HTTP service listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		return err
	case <-ctx.Done():
		stop()
		log.Print("shutting down HTTP service")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// openDatabase configures an optional pool. The HTTP scaffold can still run
// without a database; future synchronization handlers can use the existing store.
func openDatabase(ctx context.Context) (*pgxpool.Pool, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, nil
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Parse errors may include the password. Return a safe diagnostic instead.
		return nil, fmt.Errorf("DATABASE_URL is invalid")
	}
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	return pgxpool.NewWithConfig(ctx, config)
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	return mux
}
