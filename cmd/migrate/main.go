// Command migrate applies LORISS's forward-only PostgreSQL migrations.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"loriss-prototype/internal/postgres"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(fmt.Errorf("connect to postgres: %w", err))
	}
	defer pool.Close()
	if err := postgres.Migrate(context.Background(), pool); err != nil {
		log.Fatal(fmt.Errorf("apply migrations: %w", err))
	}
}
