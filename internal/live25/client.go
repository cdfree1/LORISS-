// Package live25 reads event and room data from the configured 25Live instance.
//
// The endpoints implemented here were observed in UNC's 25Live web client.
// They are deliberately read-only and require an HTTP client whose
// authentication has been provisioned through an approved, non-human method.
package live25

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const DefaultBaseURL = "https://25live.collegenet.com/25live/data/unc/run"

var ErrInvalidRequest = errors.New("live25: invalid request")

// Reader is the read-only 25Live contract used by polling and reconciliation
// code. It intentionally contains no create, update, or cancellation methods.
type Reader interface {
	Location(context.Context, int) (Location, error)
	Availability(context.Context, AvailabilityRequest) (AvailabilityPage, error)
	EventDetail(context.Context, int) (EventDetail, error)
}

// BuildingReader adds building and room discovery to the read-only event
// reader. It is used only for explicitly selected building scopes.
type BuildingReader interface {
	Reader
	Buildings(context.Context) ([]Building, error)
	LocationsForBuilding(context.Context, int) ([]Location, error)
}

// Client implements Reader against a 25Live instance.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

// NewClient creates a read-only client. baseURL is the complete Series25 run
// path, for example https://host/25live/data/unc/run. Authentication belongs
// on the supplied HTTP client; this package never reads browser cookies or
// credentials from the environment.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("live25: parse base URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("%w: base URL must use HTTP or HTTPS", ErrInvalidRequest)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: base URL host is required", ErrInvalidRequest)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: u, httpClient: httpClient}, nil
}

// HTTPError reports a non-successful 25Live response without retaining its
// potentially sensitive body.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("live25: unexpected HTTP status %d", e.StatusCode)
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, destination any) error {
	u := *c.baseURL
	u.Path = path.Join(c.baseURL.Path, endpoint)
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("live25: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("live25: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return &HTTPError{StatusCode: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(destination); err != nil {
		return fmt.Errorf("live25: decode %s response: %w", endpoint, err)
	}
	return nil
}
