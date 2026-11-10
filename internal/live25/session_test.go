package live25

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionHTTPClientAttachesSessionOnlyToConfiguredAuthority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Cookie"), "session_name=opaque-value"; got != want {
			t.Errorf("Cookie = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewSessionHTTPClient(server.URL, StaticSession("session_name=opaque-value"), nil)
	if err != nil {
		t.Fatalf("NewSessionHTTPClient() error = %v", err)
	}
	response, err := client.Get(server.URL + "/25live/data/unc/run/ping.json")
	if err != nil {
		t.Fatalf("GET() error = %v", err)
	}
	response.Body.Close()
}

func TestSessionTransportRefusesOtherAuthority(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://other.example.test/data", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (SessionTransport{
		Authority: "25live.collegenet.com",
		Source:    StaticSession("session_name=opaque-value"),
	}).RoundTrip(request)
	if !errors.Is(err, ErrSessionHost) {
		t.Fatalf("RoundTrip() error = %v, want ErrSessionHost", err)
	}
}

func TestSessionTransportRefusesHTTPForConfiguredAuthority(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://25live.collegenet.com/data", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (SessionTransport{
		Authority: "25live.collegenet.com",
		Source:    StaticSession("session_name=opaque-value"),
	}).RoundTrip(request)
	if !errors.Is(err, ErrSessionScheme) {
		t.Fatalf("RoundTrip() error = %v, want ErrSessionScheme", err)
	}
}

func TestStaticSessionRejectsEmptyAndMultilineValues(t *testing.T) {
	for _, session := range []StaticSession{"", "session=value\nother=value"} {
		_, err := session.CookieHeader(context.Background())
		if !errors.Is(err, ErrSessionValue) {
			t.Fatalf("CookieHeader(%q) error = %v, want ErrSessionValue", session, err)
		}
	}
}

func TestSessionHTTPClientRequiresHTTPSOutsideLoopback(t *testing.T) {
	_, err := NewSessionHTTPClient("http://example.test/25live/data/unc/run", StaticSession("session=value"), nil)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewSessionHTTPClient() error = %v, want ErrInvalidRequest", err)
	}
}
