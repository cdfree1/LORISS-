package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"loriss-prototype/internal/postgres"
)

func TestHealthEndpoint(t *testing.T) {
	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", http.StatusNotFound},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			newHandler().ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if tc.status == http.StatusOK {
				if response.Body.String() != "{\"status\":\"ok\"}\n" {
					t.Fatalf("unexpected health response: %q", response.Body.String())
				}
				if response.Header().Get("Content-Type") != "application/json" {
					t.Fatal("health response must be JSON")
				}
			}
		})
	}
}

func TestInvalidPort(t *testing.T) {
	for _, port := range []string{"abc", "0", "-1", "65536"} {
		t.Run(port, func(t *testing.T) {
			t.Setenv("PORT", port)
			if err := run(); err == nil {
				t.Fatal("expected invalid PORT to fail")
			}
		})
	}
}

func TestOptionalDatabaseConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		fail bool
	}{
		{name: "not configured"},
		{name: "configured", url: "postgres://loriss@127.0.0.1:5432/loriss?sslmode=disable&pool_max_conns=5"},
		{name: "invalid", url: "postgres://user:secret-password@host:invalid/db", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tc.url)
			pool, err := openDatabase(context.Background())
			if pool != nil {
				defer pool.Close()
			}
			if (err != nil) != tc.fail {
				t.Fatalf("configuration error = %v, want failure %v", err, tc.fail)
			}
			if err != nil && strings.Contains(err.Error(), "secret-password") {
				t.Fatal("configuration error exposed credentials")
			}
			if tc.url == "" && pool != nil {
				t.Fatal("unconfigured server must not create a database pool")
			}
			if tc.url != "" && !tc.fail && pool == nil {
				t.Fatal("configured server must create a database pool")
			}
		})
	}
}

func TestDatabaseConnection(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to check the server's PostgreSQL connection")
	}
	t.Setenv("DATABASE_URL", databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := openDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.New(pool).Ping(ctx); err != nil {
		t.Fatalf("server pool cannot reach PostgreSQL: %v", err)
	}
}
