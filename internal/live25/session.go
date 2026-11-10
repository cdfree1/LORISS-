package live25

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var (
	ErrSessionSource = errors.New("live25: session source is required")
	ErrSessionHost   = errors.New("live25: refusing to send session to an unconfigured host")
	ErrSessionScheme = errors.New("live25: refusing to send session over an insecure scheme")
	ErrSessionValue  = errors.New("live25: invalid session value")
)

// SessionSource obtains a user-delegated Cookie request header at request
// time. Implementations must use an approved local secret mechanism and must
// not log or persist the returned value.
type SessionSource interface {
	CookieHeader(context.Context) (string, error)
}

// StaticSession is an in-memory session source for a local process. Callers
// are responsible for loading it from an approved local secret mechanism; it
// must never be populated from source control, a database, or application
// logs.
type StaticSession string

func (s StaticSession) CookieHeader(context.Context) (string, error) {
	value := string(s)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", ErrSessionValue
	}
	return value, nil
}

// SessionTransport attaches a user session only to requests for the configured
// 25Live authority. It prevents an unexpected redirect or caller mistake from
// transmitting the session to a different host.
type SessionTransport struct {
	Base      http.RoundTripper
	Authority string
	Source    SessionSource
}

func (t SessionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.Source == nil {
		return nil, ErrSessionSource
	}
	if !strings.EqualFold(request.URL.Host, t.Authority) {
		return nil, fmt.Errorf("%w: %s", ErrSessionHost, request.URL.Host)
	}
	if request.URL.Scheme != "https" && !isLoopbackHost(request.URL.Hostname()) {
		return nil, fmt.Errorf("%w: %s", ErrSessionScheme, request.URL.Scheme)
	}
	session, err := t.Source.CookieHeader(request.Context())
	if err != nil {
		return nil, fmt.Errorf("live25: obtain session: %w", err)
	}
	if session == "" || strings.ContainsAny(session, "\r\n") {
		return nil, ErrSessionValue
	}

	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Cookie", session)
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// NewSessionHTTPClient returns a copy of base whose requests attach the
// supplied user session only to baseURL's authority. It does not create a
// cookie jar or implement login, refresh, extraction, or session persistence.
func NewSessionHTTPClient(baseURL string, source SessionSource, base *http.Client) (*http.Client, error) {
	if source == nil {
		return nil, ErrSessionSource
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("live25: parse session base URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: session base URL host is required", ErrInvalidRequest)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("%w: session base URL must use HTTPS", ErrInvalidRequest)
	}
	if base == nil {
		base = &http.Client{}
	}
	derived := *base
	derived.Transport = SessionTransport{
		Base:      base.Transport,
		Authority: u.Host,
		Source:    source,
	}
	return &derived, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
