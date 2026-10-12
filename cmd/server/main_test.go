package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
