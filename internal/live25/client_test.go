package live25

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLocationUsesObservedReadOnlyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/25live/data/unc/run/space.json"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("space_id"), "42"; got != want {
			t.Errorf("space_id = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("scope"), "minimal"; got != want {
			t.Errorf("scope = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"spaces":{"space":{"space_id":42,"space_name":"TEST-ROOM","formal_name":"Test Room","building_id":7,"building_name":"Test Building","status":"est","max_capacity":50}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	location, err := client.Location(context.Background(), 42)
	if err != nil {
		t.Fatalf("Location() error = %v", err)
	}
	if location.ID != 42 || location.Name != "TEST-ROOM" || location.MaxCapacity != 50 {
		t.Fatalf("Location() = %#v", location)
	}
}

func TestAvailabilityNormalizesMixedTimeValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/25live/data/unc/run/availability/availabilitydata.json"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"space_id": "42", "start_dt": "2026-11-02T00:00:00", "page_size": "100",
			"comptype": "availability_daily", "compsubject": "location",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"page_count":1,"last_id":99,"lastupdate":"2026-11-02T01:00:00","subjects":[{"item_date":"2026-11-02T00:00:00","items":[{"itemId":12345,"itemId2":12,"itemName":"redacted","cur_event_state":2,"start":13.5,"end":"14.5","pre_start":13,"post_end":15,"event_type_id":3}]}]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	page, err := client.Availability(context.Background(), AvailabilityRequest{
		SpaceID: 42,
		Start:   time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if page.PageCount != 1 || len(page.Days) != 1 || len(page.Days[0].Entries) != 1 {
		t.Fatalf("Availability() = %#v", page)
	}
	entry := page.Days[0].Entries[0]
	if entry.EventID != 12345 || entry.Start != "13.5" || entry.End != "14.5" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestEventDetailExcludesExpandedContactsAndReadsReservations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/25live/data/unc/run/micro/event/12345/detail.json"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("expand"), "T"; got != want {
			t.Errorf("expand = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("include"), "all"; got != want {
			t.Errorf("include = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"content":{"id":12345,"updated":"2026-11-01T12:00:00","data":{"items":[{"id":12345,"etag":"revision","name":"redacted","title":"redacted","eventLocator":"locator","priority":1,"isActive":true,"updated":"2026-11-01T12:00:00","dates":{"startDate":"2026-11-02T13:30:00","endDate":"2026-11-02T14:30:00"},"context":{"state":2,"typeId":3,"parentId":0},"profiles":[{"profileId":5,"name":"profile","expectedCount":20,"registeredCount":10,"occurrenceDefn":{"recTypeId":1,"profileCode":"weekly","initStartDt":"2026-11-02T13:30:00","initEndDt":"2026-11-02T14:30:00"},"reservations":[{"rsrvId":88,"state":2,"rsrvStartDt":"2026-11-02T13:00:00","rsrvEndDt":"2026-11-02T15:00:00","evStartDt":"2026-11-02T13:30:00","evEndDt":"2026-11-02T14:30:00","spaces":[{"reserved":[{"spaceId":42,"share":false,"attendance":20,"rating":0}]}]}]}]}]},"expandedInfo":{"spaces":[{"spaceId":42,"etag":"room-version","spaceName":"TEST-ROOM","spaceFormalName":"Test Room","maxCapacity":50}],"states":[{"state":2,"stateName":"Confirmed"}],"contacts":[{"email":"not-retained@example.edu"}]}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	detail, err := client.EventDetail(context.Background(), 12345)
	if err != nil {
		t.Fatalf("EventDetail() error = %v", err)
	}
	if detail.ID != 12345 || len(detail.Events) != 1 || len(detail.Events[0].Profiles) != 1 {
		t.Fatalf("EventDetail() = %#v", detail)
	}
	reservation := detail.Events[0].Profiles[0].Reservations[0]
	if reservation.ID != 88 || len(reservation.Spaces) != 1 || reservation.Spaces[0].ID != 42 {
		t.Fatalf("reservation = %#v", reservation)
	}
}

func TestClientRejectsInvalidRequestAndReportsHTTPStatus(t *testing.T) {
	client, err := NewClient("", &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Availability(context.Background(), AvailabilityRequest{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Availability() error = %v, want ErrInvalidRequest", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not authorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	client = newTestClient(t, server)
	_, err = client.Location(context.Background(), 42)
	var statusError *HTTPError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Location() error = %v, want HTTPError(401)", err)
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	baseURL, err := url.Parse(server.URL + "/25live/data/unc/run")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(baseURL.String(), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFlexibleStringRejectsObjects(t *testing.T) {
	var value flexibleString
	err := value.UnmarshalJSON([]byte(`{"not":"a scalar"}`))
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
}

func TestBuildingsAndLocationsForBuildingUseObservedDiscoveryEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/25live/data/unc/run/micro/building/list.json":
			if got, want := r.URL.Query().Get("caller"), "pro-BuildingService.getBuildings"; got != want {
				t.Errorf("building caller = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(`{"content":{"data":{"items":[{"id":7,"buildingCode":"SN","buildingName":"Test North"}]}}}`))
		case "/25live/data/unc/run/spaces.json":
			if got, want := r.URL.Query().Get("building_id"), "7"; got != want {
				t.Errorf("building_id = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(`{"spaces":{"space":[{"space_id":42,"space_name":"TEST-ONE","building_id":7,"building_name":"Test North","status":"est","max_capacity":30},{"space_id":43,"space_name":"TEST-TWO","building_id":7,"building_name":"Test North","status":"est","max_capacity":40}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	buildings, err := client.Buildings(context.Background())
	if err != nil || len(buildings) != 1 || buildings[0].Code != "SN" {
		t.Fatalf("Buildings() = %#v, %v", buildings, err)
	}
	locations, err := client.LocationsForBuilding(context.Background(), 7)
	if err != nil || len(locations) != 2 || locations[1].ID != 43 {
		t.Fatalf("LocationsForBuilding() = %#v, %v", locations, err)
	}
}
